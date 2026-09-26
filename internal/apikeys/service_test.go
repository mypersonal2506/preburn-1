package apikeys_test

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/secrets"
)

var secretPattern = regexp.MustCompile(`^pb_(test|live)_(runtime|admin)_[0-9A-Za-z]{32}$`)

func TestCreateStoresOnlyHashOfSecret(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	for _, environment := range []httpapi.Environment{httpapi.EnvironmentTest, httpapi.EnvironmentLive} {
		for _, scope := range []apikeys.Scope{apikeys.ScopeRuntime, apikeys.ScopeAdmin} {
			key, secret, err := harness.service.Create(t.Context(), environment, testName, scope, &harness.memberID)
			if err != nil {
				t.Fatalf("create %s %s key: %v", environment, scope, err)
			}

			if !secretPattern.MatchString(secret) || !strings.HasPrefix(secret, "pb_"+string(environment)+"_"+string(scope)+"_") {
				t.Errorf("secret %q does not have the form pb_%s_%s_<32 base62>", secret, environment, scope)
			}
			want := apikeys.APIKey{
				ID:                key.ID,
				Environment:       environment,
				Name:              testName,
				Scope:             scope,
				SecretLastFour:    secret[len(secret)-4:],
				Status:            apikeys.StatusActive,
				CreatedByMemberID: &harness.memberID,
				CreatedAt:         testStart,
			}
			if diff := cmp.Diff(want, key); diff != "" {
				t.Errorf("created key mismatch (-want +got):\n%s", diff)
			}
			var secretHash []byte
			var secretLastFour string
			var rowText string
			err = harness.pool.QueryRow(t.Context(), "SELECT secret_hash, secret_last_four, row_to_json(api_keys)::text FROM api_keys WHERE api_key_id = $1", key.ID).Scan(&secretHash, &secretLastFour, &rowText)
			if err != nil {
				t.Fatalf("read key row: %v", err)
			}
			if !bytes.Equal(secretHash, secrets.HashToken(secret)) || secretLastFour != secret[len(secret)-4:] {
				t.Errorf("stored secret_hash=%x secret_last_four=%s, want the SHA-256 and last four of the secret", secretHash, secretLastFour)
			}
			if strings.Contains(rowText, secret) || strings.Contains(rowText, secret[:len(secret)-4]) {
				t.Errorf("stored row %s holds the secret", rowText)
			}
		}
	}
}

func TestCreateValidatesNameAndScope(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	tests := []struct {
		name      string
		keyName   string
		scope     apikeys.Scope
		locations []string
	}{
		{name: "empty name", keyName: "", scope: apikeys.ScopeRuntime, locations: []string{"body.name"}},
		{name: "only spaces", keyName: "   ", scope: apikeys.ScopeRuntime, locations: []string{"body.name"}},
		{name: "81 characters", keyName: strings.Repeat("é", 81), scope: apikeys.ScopeRuntime, locations: []string{"body.name"}},
		{name: "control character", keyName: "Checkout\nservice", scope: apikeys.ScopeRuntime, locations: []string{"body.name"}},
		{name: "unknown scope", keyName: testName, scope: "owner", locations: []string{"body.scope"}},
		{name: "both", keyName: "", scope: "", locations: []string{"body.name", "body.scope"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := harness.service.Create(t.Context(), httpapi.EnvironmentTest, test.keyName, test.scope, nil)

			problem, isProblem := errors.AsType[*httpapi.Problem](err)
			if !isProblem {
				t.Fatalf("Create error = %v, want a validation problem", err)
			}
			var locations []string
			for _, fieldError := range problem.Errors {
				locations = append(locations, fieldError.Location)
			}
			if problem.Code != "validation_failed" || !cmp.Equal(test.locations, locations) {
				t.Errorf("problem code=%s locations=%v, want validation_failed at %v", problem.Code, locations, test.locations)
			}
		})
	}
	if _, _, err := harness.service.Create(t.Context(), httpapi.EnvironmentTest, strings.Repeat("é", 80), apikeys.ScopeAdmin, nil); err != nil {
		t.Errorf("create with an 80 character name: %v", err)
	}
}

func TestListPagesNewestFirstWithinEnvironment(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	var testKeys []apikeys.APIKey
	for range 3 {
		key, _ := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeRuntime)
		testKeys = append(testKeys, key)
	}
	liveKey, _ := harness.createKey(t, httpapi.EnvironmentLive, apikeys.ScopeAdmin)
	harness.clock.Advance(time.Second)
	newestKey, _ := harness.createKey(t, httpapi.EnvironmentTest, apikeys.ScopeAdmin)

	firstPage, cursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, "", 2)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	secondPage, lastCursor, err := harness.service.List(t.Context(), httpapi.EnvironmentTest, cursor, 2)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	livePage, liveCursor, err := harness.service.List(t.Context(), httpapi.EnvironmentLive, "", 0)
	if err != nil {
		t.Fatalf("list live keys: %v", err)
	}

	sortedByNewest := []apikeys.APIKey{newestKey, testKeys[2], testKeys[1], testKeys[0]}
	if diff := cmp.Diff(sortedByNewest[:2], firstPage); diff != "" || cursor == "" {
		t.Errorf("first page cursor=%q mismatch (-want +got):\n%s", cursor, diff)
	}
	if diff := cmp.Diff(sortedByNewest[2:], secondPage); diff != "" || lastCursor != "" {
		t.Errorf("second page cursor=%q mismatch (-want +got):\n%s", lastCursor, diff)
	}
	if diff := cmp.Diff([]apikeys.APIKey{liveKey}, livePage); diff != "" || liveCursor != "" {
		t.Errorf("live page cursor=%q mismatch (-want +got):\n%s", liveCursor, diff)
	}
}
