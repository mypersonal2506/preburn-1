package apikeys_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/logging"
)

func TestValidKeyAuthenticatesWithEnvironmentAndScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		for _, scope := range []apikeys.Scope{apikeys.ScopeRuntime, apikeys.ScopeAdmin} {
			key, secret := harness.createKey(t, environment, scope)

			recorder := harness.bearerRequest(t, runtimeProbe, secret)

			assertStatus(t, recorder, http.StatusOK)
			want := map[string]any{
				"api_key_id":  identifiers.Encode(identifiers.PrefixAPIKey, key.ID),
				"environment": string(environment),
				"scope":       string(scope),
			}
			if diff := cmp.Diff(want, decodeBody(t, recorder)); diff != "" {
				t.Errorf("principal mismatch (-want +got):\n%s", diff)
			}
		}
	}
	_, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	request := newRequest(t, http.MethodGet, runtimeProbe, "")
	request.Header.Set(authorizationHeader, "bearer "+secret)
	assertStatus(t, harness.serve(request), http.StatusOK)
}

func TestRouteGroupsAdmitKeysByScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	_, runtimeSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)
	_, adminSecret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)
	tests := []struct {
		name   string
		secret string
		target string
		status int
		code   string
	}{
		{name: "runtime key on runtime route", secret: runtimeSecret, target: runtimeProbe, status: http.StatusOK},
		{name: "admin key on runtime route", secret: adminSecret, target: runtimeProbe, status: http.StatusOK},
		{name: "admin key on admin route", secret: adminSecret, target: adminProbe, status: http.StatusOK},
		{name: "runtime key on admin route", secret: runtimeSecret, target: adminProbe, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "runtime key on dashboard route", secret: runtimeSecret, target: keysPath, status: http.StatusForbidden, code: "scope_forbidden"},
		{name: "admin key on dashboard route", secret: adminSecret, target: keysPath, status: http.StatusForbidden, code: "scope_forbidden"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := harness.bearerRequest(t, test.target, test.secret)

			if test.code != "" {
				assertProblem(t, recorder, test.status, test.code)
				return
			}
			assertStatus(t, recorder, test.status)
		})
	}
}

func TestRejectedKeysGetIdenticalProblems(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	revokedKey, revokedSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	if err := harness.service.Revoke(t.Context(), httpapi.EnvironmentTest, revokedKey.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}
	_, validSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	tests := []struct {
		name          string
		authorization []string
	}{
		{name: "revoked", authorization: []string{"Bearer " + revokedSecret}},
		{name: "unknown", authorization: []string{"Bearer " + wellFormedSecret(httpapi.EnvironmentLive, apikeys.ScopeAdmin)}},
		{name: "empty header", authorization: []string{""}},
		{name: "scheme only", authorization: []string{"Bearer"}},
		{name: "empty token", authorization: []string{"Bearer "}},
		{name: "basic scheme", authorization: []string{"Basic " + validSecret}},
		{name: "unknown environment", authorization: []string{"Bearer pb_prod_admin_" + validSecret[len(validSecret)-32:]}},
		{name: "unknown scope", authorization: []string{"Bearer pb_test_owner_" + validSecret[len(validSecret)-32:]}},
		{name: "short random part", authorization: []string{"Bearer " + validSecret[:len(validSecret)-1]}},
		{name: "long random part", authorization: []string{"Bearer " + validSecret + "A"}},
		{name: "character outside base62", authorization: []string{"Bearer " + validSecret[:len(validSecret)-1] + "-"}},
		{name: "trailing space", authorization: []string{"Bearer " + validSecret + " "}},
		{name: "two headers", authorization: []string{"Bearer " + validSecret, "Bearer " + validSecret}},
	}
	var reference *httptest.ResponseRecorder
	for _, test := range tests {
		request := newRequest(t, http.MethodGet, runtimeProbe, "")
		for _, value := range test.authorization {
			request.Header.Add(authorizationHeader, value)
		}

		recorder := harness.serve(request)

		assertProblem(t, recorder, http.StatusUnauthorized, "authentication_required")
		if reference == nil {
			reference = recorder
			continue
		}
		if recorder.Body.String() != reference.Body.String() {
			t.Errorf("%s answered %s, want the revoked key answer %s", test.name, recorder.Body.String(), reference.Body.String())
		}
	}
}

func TestMalformedKeysNeverReachStorage(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	_, validSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	harness.pool.Close()

	malformed := harness.bearerRequest(t, runtimeProbe, validSecret[:len(validSecret)-1])
	wellFormed := harness.bearerRequest(t, runtimeProbe, wellFormedSecret(httpapi.EnvironmentTest, apikeys.ScopeRuntime))

	assertProblem(t, malformed, http.StatusUnauthorized, "authentication_required")
	assertProblem(t, wellFormed, http.StatusInternalServerError, "internal_error")
}

func TestRevokedKeyFailsOnNextRequest(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	created := harness.memberRequest(t, http.MethodPost, keysPath, httpapi.EnvironmentLive, `{"name":"Checkout service","scope":"runtime"}`)
	assertStatus(t, created, http.StatusCreated)
	body := decodeBody(t, created)
	secret := body["secret"].(string)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)

	revoked := harness.memberRequest(t, http.MethodDelete, keysPath+"/"+body["id"].(string), httpapi.EnvironmentLive, "")

	assertStatus(t, revoked, http.StatusNoContent)
	assertProblem(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusUnauthorized, "authentication_required")
}

func TestCachedKeyExpiresAfterThirtySeconds(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	key, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	disableInDatabase(t, harness, key)

	harness.clock.Advance(30*time.Second - time.Nanosecond)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	harness.clock.Advance(time.Nanosecond)
	assertProblem(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusUnauthorized, "authentication_required")
}

func TestRevokeInAnotherProcessClearsCacheThroughInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	key, secret := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeRuntime)
	otherProcess := apikeys.NewService(harness.pool, harness.cache, harness.clock, harness.logger).Authenticator()
	authenticate := func() error {
		request := newRequest(t, http.MethodGet, runtimeProbe, "")
		request.Header.Set(authorizationHeader, "Bearer "+secret)
		_, err := otherProcess.Authenticate(t.Context(), request, httpapi.RouteGroupRuntime)
		return err
	}
	if err := authenticate(); err != nil {
		t.Fatalf("authenticate before revoke: %v", err)
	}
	disableInDatabase(t, harness, key)
	if err := authenticate(); err != nil {
		t.Fatalf("authenticate from the cache: %v", err)
	}
	subscribe(t, harness.cache, otherProcess)

	if err := harness.service.Revoke(t.Context(), httpapi.EnvironmentLive, key.ID); err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	waitFor(t, "key rejected in the other process", func() bool {
		return errors.Is(authenticate(), httpapi.ErrAuthenticationRequired)
	})
}

func TestLookupRacingRevocationIsNotCached(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		revoke func(ctx context.Context, harness *harness, hooked *apikeys.Service, key apikeys.APIKey) error
	}{
		{name: "revoke in this process", revoke: func(ctx context.Context, _ *harness, hooked *apikeys.Service, key apikeys.APIKey) error {
			return hooked.Revoke(ctx, key.Environment, key.ID)
		}},
		{name: "invalidation from another process", revoke: func(ctx context.Context, harness *harness, hooked *apikeys.Service, key apikeys.APIKey) error {
			if err := harness.service.Revoke(ctx, key.Environment, key.ID); err != nil {
				return err
			}
			hooked.Authenticator().Invalidate(cache.Invalidation{
				Kind:        cache.InvalidationKindAPIKey,
				Environment: string(key.Environment),
				ID:          identifiers.Encode(identifiers.PrefixAPIKey, key.ID),
			})
			return nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := newHarness(t)
			hook := &queryHook{queryName: "SelectActiveAPIKeyBySecretHash"}
			hooked := harness.newHookedService(t, hook)
			key, secret, err := hooked.Create(t.Context(), httpapi.EnvironmentTest, testName, apikeys.ScopeRuntime, nil)
			if err != nil {
				t.Fatalf("create key: %v", err)
			}
			hook.run = func(ctx context.Context) {
				if err := test.revoke(context.WithoutCancel(ctx), harness, hooked, key); err != nil {
					t.Errorf("revoke during the lookup: %v", err)
				}
			}
			hook.armed.Store(true)
			authenticate := func() error {
				request := newRequest(t, http.MethodGet, runtimeProbe, "")
				request.Header.Set(authorizationHeader, "Bearer "+secret)
				_, err := hooked.Authenticator().Authenticate(t.Context(), request, httpapi.RouteGroupRuntime)
				return err
			}
			if err := authenticate(); err != nil {
				t.Fatalf("authenticate the request that started before the revoke: %v", err)
			}

			err = authenticate()

			if !errors.Is(err, httpapi.ErrAuthenticationRequired) {
				t.Errorf("authenticate after the revoke error = %v, want ErrAuthenticationRequired", err)
			}
		})
	}
}

func TestInvalidationOfOtherKindsKeepsCache(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	key, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	disableInDatabase(t, harness, key)
	authenticator := harness.service.Authenticator()

	authenticator.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: "test"})
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	authenticator.Invalidate(cache.Invalidation{Kind: cache.InvalidationKindAPIKey, Environment: "test", ID: identifiers.Encode(identifiers.PrefixAPIKey, key.ID)})
	assertProblem(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusUnauthorized, "authentication_required")
}

func TestClearCacheForgetsKeys(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	key, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	disableInDatabase(t, harness, key)

	harness.service.Authenticator().ClearCache()

	assertProblem(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusUnauthorized, "authentication_required")
}

func TestLastUsedWrittenAtMostOncePerMinute(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	key, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	_, otherSecret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)
	if lastUsedAt := harness.lastUsedAt(t, key.ID); lastUsedAt != nil {
		t.Fatalf("last_used_at = %s before first use, want null", lastUsedAt)
	}

	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	assertLastUsedAt(t, harness, key.ID, testStart)

	harness.clock.Advance(time.Minute - time.Second)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, otherSecret), http.StatusOK)
	assertLastUsedAt(t, harness, key.ID, testStart)

	harness.clock.Advance(time.Second)
	assertStatus(t, harness.bearerRequest(t, runtimeProbe, secret), http.StatusOK)
	assertLastUsedAt(t, harness, key.ID, testStart.Add(time.Minute))
}

func TestFailedLastUsedUpdateKeepsRequest(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	_, secret := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
	for _, statement := range []string{
		"CREATE FUNCTION reject_last_used() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'last used rejected'; END $$",
		"CREATE TRIGGER reject_last_used BEFORE UPDATE OF last_used_at ON api_keys FOR EACH ROW EXECUTE FUNCTION reject_last_used()",
	} {
		if _, err := harness.pool.Exec(t.Context(), statement); err != nil {
			t.Fatalf("prepare failing update: %v", err)
		}
	}

	recorder := harness.bearerRequest(t, runtimeProbe, secret)

	assertStatus(t, recorder, http.StatusOK)
	if events := harness.logs.events(t); !slices.Contains(events, string(logging.APIKeysLastUsedUpdateFailed)) {
		t.Errorf("logged events %v, want %s", events, logging.APIKeysLastUsedUpdateFailed)
	}
}

func disableInDatabase(t *testing.T, harness *harness, key apikeys.APIKey) {
	t.Helper()
	if _, err := harness.pool.Exec(t.Context(), "UPDATE api_keys SET status = 'disabled' WHERE api_key_id = $1", key.ID); err != nil {
		t.Fatalf("disable key in the database: %v", err)
	}
}

func assertLastUsedAt(t *testing.T, harness *harness, apiKeyID uuid.UUID, want time.Time) {
	t.Helper()
	lastUsedAt := harness.lastUsedAt(t, apiKeyID)
	if lastUsedAt == nil || !lastUsedAt.Equal(want) {
		t.Errorf("last_used_at = %v, want %s", lastUsedAt, want)
	}
}

func subscribe(t *testing.T, cacheClient *cache.Client, authenticator *apikeys.Authenticator) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		cacheClient.SubscribeInvalidations(ctx, authenticator.Invalidate, authenticator.ClearCache)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
	channel := cacheClient.Key(invalidationChannel)
	waitFor(t, "subscriber on "+channel, func() bool {
		counts, err := cacheClient.Redis().PubSubNumSub(t.Context(), channel).Result()
		if err != nil {
			t.Fatalf("pubsub numsub: %v", err)
		}
		return counts[channel] == 1
	})
}
