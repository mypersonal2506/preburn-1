package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
)

const (
	apiURLFlag      = "api-url"
	apiKeyFlag      = "api-key"
	customersFlag   = "customers"
	durationFlag    = "duration"
	concurrencyFlag = "concurrency"

	defaultBenchCustomers   = 100
	defaultBenchDuration    = time.Minute
	defaultBenchConcurrency = 20
	benchRequestTimeout     = 10 * time.Second

	benchCheckPath        = "api/v1/check"
	benchReportPath       = "api/v1/report"
	benchCustomerPrefix   = "bench-customer-"
	benchFeature          = "text_to_video"
	benchProvider         = "fal_ai"
	benchModel            = "fal-ai/veo3.1/fast"
	benchMeter            = "output_seconds"
	benchQuantity         = "8"
	benchDecisionSource   = "server"
	deniedOutcome         = "deny"
	benchLatencyPrecision = time.Microsecond
	benchTableColumnGap   = 2
)

type benchSettings struct {
	checkURL    string
	reportURL   string
	apiKey      string
	customers   int
	duration    time.Duration
	concurrency int
}

type benchCheckRequest struct {
	CustomerID    string            `json:"customer_id"`
	Feature       string            `json:"feature"`
	Provider      string            `json:"provider"`
	Model         string            `json:"model"`
	UsageEstimate map[string]string `json:"usage_estimate"`
}

type benchCheckResponse struct {
	DecisionID string `json:"decision_id"`
	Outcome    string `json:"outcome"`
}

type benchReportRequest struct {
	DecisionSource string            `json:"decision_source"`
	DecisionID     string            `json:"decision_id"`
	Usage          map[string]string `json:"usage"`
}

type benchOperation struct {
	latencies []time.Duration
	failures  map[string]int
}

type benchWorkerResult struct {
	checks  benchOperation
	reports benchOperation
}

var benchPercentiles = []float64{0.50, 0.95, 0.99}

func newBenchCommand() *cobra.Command {
	bench := newGroupCommand("bench", "Measure the API under load", newBenchCheckCommand())
	bench.Hidden = true
	return bench
}

func newBenchCheckCommand() *cobra.Command {
	var apiURL, apiKey string
	var customers, concurrency int
	var duration time.Duration
	check := &cobra.Command{
		Use:   "check",
		Short: "Send checks, report each decision that is not a deny, and print the latencies",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			settings, err := newBenchSettings(apiURL, apiKey, customers, duration, concurrency)
			if err != nil {
				return err
			}
			return runBenchCheck(command, settings)
		},
	}
	check.Flags().StringVar(&apiURL, apiURLFlag, "", "base URL of the api process, such as http://localhost:8080 (required)")
	check.Flags().StringVar(&apiKey, apiKeyFlag, "", "runtime or admin API key of the environment to load (required)")
	check.Flags().IntVar(&customers, customersFlag, defaultBenchCustomers, "number of customers the checks spread over")
	check.Flags().DurationVar(&duration, durationFlag, defaultBenchDuration, "how long to start new checks")
	check.Flags().IntVar(&concurrency, concurrencyFlag, defaultBenchConcurrency, "number of checks and reports in flight")
	return check
}

func newBenchSettings(apiURL string, apiKey string, customers int, duration time.Duration, concurrency int) (benchSettings, error) {
	if err := errors.Join(requiredFlag(apiURLFlag, apiURL), requiredFlag(apiKeyFlag, apiKey)); err != nil {
		return benchSettings{}, err
	}
	baseURL, err := url.Parse(apiURL)
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return benchSettings{}, fmt.Errorf("%w: --%s must be an http or https URL with a host", errInvalidInput, apiURLFlag)
	}
	if customers < 1 {
		return benchSettings{}, fmt.Errorf("%w: --%s must be at least 1", errInvalidInput, customersFlag)
	}
	if concurrency < 1 {
		return benchSettings{}, fmt.Errorf("%w: --%s must be at least 1", errInvalidInput, concurrencyFlag)
	}
	if duration <= 0 {
		return benchSettings{}, fmt.Errorf("%w: --%s must be positive", errInvalidInput, durationFlag)
	}
	return benchSettings{
		checkURL:    baseURL.JoinPath(benchCheckPath).String(),
		reportURL:   baseURL.JoinPath(benchReportPath).String(),
		apiKey:      apiKey,
		customers:   customers,
		duration:    duration,
		concurrency: concurrency,
	}, nil
}

//nolint:contextcheck // cobra hands commands their context through command.Context(), which the flag closures of RunE cannot take as a parameter.
func runBenchCheck(command *cobra.Command, settings benchSettings) error {
	ctx := command.Context()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConnsPerHost = settings.concurrency
	client := &http.Client{Transport: transport, Timeout: benchRequestTimeout}
	deadline := time.Now().Add(settings.duration)
	results := make([]benchWorkerResult, settings.concurrency)
	failures := make([]error, settings.concurrency)
	var running sync.WaitGroup
	for worker := range settings.concurrency {
		running.Go(func() {
			results[worker], failures[worker] = runBenchWorker(ctx, client, settings, worker, deadline)
		})
	}
	running.Wait()
	if err := errors.Join(append(failures, ctx.Err())...); err != nil {
		return err
	}
	checks, reports := mergeBenchResults(results)
	return printBenchResults(command.OutOrStdout(), checks, reports)
}

func runBenchWorker(ctx context.Context, client *http.Client, settings benchSettings, worker int, deadline time.Time) (benchWorkerResult, error) {
	result := benchWorkerResult{
		checks:  benchOperation{failures: map[string]int{}},
		reports: benchOperation{failures: map[string]int{}},
	}
	for index := worker; ctx.Err() == nil && time.Now().Before(deadline); index += settings.concurrency {
		check := benchCheckRequest{
			CustomerID:    benchCustomerPrefix + strconv.Itoa(index%settings.customers),
			Feature:       benchFeature,
			Provider:      benchProvider,
			Model:         benchModel,
			UsageEstimate: map[string]string{benchMeter: benchQuantity},
		}
		var decision benchCheckResponse
		checked, err := result.checks.send(ctx, client, settings, settings.checkURL, check, http.StatusOK, &decision)
		if err != nil {
			return benchWorkerResult{}, err
		}
		if !checked || decision.Outcome == deniedOutcome {
			continue
		}
		report := benchReportRequest{
			DecisionSource: benchDecisionSource,
			DecisionID:     decision.DecisionID,
			Usage:          map[string]string{benchMeter: benchQuantity},
		}
		if _, err := result.reports.send(ctx, client, settings, settings.reportURL, report, http.StatusAccepted, nil); err != nil {
			return benchWorkerResult{}, err
		}
	}
	return result, nil
}

func (operation *benchOperation) send(ctx context.Context, client *http.Client, settings benchSettings, target string, body any, wantStatus int, response any) (bool, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return false, fmt.Errorf("encode request body: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(encoded))
	if err != nil {
		return false, fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+settings.apiKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	answer, err := client.Do(request)
	if err != nil {
		operation.failures["transport error"]++
		return false, nil
	}
	defer func() { _ = answer.Body.Close() }()
	contents, err := io.ReadAll(answer.Body)
	elapsed := time.Since(started)
	if err != nil {
		operation.failures["transport error"]++
		return false, nil
	}
	if answer.StatusCode != wantStatus {
		operation.failures["status "+strconv.Itoa(answer.StatusCode)]++
		return false, nil
	}
	if response != nil {
		if err := json.Unmarshal(contents, response); err != nil {
			operation.failures["undecodable body"]++
			return false, nil
		}
	}
	operation.latencies = append(operation.latencies, elapsed)
	return true, nil
}

func mergeBenchResults(results []benchWorkerResult) (checks benchOperation, reports benchOperation) {
	checks = benchOperation{failures: map[string]int{}}
	reports = benchOperation{failures: map[string]int{}}
	for _, result := range results {
		checks.latencies = append(checks.latencies, result.checks.latencies...)
		reports.latencies = append(reports.latencies, result.reports.latencies...)
		for cause, count := range result.checks.failures {
			checks.failures[cause] += count
		}
		for cause, count := range result.reports.failures {
			reports.failures[cause] += count
		}
	}
	return checks, reports
}

func printBenchResults(output io.Writer, checks benchOperation, reports benchOperation) error {
	table := tabwriter.NewWriter(output, 0, 0, benchTableColumnGap, ' ', 0)
	if _, err := fmt.Fprintln(table, "operation\trequests\terrors\tp50\tp95\tp99"); err != nil {
		return err
	}
	operations := []struct {
		name      string
		operation benchOperation
	}{{name: "check", operation: checks}, {name: "report", operation: reports}}
	for _, named := range operations {
		if _, err := fmt.Fprintf(table, "%s\t%d\t%d%s\n", named.name, named.operation.requests(), named.operation.errors(), named.operation.percentileColumns()); err != nil {
			return err
		}
	}
	if err := table.Flush(); err != nil {
		return err
	}
	for _, named := range operations {
		for _, cause := range slices.Sorted(maps.Keys(named.operation.failures)) {
			if _, err := fmt.Fprintf(output, "%s errors: %d with %s\n", named.name, named.operation.failures[cause], cause); err != nil {
				return err
			}
		}
	}
	return nil
}

func (operation benchOperation) requests() int {
	return len(operation.latencies) + operation.errors()
}

func (operation benchOperation) errors() int {
	total := 0
	for _, count := range operation.failures {
		total += count
	}
	return total
}

func (operation benchOperation) percentileColumns() string {
	sorted := slices.Sorted(slices.Values(operation.latencies))
	var columns bytes.Buffer
	for _, percentile := range benchPercentiles {
		columns.WriteString("\t")
		if len(sorted) == 0 {
			columns.WriteString("-")
			continue
		}
		rank := int(math.Ceil(percentile*float64(len(sorted)))) - 1
		columns.WriteString(sorted[rank].Round(benchLatencyPrecision).String())
	}
	return columns.String()
}
