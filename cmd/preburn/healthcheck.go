package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

const (
	urlFlag            = "url"
	healthcheckTimeout = 3 * time.Second
)

func newHealthcheckCommand() *cobra.Command {
	var probeURL string
	healthcheck := &cobra.Command{
		Use:    "healthcheck",
		Short:  "GET a URL and exit 0 on a 2xx answer, for container healthchecks",
		Args:   noArguments,
		Hidden: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := requiredFlag(urlFlag, probeURL); err != nil {
				return err
			}
			parsed, err := url.Parse(probeURL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
				return fmt.Errorf("%w: --%s must be an http or https URL with a host", errInvalidInput, urlFlag)
			}
			return runHealthcheck(command, parsed.String())
		},
	}
	healthcheck.Flags().StringVar(&probeURL, urlFlag, "", "URL to GET, such as http://127.0.0.1:8080/readyz (required)")
	return healthcheck
}

//nolint:contextcheck // cobra hands commands their context through command.Context(), which the flag closures of RunE cannot take as a parameter.
func runHealthcheck(command *cobra.Command, probeURL string) error {
	ctx, cancel := context.WithTimeout(command.Context(), healthcheckTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("get %s: status %d", probeURL, response.StatusCode)
	}
	return nil
}
