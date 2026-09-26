package plans_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
)

func TestCreateAndUpdatePublishPlanInvalidation(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	subscription := subscribeInvalidations(t, harness.cache)

	plan := harness.createPlan(t, httpapi.EnvironmentLive, "Creator")

	want := cache.Invalidation{Kind: cache.InvalidationKindPlan, Environment: "live", ID: identifiers.Encode(identifiers.PrefixPlan, plan.ID)}
	assertInvalidation(t, subscription, want)
	name := "Studio"
	if _, err := harness.service.Update(t.Context(), httpapi.EnvironmentLive, plan.ID, plans.UpdateInput{Name: &name}); err != nil {
		t.Fatalf("update plan: %v", err)
	}
	assertInvalidation(t, subscription, want)
}

func TestCreateReturnsTypedPlan(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	allowance := "2.5"

	plan, err := harness.service.Create(t.Context(), httpapi.EnvironmentTest, plans.CreateInput{
		Name:      "Free",
		Mode:      plans.ModeFixedAllowance,
		Allowance: &allowance,
		HoldTimes: map[string]int{"text_to_video": 600},
	})

	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if plan.Mode != plans.ModeFixedAllowance || plan.Allowance == nil || *plan.Allowance != money.Amount(2_500_000_000) || plan.TargetMargin != 0 {
		t.Errorf("plan = %+v, want fixed allowance mode with 2.5 USD and a target margin of 0", plan)
	}
	if plan.Environment != httpapi.EnvironmentTest || plan.Status != plans.StatusActive || plan.HoldTimes["text_to_video"] != 600 || !plan.CreatedAt.Equal(testStart) {
		t.Errorf("plan = %+v, want an active test plan created at %s holding text_to_video for 600 seconds", plan, testStart)
	}
	fetched, err := harness.service.Get(t.Context(), httpapi.EnvironmentTest, plan.ID)
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if fetched.Name != "Free" || fetched.Allowance == nil || *fetched.Allowance != *plan.Allowance {
		t.Errorf("fetched plan = %+v, want the created plan", fetched)
	}
}

func TestCreateRejectsUnknownMode(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)

	_, err := harness.service.Create(t.Context(), httpapi.EnvironmentTest, plans.CreateInput{Name: "Free", Mode: plans.Mode("free")})

	problem, isProblem := errors.AsType[*httpapi.Problem](err)
	if !isProblem || len(problem.Errors) != 1 || problem.Errors[0].Location != "body.mode" {
		t.Errorf("create with an unknown mode: err = %v, want a validation problem at body.mode", err)
	}
}

func TestFeaturePatternMatchesFeatureNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		feature string
		valid   bool
	}{
		{feature: "video_generation", valid: true},
		{feature: "chat2", valid: true},
		{feature: "a" + strings.Repeat("b", 63), valid: true},
		{feature: "a" + strings.Repeat("b", 64), valid: false},
		{feature: "", valid: false},
		{feature: "2chat", valid: false},
		{feature: "_chat", valid: false},
		{feature: "Chat", valid: false},
		{feature: "chat-bot", valid: false},
	}
	for _, test := range tests {
		if valid := plans.FeaturePattern.MatchString(test.feature); valid != test.valid {
			t.Errorf("FeaturePattern matches %q = %t, want %t", test.feature, valid, test.valid)
		}
	}
}

func TestUpdateUnknownPlanIsNotFound(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	name := "Studio"

	_, err := harness.service.Update(t.Context(), httpapi.EnvironmentTest, identifiers.New(), plans.UpdateInput{Name: &name})

	if !errors.Is(err, httpapi.ErrNotFound) {
		t.Errorf("update an unknown plan: err = %v, want httpapi.ErrNotFound", err)
	}
}

func TestListRejectsCursorOfAnotherListing(t *testing.T) {
	t.Parallel()
	harness := newHarness(t)
	cursor, err := httpapi.NewCursor[struct{}]("api_keys").Encode(httpapi.EnvironmentTest, struct{}{})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	_, _, err = harness.service.List(t.Context(), httpapi.EnvironmentTest, cursor, 0)

	if !errors.Is(err, httpapi.ErrInvalidCursor) {
		t.Errorf("list with a cursor of another listing: err = %v, want httpapi.ErrInvalidCursor", err)
	}
}

func TestArchiveWaitsForConcurrentDefaultPlanChange(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	harness := newHarness(t)
	plan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	settingsChange, err := harness.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin settings change: %v", err)
	}
	defer func() {
		if err := settingsChange.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back settings change: %v", err)
		}
	}()
	if _, err := settingsChange.Exec(ctx, "SELECT FROM plans WHERE plan_id = $1 FOR SHARE", plan.ID); err != nil {
		t.Fatalf("lock plan for share: %v", err)
	}
	if _, err := settingsChange.Exec(ctx, "UPDATE environment_settings SET default_plan_id = $1 WHERE environment = 'test'", plan.ID); err != nil {
		t.Fatalf("set default plan: %v", err)
	}
	archived := plans.StatusArchived
	result := make(chan error, 1)
	go func() {
		_, err := harness.service.Update(ctx, httpapi.EnvironmentTest, plan.ID, plans.UpdateInput{Status: &archived})
		result <- err
	}()

	waitForLockWaiters(ctx, t, harness.pool, 1)
	if err := settingsChange.Commit(ctx); err != nil {
		t.Fatalf("commit settings change: %v", err)
	}

	if err := <-result; !errors.Is(err, plans.ErrIsDefault) {
		t.Errorf("archive during a default plan change: err = %v, want plans.ErrIsDefault", err)
	}
	if status := harness.planStatus(t, plan.ID); status != "active" {
		t.Errorf("status of the default plan = %s, want active", status)
	}
}
