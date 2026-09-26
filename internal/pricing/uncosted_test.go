package pricing_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/jobs"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/pricing"
)

const (
	uncostedPath       = "/api/v1/pricing/uncosted"
	unknownImageModel  = "acme-image-1"
	unknownProvider    = "acme"
	uncostedFeature    = "text_to_video"
	correctionKeyStart = "correction:"
)

type uncostedEntryFixture struct {
	environment   httpapi.Environment
	customerID    uuid.UUID
	provider      string
	model         string
	missingMeters []pricing.Meter
	pricedMeters  []pricing.Meter
	occurredAt    time.Time
}

type costBreakdownFixture struct {
	Lines []costLineFixture `json:"lines"`
}

type costLineFixture struct {
	Meter   pricing.Meter `json:"meter"`
	Missing bool          `json:"missing"`
}

func TestUncostedRerateArgsDeclareKindQueueAndUniqueness(t *testing.T) {
	t.Parallel()
	arguments := pricing.UncostedRerateArgs{Environment: httpapi.EnvironmentTest}

	if kind := arguments.Kind(); kind != "uncosted_rerate" {
		t.Errorf("kind = %s, want uncosted_rerate", kind)
	}
	options := arguments.InsertOpts()
	if options.Queue != jobs.QueueLedger {
		t.Errorf("queue = %s, want %s", options.Queue, jobs.QueueLedger)
	}
	wantUnique := river.UniqueOpts{
		ByArgs: true,
		ByState: []rivertype.JobState{
			rivertype.JobStateAvailable,
			rivertype.JobStatePending,
			rivertype.JobStateRetryable,
			rivertype.JobStateRunning,
			rivertype.JobStateScheduled,
		},
	}
	sortStates := cmpopts.SortSlices(func(left, right rivertype.JobState) bool { return left < right })
	if diff := cmp.Diff(wantUnique, options.UniqueOpts, sortStates); diff != "" {
		t.Errorf("unique options mismatch (-want +got):\n%s", diff)
	}
}

func TestRateUncosted(t *testing.T) {
	t.Parallel()
	rerateTime := octoberStart.Add(24 * time.Hour)
	gptInputUntilOctober := effective(catalogRule(1, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(150_000_000)), catalogStart, &octoberStart)
	gptInputFromOctober := effective(catalogRule(2, "openai", "gpt-4o-mini", pricing.MeterInputTokens, nil, perMillionTokens(300_000_000)), octoberStart, nil)
	gptOutputFromOctober := effective(catalogRule(3, "openai", "gpt-4o-mini", pricing.MeterOutputTokens, nil, perMillionTokens(600_000_000)), octoberStart, nil)
	veoOverrideFromOctober := override(11, "google", "veo-3", pricing.MeterOutputSeconds, nil, perUnit(500_000_000))
	veoOverrideFromOctober.EffectiveFrom = octoberStart
	gptUsage := map[pricing.Meter]money.Quantity{pricing.MeterInputTokens: 1_000_000_000, pricing.MeterOutputTokens: 500_000_000}

	tests := []struct {
		name      string
		rules     []pricing.Rule
		overrides []pricing.Override
		request   pricing.RatingRequest
		want      pricing.RatedRequest
	}{
		{
			name:    "a request priced when it occurred keeps that price",
			rules:   []pricing.Rule{gptInputUntilOctober, gptInputFromOctober},
			request: ratingRequest("openai", "gpt-4o-mini", nil, usage(pricing.MeterInputTokens, 1_000_000_000)),
			want:    costed(150_000, pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1)),
		},
		{
			name:      "an override that starts after the request prices its missing meter",
			overrides: []pricing.Override{veoOverrideFromOctober},
			request:   ratingRequest("google", "veo-3", nil, usage(pricing.MeterOutputSeconds, 8_000_000)),
			want:      costed(4_000_000_000, overrideLine(pricing.MeterOutputSeconds, 8_000_000, perUnit(500_000_000), 4_000_000_000, 11)),
		},
		{
			name:    "only missing meters take the current price",
			rules:   []pricing.Rule{gptInputUntilOctober, gptInputFromOctober, gptOutputFromOctober},
			request: ratingRequest("openai", "gpt-4o-mini", nil, gptUsage),
			want: costed(450_000,
				pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1),
				pricedLine(pricing.MeterOutputTokens, 500_000_000, perMillionTokens(600_000_000), 300_000, 3),
			),
		},
		{
			name:    "a meter without a price at either time stays missing",
			rules:   []pricing.Rule{gptInputUntilOctober},
			request: ratingRequest("openai", "gpt-4o-mini", nil, gptUsage),
			want: uncosted(
				pricedLine(pricing.MeterInputTokens, 1_000_000_000, perMillionTokens(150_000_000), 150_000, 1),
				missingLine(pricing.MeterOutputTokens, 500_000_000),
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ruleSet, err := pricing.NewRuleSet(test.rules, test.overrides, nil)
			if err != nil {
				t.Fatalf("NewRuleSet: %v", err)
			}

			got, err := pricing.RateUncosted(test.request, ruleSet, rerateTime)

			if err != nil {
				t.Fatalf("RateUncosted: %v", err)
			}
			if diff := cmp.Diff(test.want, got); diff != "" {
				t.Errorf("rated request mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadRuleSetReadsOverridesTheCacheHasNotSeen(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	assertUnknownModelCost(t, harness.service.RuleSets(), harness.clock.Now(), nil)
	harness.insertUnknownModelOverride(t, httpapi.EnvironmentTest)

	ruleSet, err := harness.service.LoadRuleSet(t.Context(), httpapi.EnvironmentTest)

	if err != nil {
		t.Fatalf("LoadRuleSet: %v", err)
	}
	rated, err := pricing.Rate(pricing.RatingRequest{
		Provider:   unknownProvider,
		Model:      unknownVideoModel,
		Usage:      map[pricing.Meter]money.Quantity{pricing.MeterOutputSeconds: 8_000_000},
		OccurredAt: harness.clock.Now(),
	}, ruleSet)
	if err != nil {
		t.Fatalf("Rate: %v", err)
	}
	assertRatedCost(t, "the loaded rule set", rated.Cost, pointer(money.Amount(4_000_000_000)))
	assertUnknownModelCost(t, harness.service.RuleSets(), harness.clock.Now(), nil)
}

func TestOverrideDigestChangesWithEveryOverrideChange(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	empty := harness.overrideDigest(t, httpapi.EnvironmentTest)
	if again := harness.overrideDigest(t, httpapi.EnvironmentTest); again != empty {
		t.Errorf("digest without a change = %q, want %q", again, empty)
	}

	created := harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	afterCreate := harness.overrideDigest(t, httpapi.EnvironmentTest)
	scheduledEnd := harness.clock.Now().Add(time.Hour).Format(time.RFC3339)
	assertStatus(t, harness.memberRequest(t, http.MethodPatch, overridePath(created["id"]), httpapi.EnvironmentTest, `{"effective_to":"`+scheduledEnd+`"}`), http.StatusOK)
	afterSameInstantChange := harness.overrideDigest(t, httpapi.EnvironmentTest)
	harness.clock.Advance(time.Minute)
	assertStatus(t, harness.memberRequest(t, http.MethodDelete, overridePath(created["id"]), httpapi.EnvironmentTest, ""), http.StatusNoContent)
	afterEnd := harness.overrideDigest(t, httpapi.EnvironmentTest)

	if afterCreate == empty {
		t.Error("digest did not change when an override was created")
	}
	if afterSameInstantChange == afterCreate {
		t.Error("digest did not change when the end changed at the instant the override was created")
	}
	if afterEnd == afterSameInstantChange {
		t.Error("digest did not change when the override ended")
	}
	if live := harness.overrideDigest(t, httpapi.EnvironmentLive); live != empty {
		t.Errorf("live digest = %q, want the digest of no overrides %q", live, empty)
	}
}

func TestListUncostedGroupsByMissingMeter(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	now := harness.clock.Now()
	customerID := harness.insertCustomer(t, httpapi.EnvironmentTest)
	liveCustomerID := harness.insertCustomer(t, httpapi.EnvironmentLive)
	videoOutput := uncostedEntryFixture{
		environment:   httpapi.EnvironmentTest,
		customerID:    customerID,
		provider:      unknownProvider,
		model:         unknownVideoModel,
		missingMeters: []pricing.Meter{pricing.MeterOutputSeconds},
		occurredAt:    now.Add(-2 * time.Hour),
	}
	harness.insertUncostedEntry(t, videoOutput)
	videoBoth := videoOutput
	videoBoth.missingMeters = []pricing.Meter{pricing.MeterInputSeconds, pricing.MeterOutputSeconds}
	videoBoth.occurredAt = now.Add(-time.Hour)
	harness.insertUncostedEntry(t, videoBoth)
	imageWithPricedMeter := videoOutput
	imageWithPricedMeter.model = unknownImageModel
	imageWithPricedMeter.missingMeters = []pricing.Meter{pricing.MeterImages}
	imageWithPricedMeter.pricedMeters = []pricing.Meter{pricing.MeterRequests}
	imageWithPricedMeter.occurredAt = now.Add(-3 * time.Hour)
	harness.insertUncostedEntry(t, imageWithPricedMeter)
	corrected := videoOutput
	corrected.occurredAt = now.Add(-time.Minute)
	harness.insertCorrection(t, httpapi.EnvironmentTest, customerID, harness.insertUncostedEntry(t, corrected))
	outsideWindow := videoOutput
	outsideWindow.occurredAt = now.Add(-pricing.RerateWindow - time.Second)
	harness.insertUncostedEntry(t, outsideWindow)
	atWindowStart := videoOutput
	atWindowStart.model = "acme-video-2"
	atWindowStart.occurredAt = now.Add(-pricing.RerateWindow)
	harness.insertUncostedEntry(t, atWindowStart)
	live := videoOutput
	live.environment = httpapi.EnvironmentLive
	live.customerID = liveCustomerID
	harness.insertUncostedEntry(t, live)

	want := []pricing.UncostedUsage{
		{Provider: unknownProvider, Model: unknownImageModel, Meter: pricing.MeterImages, RequestCount: 1, LastSeenAt: imageWithPricedMeter.occurredAt},
		{Provider: unknownProvider, Model: unknownVideoModel, Meter: pricing.MeterInputSeconds, RequestCount: 1, LastSeenAt: videoBoth.occurredAt},
		{Provider: unknownProvider, Model: unknownVideoModel, Meter: pricing.MeterOutputSeconds, RequestCount: 2, LastSeenAt: videoBoth.occurredAt},
		{Provider: unknownProvider, Model: "acme-video-2", Meter: pricing.MeterOutputSeconds, RequestCount: 1, LastSeenAt: atWindowStart.occurredAt},
	}
	firstPage, cursor, err := harness.service.ListUncosted(t.Context(), httpapi.EnvironmentTest, "", 3)
	if err != nil {
		t.Fatalf("ListUncosted first page: %v", err)
	}
	secondPage, lastCursor, err := harness.service.ListUncosted(t.Context(), httpapi.EnvironmentTest, cursor, 3)
	if err != nil {
		t.Fatalf("ListUncosted second page: %v", err)
	}
	if _, _, err := harness.service.ListUncosted(t.Context(), httpapi.EnvironmentLive, cursor, 3); !errors.Is(err, httpapi.ErrInvalidCursor) {
		t.Errorf("ListUncosted in live with a test cursor error = %v, want ErrInvalidCursor", err)
	}

	if diff := cmp.Diff(want, append(firstPage, secondPage...)); diff != "" {
		t.Errorf("uncosted usage mismatch (-want +got):\n%s", diff)
	}
	if cursor == "" || lastCursor != "" {
		t.Errorf("cursors = %q then %q, want one and then none", cursor, lastCursor)
	}
}

func TestUncostedRouteListsUsageAndRejectsForeignCursor(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	handler := harness.uncostedHandler(t)
	customerID := harness.insertCustomer(t, httpapi.EnvironmentTest)
	occurredAt := harness.clock.Now().Add(-time.Hour)
	harness.insertUncostedEntry(t, uncostedEntryFixture{
		environment:   httpapi.EnvironmentTest,
		customerID:    customerID,
		provider:      unknownProvider,
		model:         unknownVideoModel,
		missingMeters: []pricing.Meter{pricing.MeterOutputSeconds},
		occurredAt:    occurredAt,
	})

	recorder := serveUncosted(t, handler, uncostedPath)

	assertStatus(t, recorder, http.StatusOK)
	wantPage := map[string]any{
		"items": []any{map[string]any{
			"provider":      unknownProvider,
			"model":         unknownVideoModel,
			"meter":         string(pricing.MeterOutputSeconds),
			"request_count": float64(1),
			"last_seen_at":  occurredAt.Format(time.RFC3339),
		}},
		"next_cursor": nil,
	}
	if diff := cmp.Diff(wantPage, decodeBody(t, recorder)); diff != "" {
		t.Errorf("page mismatch (-want +got):\n%s", diff)
	}
	foreignCursor := harness.foreignCursor(t)
	assertProblem(t, serveUncosted(t, handler, uncostedPath+"?cursor="+foreignCursor), http.StatusUnprocessableEntity, "invalid_cursor")
}

func (harness *harness) overrideDigest(t *testing.T, environment httpapi.Environment) string {
	t.Helper()
	digest, err := harness.service.OverrideDigest(t.Context(), environment)
	if err != nil {
		t.Fatalf("OverrideDigest of %s: %v", environment, err)
	}
	return digest
}

func (harness *harness) insertCustomer(t *testing.T, environment httpapi.Environment) uuid.UUID {
	t.Helper()
	customerID := identifiers.New()
	_, err := harness.pool.Exec(t.Context(),
		"INSERT INTO customers (customer_id, environment, external_id, status) VALUES ($1, $2, $3, 'active')",
		customerID, string(environment), "customer-"+customerID.String(),
	)
	if err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	return customerID
}

func (harness *harness) insertUncostedEntry(t *testing.T, entry uncostedEntryFixture) uuid.UUID {
	t.Helper()
	breakdown := costBreakdownFixture{}
	for _, meter := range entry.pricedMeters {
		breakdown.Lines = append(breakdown.Lines, costLineFixture{Meter: meter})
	}
	for _, meter := range entry.missingMeters {
		breakdown.Lines = append(breakdown.Lines, costLineFixture{Meter: meter, Missing: true})
	}
	costBreakdown, err := json.Marshal(breakdown)
	if err != nil {
		t.Fatalf("encode cost breakdown: %v", err)
	}
	ledgerEntryID := identifiers.New()
	_, err = harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, '{}', '{}', NULL, $8, 'uncosted', 'fallback', $9, $10, $9)`,
		ledgerEntryID, string(entry.environment), entry.customerID, ledgerEntryID.String(), uncostedFeature, entry.provider, entry.model,
		costBreakdown, entry.occurredAt, entry.occurredAt.Add(time.Hour),
	)
	if err != nil {
		t.Fatalf("insert uncosted ledger entry: %v", err)
	}
	return ledgerEntryID
}

func (harness *harness) insertCorrection(t *testing.T, environment httpapi.Environment, customerID uuid.UUID, originalID uuid.UUID) {
	t.Helper()
	_, err := harness.pool.Exec(t.Context(),
		`INSERT INTO ledger_entries (ledger_entry_id, environment, customer_id, idempotency_key, feature, provider, model, attributes,
			usage, cost_nanos, cost_breakdown, cost_status, decision_source, period_start, period_end, correction_of, occurred_at)
		SELECT $1, environment, customer_id, $2, feature, provider, model, attributes, usage, 4000000000, '{"lines": []}', 'costed',
			decision_source, period_start, period_end, ledger_entry_id, occurred_at
		FROM ledger_entries
		WHERE environment = $3 AND ledger_entry_id = $4 AND customer_id = $5`,
		identifiers.New(), correctionKeyStart+originalID.String(), string(environment), originalID, customerID,
	)
	if err != nil {
		t.Fatalf("insert correction: %v", err)
	}
}

func (harness *harness) uncostedHandler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logging.New(t.Output(), slog.LevelDebug), testAuthenticator{apiKeys: harness.apiKeys.Authenticator()})
	pricing.RegisterUncostedRoutes(api, harness.service)
	return mux
}

func (harness *harness) foreignCursor(t *testing.T) string {
	t.Helper()
	harness.createOverride(t, httpapi.EnvironmentTest, standaloneBody)
	harness.createOverride(t, httpapi.EnvironmentTest, secondStandaloneBody)
	page := decodeBody(t, harness.memberRequest(t, http.MethodGet, overridesPath+"?limit=1", httpapi.EnvironmentTest, ""))
	cursor, isCursor := page["next_cursor"].(string)
	if !isCursor {
		t.Fatalf("overrides page %v has no next cursor", page)
	}
	return cursor
}

func serveUncosted(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := newRequest(t, http.MethodGet, target, "")
	request.Header.Set(httpapi.EnvironmentHeader, string(httpapi.EnvironmentTest))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
