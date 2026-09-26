package installation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/preburn/preburn/internal/cache"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/installation"
	"github.com/preburn/preburn/internal/plans"
)

func TestSettingsUpdatePublishesSettingsInvalidation(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	subscription := subscribeSettingsInvalidations(t, harness.cache)
	metadataKey := "account_id"

	settings, err := harness.service.Update(t.Context(), httpapi.EnvironmentLive, installation.SettingsUpdate{StripeCustomerMetadataKey: &metadataKey})

	if err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if settings.StripeCustomerMetadataKey != metadataKey || settings.InstallationName != "Preburn" || settings.DefaultPlanID != nil {
		t.Errorf("settings = %+v, want the account_id key, the Preburn name and no default plan", settings)
	}
	assertSettingsInvalidation(t, subscription, cache.Invalidation{Kind: cache.InvalidationKindSettings, Environment: "live"})
}

func TestSettingsGetReadsEnvironment(t *testing.T) {
	t.Parallel()
	harness := newSettingsHarness(t)
	plan := harness.createPlan(t, httpapi.EnvironmentLive, "Free")
	if _, err := harness.service.Update(t.Context(), httpapi.EnvironmentLive, installation.SettingsUpdate{ReplaceDefaultPlan: true, DefaultPlanID: &plan.ID}); err != nil {
		t.Fatalf("set default plan: %v", err)
	}

	liveSettings, err := harness.service.Get(t.Context(), httpapi.EnvironmentLive)
	if err != nil {
		t.Fatalf("get live settings: %v", err)
	}
	testSettings, err := harness.service.Get(t.Context(), httpapi.EnvironmentTest)
	if err != nil {
		t.Fatalf("get test settings: %v", err)
	}

	if liveSettings.DefaultPlanID == nil || *liveSettings.DefaultPlanID != plan.ID {
		t.Errorf("live default plan = %v, want %s", liveSettings.DefaultPlanID, plan.ID)
	}
	if testSettings.DefaultPlanID != nil {
		t.Errorf("test default plan = %v, want none", testSettings.DefaultPlanID)
	}
}

func TestSettingsUpdateWaitsForConcurrentArchive(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), settingsWaitTimeout)
	defer cancel()
	harness := newSettingsHarness(t)
	plan := harness.createPlan(t, httpapi.EnvironmentTest, "Free")
	archive, err := harness.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin archive: %v", err)
	}
	defer func() {
		if err := archive.Rollback(context.WithoutCancel(ctx)); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("roll back archive: %v", err)
		}
	}()
	if _, err := archive.Exec(ctx, "UPDATE plans SET status = 'archived' WHERE plan_id = $1", plan.ID); err != nil {
		t.Fatalf("archive plan: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := harness.service.Update(ctx, httpapi.EnvironmentTest, installation.SettingsUpdate{ReplaceDefaultPlan: true, DefaultPlanID: &plan.ID})
		result <- err
	}()

	waitForLockWaiters(ctx, t, harness.pool, 1)
	if err := archive.Commit(ctx); err != nil {
		t.Fatalf("commit archive: %v", err)
	}

	if err := <-result; !errors.Is(err, plans.ErrPlanNotFound) {
		t.Errorf("set default plan during its archive: err = %v, want plans.ErrPlanNotFound", err)
	}
	settings, err := harness.service.Get(t.Context(), httpapi.EnvironmentTest)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if settings.DefaultPlanID != nil {
		t.Errorf("default plan = %s, want none", settings.DefaultPlanID)
	}
}
