package dashboard_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/cache/cachetest"
	"github.com/preburn/preburn/internal/clock"
	"github.com/preburn/preburn/internal/dashboard"
	"github.com/preburn/preburn/internal/database/databasetest"
	"github.com/preburn/preburn/internal/decisions"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
	"github.com/preburn/preburn/internal/members"
	"github.com/preburn/preburn/internal/policies"
)

const (
	streamPath             = "/api/v1/dashboard/decisions/stream"
	lastEventIDHeader      = "Last-Event-ID"
	streamWaitTimeout      = 10 * time.Second
	testHeartbeatInterval  = 50 * time.Millisecond
	testEnvironmentQuery   = "?environment=test"
	lastEventIDQueryPrefix = "&last_event_id="
)

type streamHarness struct {
	cache        *cache.Client
	writer       *decisions.StreamWriter
	service      *dashboard.StreamService
	server       *httptest.Server
	sessionToken string
}

type sessionStreamHarness struct {
	*streamHarness
	pool     *pgxpool.Pool
	clock    *clock.Manual
	sessions *members.SessionStore
}

type queryEnvironmentAuthenticator struct{}

type streamEvent struct {
	id   string
	name string
	data string
}

func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	return newStreamHarnessWith(t, queryEnvironmentAuthenticator{}, queryEnvironmentAuthenticator{})
}

func newStreamHarnessWith(t *testing.T, authenticator httpapi.Authenticator, sessions dashboard.SessionChecker) *streamHarness {
	t.Helper()
	cacheClient := cachetest.NewClient(t)
	logger := logging.New(t.Output(), slog.LevelDebug)
	service := dashboard.NewStreamService(cacheClient, sessions, logger, testHeartbeatInterval)
	mux := http.NewServeMux()
	api := httpapi.NewAPI(mux, "test", logger, authenticator)
	dashboard.RegisterStreamRoutes(api, service)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &streamHarness{cache: cacheClient, writer: decisions.NewStreamWriter(cacheClient, logger), service: service, server: server}
}

func newSessionStreamHarness(t *testing.T) *sessionStreamHarness {
	t.Helper()
	pool := databasetest.NewPool(t)
	manualClock := clock.NewManual(testNow)
	sessions := members.NewSessionStore(pool, manualClock)
	authenticator := members.NewSessionAuthenticator(sessions)
	return &sessionStreamHarness{
		streamHarness: newStreamHarnessWith(t, authenticator, authenticator),
		pool:          pool,
		clock:         manualClock,
		sessions:      sessions,
	}
}

func (queryEnvironmentAuthenticator) Authenticate(_ context.Context, request *http.Request, _ httpapi.RouteGroup) (httpapi.Principal, error) {
	environment, err := httpapi.EnvironmentFromHeader(request.Header)
	if err != nil {
		return nil, err
	}
	return httpapi.MemberPrincipal{MemberID: identifiers.New(), Environment: environment}, nil
}

func (queryEnvironmentAuthenticator) CheckSession(context.Context, *http.Request) error {
	return nil
}

func (harness *streamHarness) open(ctx context.Context, t *testing.T, query string, lastEventID string) *bufio.Reader {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, harness.server.URL+streamPath+query, nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	if lastEventID != "" {
		request.Header.Set(lastEventIDHeader, lastEventID)
	}
	if harness.sessionToken != "" {
		request.AddCookie(&http.Cookie{Name: members.SessionCookieName, Value: harness.sessionToken}) //nolint:gosec // G124: a request cookie carries no attributes.
	}
	response, err := harness.server.Client().Do(request)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close stream: %v", err)
		}
	})
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("stream status = %d, want 200, body %s", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "text/event-stream" {
		t.Errorf("content type = %s, want text/event-stream", contentType)
	}
	return bufio.NewReader(response.Body)
}

func (harness *sessionStreamHarness) signIn(ctx context.Context, t *testing.T) uuid.UUID {
	t.Helper()
	memberID := identifiers.New()
	_, err := harness.pool.Exec(ctx,
		"INSERT INTO members (member_id, email, display_name, status) VALUES ($1, 'sam@example.com', 'Sam Rivera', 'active')",
		memberID)
	if err != nil {
		t.Fatalf("insert member: %v", err)
	}
	harness.sessionToken, err = harness.sessions.Start(ctx, memberID, "preburn-test", netip.MustParseAddr("192.0.2.1"))
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	return memberID
}

func (harness *streamHarness) problem(t *testing.T, query string, lastEventID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, streamPath+query, nil)
	if lastEventID != "" {
		request.Header.Set(lastEventIDHeader, lastEventID)
	}
	recorder := httptest.NewRecorder()
	harness.server.Config.Handler.ServeHTTP(recorder, request)
	return recorder
}

func (harness *streamHarness) streamIDs(t *testing.T, environment httpapi.Environment) []string {
	t.Helper()
	messages, err := harness.cache.Redis().XRange(t.Context(), decisions.StreamKey(harness.cache, environment), "-", "+").Result()
	if err != nil {
		t.Fatalf("read stream of %s: %v", environment, err)
	}
	streamIDs := make([]string, len(messages))
	for index, message := range messages {
		streamIDs[index] = message.ID
	}
	return streamIDs
}

func streamEntry(externalID string, outcome policies.Outcome) decisions.StreamEntry {
	return decisions.StreamEntry{
		DecisionID:         identifiers.New(),
		CreatedAt:          testNow,
		CustomerID:         identifiers.New(),
		CustomerExternalID: externalID,
		Feature:            textToVideo,
		RequestedModel:     veoModel,
		Model:              veoModel,
		Outcome:            outcome,
		Reason:             policies.ReasonNoPolicyMatched,
	}
}

func readEvent(t *testing.T, reader *bufio.Reader) streamEvent {
	t.Helper()
	var event streamEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream: %v", err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" && event.id != "" {
			return event
		}
		field, value, _ := strings.Cut(line, ": ")
		switch field {
		case "id":
			event.id = value
		case "event":
			event.name = value
		case "data":
			event.data = value
		}
	}
}

func readHeartbeats(t *testing.T, reader *bufio.Reader, count int) {
	t.Helper()
	for heartbeats := 0; heartbeats < count; {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read stream before heartbeat %d: %v", heartbeats+1, err)
		}
		if strings.HasPrefix(line, ":") {
			heartbeats++
		}
	}
}

func heartbeatsUntilEnd(t *testing.T, reader *bufio.Reader) int {
	t.Helper()
	heartbeats := 0
	for {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			return heartbeats
		}
		if err != nil {
			t.Fatalf("stream still open after %d heartbeats: %v", heartbeats, err)
		}
		if strings.HasPrefix(line, ":") {
			heartbeats++
		}
	}
}

func readEventIDs(t *testing.T, reader *bufio.Reader, count int) []string {
	t.Helper()
	streamIDs := make([]string, count)
	for index := range streamIDs {
		streamIDs[index] = readEvent(t, reader).id
	}
	return streamIDs
}

func decodeDecisionEvent(t *testing.T, event streamEvent) dashboard.DecisionEventResponse {
	t.Helper()
	if event.name != "decision" {
		t.Errorf("event name = %s, want decision", event.name)
	}
	var decision dashboard.DecisionEventResponse
	if err := json.Unmarshal([]byte(event.data), &decision); err != nil {
		t.Fatalf("decode event data %q: %v", event.data, err)
	}
	return decision
}

func TestDecisionStreamDeliversEntriesAppendedAfterConnecting(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
	defer cancel()
	harness.writer.Append(ctx, httpapi.EnvironmentTest, streamEntry("before", policies.OutcomeAllow))
	reader := harness.open(ctx, t, testEnvironmentQuery, "")
	policyID := identifiers.New()
	routed := streamEntry("acme", policies.OutcomeRoute)
	routed.CustomerDisplayName, routed.Model, routed.Reason = pointer("Acme Inc"), klingPro, policies.ReasonPolicyMatched
	routed.EstimatedCost, routed.MatchedPolicyID = amount(1.5), &policyID
	denied := streamEntry("cedar", policies.OutcomeDeny)
	harness.writer.Append(ctx, httpapi.EnvironmentLive, streamEntry("live", policies.OutcomeAllow))
	harness.writer.Append(ctx, httpapi.EnvironmentTest, routed)
	harness.writer.Append(ctx, httpapi.EnvironmentTest, denied)

	first, second := readEvent(t, reader), readEvent(t, reader)

	streamIDs := harness.streamIDs(t, httpapi.EnvironmentTest)
	if first.id != streamIDs[1] || second.id != streamIDs[2] {
		t.Errorf("event ids = %s and %s, want %v without the entry appended before connecting", first.id, second.id, streamIDs[1:])
	}
	want := []dashboard.DecisionEventResponse{
		{
			ID:                  encodedDecision(routed.DecisionID),
			CreatedAt:           testNow,
			CustomerID:          encodedCustomer(routed.CustomerID),
			CustomerExternalID:  "acme",
			CustomerDisplayName: pointer("Acme Inc"),
			Feature:             textToVideo,
			RequestedModel:      veoModel,
			Model:               klingPro,
			Outcome:             policies.OutcomeRoute,
			Reason:              policies.ReasonPolicyMatched,
			EstimatedCost:       pointer("1.500000000"),
			MatchedPolicyID:     pointer(encodedPolicy(policyID)),
		},
		{
			ID:                 encodedDecision(denied.DecisionID),
			CreatedAt:          testNow,
			CustomerID:         encodedCustomer(denied.CustomerID),
			CustomerExternalID: "cedar",
			Feature:            textToVideo,
			RequestedModel:     veoModel,
			Model:              veoModel,
			Outcome:            policies.OutcomeDeny,
			Reason:             policies.ReasonNoPolicyMatched,
		},
	}
	if diff := cmp.Diff(want, []dashboard.DecisionEventResponse{decodeDecisionEvent(t, first), decodeDecisionEvent(t, second)}); diff != "" {
		t.Errorf("decision events mismatch (-want +got):\n%s", diff)
	}
}

func TestDecisionStreamResumesAfterLastEventID(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
	defer cancel()
	for _, externalID := range []string{"acme", "cedar", "lumber"} {
		harness.writer.Append(ctx, httpapi.EnvironmentTest, streamEntry(externalID, policies.OutcomeAllow))
	}
	streamIDs := harness.streamIDs(t, httpapi.EnvironmentTest)

	cases := []struct {
		name        string
		query       string
		lastEventID string
		want        []string
	}{
		{name: "header", query: testEnvironmentQuery, lastEventID: streamIDs[0], want: streamIDs[1:]},
		{name: "query parameter", query: testEnvironmentQuery + lastEventIDQueryPrefix + streamIDs[1], want: streamIDs[2:]},
		{name: "header over query parameter", query: testEnvironmentQuery + lastEventIDQueryPrefix + streamIDs[0], lastEventID: streamIDs[1], want: streamIDs[2:]},
	}
	for _, testCase := range cases {
		reader := harness.open(ctx, t, testCase.query, testCase.lastEventID)

		if diff := cmp.Diff(testCase.want, readEventIDs(t, reader, len(testCase.want))); diff != "" {
			t.Errorf("%s: event ids mismatch (-want +got):\n%s", testCase.name, diff)
		}
	}
}

func TestDecisionStreamSendsHeartbeats(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
	defer cancel()
	reader := harness.open(ctx, t, testEnvironmentQuery, "")

	readHeartbeats(t, reader, 2)
}

func TestDecisionStreamEndsWithinOneHeartbeatOfTheSessionEnding(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		endSession func(ctx context.Context, harness *sessionStreamHarness, memberID uuid.UUID) error
	}{
		{
			name: "logout",
			endSession: func(ctx context.Context, harness *sessionStreamHarness, _ uuid.UUID) error {
				return harness.sessions.End(ctx, harness.sessionToken)
			},
		},
		{
			name: "member disabled",
			endSession: func(ctx context.Context, harness *sessionStreamHarness, memberID uuid.UUID) error {
				_, err := harness.pool.Exec(ctx, "UPDATE members SET status = 'disabled' WHERE member_id = $1", memberID)
				return err
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			harness := newSessionStreamHarness(t)
			ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
			defer cancel()
			memberID := harness.signIn(ctx, t)
			reader := harness.open(ctx, t, testEnvironmentQuery, "")
			readHeartbeats(t, reader, 2)

			if err := testCase.endSession(ctx, harness, memberID); err != nil {
				t.Fatalf("end session: %v", err)
			}

			if heartbeats := heartbeatsUntilEnd(t, reader); heartbeats > 1 {
				t.Errorf("stream sent %d heartbeats after the session ended, want it to end within one", heartbeats)
			}
		})
	}
}

func TestDecisionStreamRejectsInvalidParameters(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)

	cases := []struct {
		query       string
		lastEventID string
		location    string
	}{
		{query: "", location: "query.environment"},
		{query: "?environment=staging", location: "query.environment"},
		{query: "?environment=LIVE", location: "query.environment"},
		{query: testEnvironmentQuery + lastEventIDQueryPrefix + "abc", location: "query.last_event_id"},
		{query: testEnvironmentQuery + lastEventIDQueryPrefix + "1-2-3", location: "query.last_event_id"},
		{query: testEnvironmentQuery + lastEventIDQueryPrefix + "18446744073709551616-0", location: "query.last_event_id"},
		{query: testEnvironmentQuery, lastEventID: "1726488000000", location: "header.Last-Event-ID"},
	}
	for _, testCase := range cases {
		assertProblem(t, harness.problem(t, testCase.query, testCase.lastEventID), http.StatusUnprocessableEntity, "validation_failed", testCase.location)
	}
}

func TestDecisionStreamEndsWhenTheClientDisconnects(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	harness.open(ctx, t, testEnvironmentQuery, "")

	cancel()
	closed := make(chan struct{})
	go func() {
		harness.server.Close()
		close(closed)
	}()

	select {
	case <-closed:
	case <-time.After(streamWaitTimeout):
		t.Fatalf("stream still open %s after the client disconnected", streamWaitTimeout)
	}
}

func TestDecisionStreamStopEndsOpenStreams(t *testing.T) {
	t.Parallel()
	harness := newStreamHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
	defer cancel()
	reader := harness.open(ctx, t, testEnvironmentQuery, "")

	harness.service.Stop()

	if _, err := io.ReadAll(reader); err != nil {
		t.Errorf("read until the stream ends: %v", err)
	}
}

func TestDecisionStreamRecheckKeepsTheSessionExpiry(t *testing.T) {
	t.Parallel()
	harness := newSessionStreamHarness(t)
	ctx, cancel := context.WithTimeout(t.Context(), streamWaitTimeout)
	defer cancel()
	memberID := harness.signIn(ctx, t)
	reader := harness.open(ctx, t, testEnvironmentQuery, "")
	readHeartbeats(t, reader, 1)

	harness.clock.Advance(time.Hour)
	readHeartbeats(t, reader, 3)

	var expiresAt, lastSeenAt time.Time
	err := harness.pool.QueryRow(ctx, "SELECT expires_at, last_seen_at FROM member_sessions WHERE member_id = $1", memberID).Scan(&expiresAt, &lastSeenAt)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	if !expiresAt.Equal(testNow.Add(30*24*time.Hour)) || !lastSeenAt.Equal(testNow) {
		t.Errorf("session expires_at=%s last_seen_at=%s after an hour of heartbeats, want %s and %s", expiresAt, lastSeenAt, testNow.Add(30*24*time.Hour), testNow)
	}
}
