package customerstate_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/signals"
)

func TestLoadCustomerWithItsOwnPlan(t *testing.T) {
	harness := newHarness(t)
	defaultPlanID := harness.createPlan(t, httpapi.EnvironmentTest, 0, fixedAllowance, map[string]int{"text_to_video": 60})
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlanID)
	planID := harness.createPlan(t, httpapi.EnvironmentTest, marginTarget, withoutAllowance, map[string]int{"text_to_video": 30, "chat": 45})
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, &planID, "active")

	want := signals.CustomerState{
		CustomerID:   customerID,
		PlanID:       &planID,
		PlanMode:     plans.ModeMarginTarget,
		TargetMargin: marginTarget,
		HoldTimes:    map[string]int{"text_to_video": 30, "chat": 45},
		Period:       september,
	}
	if diff := cmp.Diff(want, harness.loadTest(t, customerID)); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadCustomerWithoutPlanUsesTheDefaultPlan(t *testing.T) {
	harness := newHarness(t)
	defaultPlanID := harness.createPlan(t, httpapi.EnvironmentTest, 2500, fixedAllowance, map[string]int{"image_generation": 120})
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlanID)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")

	want := signals.CustomerState{
		CustomerID:   customerID,
		PlanID:       &defaultPlanID,
		PlanMode:     plans.ModeFixedAllowance,
		TargetMargin: 2500,
		Allowance:    *fixedAllowance,
		HoldTimes:    map[string]int{"image_generation": 120},
		Period:       september,
	}
	if diff := cmp.Diff(want, harness.loadTest(t, customerID)); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadCustomerWithoutPlanOrDefaultPlanHasNoAllowance(t *testing.T) {
	harness := newHarness(t)
	liveDefaultPlanID := harness.createPlan(t, httpapi.EnvironmentLive, 0, fixedAllowance, map[string]int{"chat": 60})
	harness.setDefaultPlan(t, httpapi.EnvironmentLive, liveDefaultPlanID)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, september, dollars(100))

	state := harness.loadTest(t, customerID)
	want := signals.CustomerState{
		CustomerID: customerID,
		PlanMode:   plans.ModeFixedAllowance,
		HoldTimes:  map[string]int{},
		Period:     september,
		NetRevenue: dollars(100),
	}
	if diff := cmp.Diff(want, state); diff != "" {
		t.Errorf("Load() mismatch (-want +got):\n%s", diff)
	}
	computed, err := signals.Compute(state, signals.CounterSnapshot{}, nil, harness.clock.Now())
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	if computed.CostAllowance != 0 {
		t.Errorf("CostAllowance = %d, want 0 without a plan or default plan", computed.CostAllowance)
	}
}

func TestLoadRevenueComesFromTheCurrentPeriodRollupOnly(t *testing.T) {
	harness := newHarness(t)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	otherCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, august, dollars(30))
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, september, dollars(50))
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, october, dollars(70))
	harness.insertRollup(t, httpapi.EnvironmentLive, customerID, september, dollars(90))
	harness.insertRollup(t, httpapi.EnvironmentTest, otherCustomerID, september, dollars(11))

	state := harness.loadTest(t, customerID)
	if state.Period != september || state.NetRevenue != dollars(50) {
		t.Errorf("Load() period = %+v net revenue = %d, want %+v and %d", state.Period, state.NetRevenue, september, dollars(50))
	}
}

func TestLoadWithoutRollupHasNoRevenue(t *testing.T) {
	harness := newHarness(t)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, august, dollars(30))

	if state := harness.loadTest(t, customerID); state.NetRevenue != 0 {
		t.Errorf("Load() net revenue = %d, want 0", state.NetRevenue)
	}
}

func TestLoadPeriodFromTheLatestStartingSubscriptionRevenue(t *testing.T) {
	harness := newHarness(t)
	customerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	earlier := signals.Period{Start: time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 2, 0, 0, 0, 0, time.UTC)}
	latest := signals.Period{Start: time.Date(2026, time.September, 10, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)}
	ended := signals.Period{Start: time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC), End: testStart}
	future := signals.Period{Start: testStart.Add(time.Hour), End: time.Date(2026, time.October, 26, 0, 0, 0, 0, time.UTC)}
	adjustment := signals.Period{Start: time.Date(2026, time.September, 20, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC)}
	harness.insertRevenue(t, httpapi.EnvironmentTest, customerID, "subscription", dollars(30), earlier)
	harness.insertRevenue(t, httpapi.EnvironmentTest, customerID, "subscription", dollars(30), latest)
	harness.insertRevenue(t, httpapi.EnvironmentTest, customerID, "subscription", dollars(30), ended)
	harness.insertRevenue(t, httpapi.EnvironmentTest, customerID, "subscription", dollars(30), future)
	harness.insertRevenue(t, httpapi.EnvironmentTest, customerID, "adjustment", dollars(5), adjustment)
	harness.insertRevenue(t, httpapi.EnvironmentLive, customerID, "subscription", dollars(30), adjustment)
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, earlier, dollars(30))
	harness.insertRollup(t, httpapi.EnvironmentTest, customerID, latest, dollars(35))

	state := harness.loadTest(t, customerID)
	if state.Period != latest || state.NetRevenue != dollars(35) {
		t.Errorf("Load() period = %+v net revenue = %d, want %+v and %d", state.Period, state.NetRevenue, latest, dollars(35))
	}
}

func TestLoadUnknownCustomer(t *testing.T) {
	harness := newHarness(t)
	liveCustomerID := harness.createCustomer(t, httpapi.EnvironmentLive, withoutPlan, "active")
	for _, customerID := range []uuid.UUID{identifiers.New(), liveCustomerID} {
		_, err := harness.loader.Load(t.Context(), httpapi.EnvironmentTest, customerID, harness.clock.Now())
		if !errors.Is(err, httpapi.ErrNotFound) {
			t.Errorf("Load(test, %s) error = %v, want %v", customerID, err, httpapi.ErrNotFound)
		}
	}
}

func TestLoadAllMatchesLoadForEveryActiveCustomer(t *testing.T) {
	harness := newHarness(t)
	defaultPlanID := harness.createPlan(t, httpapi.EnvironmentTest, 0, fixedAllowance, map[string]int{"chat": 60})
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlanID)
	marginPlanID := harness.createPlan(t, httpapi.EnvironmentTest, marginTarget, withoutAllowance, withoutHoldTimes)
	subscribed := signals.Period{Start: time.Date(2026, time.September, 10, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)}

	marginCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, marginCustomerID, september, dollars(50))
	harness.insertRollup(t, httpapi.EnvironmentTest, marginCustomerID, august, dollars(20))
	defaultCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	subscribedCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "active")
	harness.insertRevenue(t, httpapi.EnvironmentTest, subscribedCustomerID, "subscription", dollars(40), subscribed)
	harness.insertRollup(t, httpapi.EnvironmentTest, subscribedCustomerID, subscribed, dollars(40))
	harness.insertRollup(t, httpapi.EnvironmentTest, subscribedCustomerID, september, dollars(99))
	disabledCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "disabled")
	harness.insertRollup(t, httpapi.EnvironmentTest, disabledCustomerID, september, dollars(10))
	harness.createCustomer(t, httpapi.EnvironmentLive, withoutPlan, "active")

	states, err := harness.loader.LoadAll(t.Context(), httpapi.EnvironmentTest, harness.clock.Now())
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	activeCustomerIDs := []uuid.UUID{marginCustomerID, defaultCustomerID, subscribedCustomerID}
	slices.SortFunc(activeCustomerIDs, func(first, second uuid.UUID) int { return slices.Compare(first[:], second[:]) })
	want := make([]signals.CustomerState, len(activeCustomerIDs))
	for index, customerID := range activeCustomerIDs {
		want[index] = harness.loadTest(t, customerID)
	}
	if diff := cmp.Diff(want, states); diff != "" {
		t.Errorf("LoadAll() mismatch (-want +got):\n%s", diff)
	}
	if state := harness.loadTest(t, subscribedCustomerID); state.Period != subscribed || state.NetRevenue != dollars(40) {
		t.Errorf("subscribed customer period = %+v net revenue = %d, want %+v and %d", state.Period, state.NetRevenue, subscribed, dollars(40))
	}
}

func TestLoadManyMatchesLoadForTheNamedCustomers(t *testing.T) {
	harness := newHarness(t)
	defaultPlanID := harness.createPlan(t, httpapi.EnvironmentTest, 0, fixedAllowance, map[string]int{"chat": 60})
	harness.setDefaultPlan(t, httpapi.EnvironmentTest, defaultPlanID)
	marginPlanID := harness.createPlan(t, httpapi.EnvironmentTest, marginTarget, withoutAllowance, withoutHoldTimes)
	subscribed := signals.Period{Start: time.Date(2026, time.September, 10, 0, 0, 0, 0, time.UTC), End: time.Date(2026, time.October, 10, 0, 0, 0, 0, time.UTC)}

	marginCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "active")
	harness.insertRollup(t, httpapi.EnvironmentTest, marginCustomerID, september, dollars(50))
	harness.insertRollup(t, httpapi.EnvironmentTest, marginCustomerID, august, dollars(20))
	subscribedCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")
	harness.insertRevenue(t, httpapi.EnvironmentTest, subscribedCustomerID, "subscription", dollars(40), subscribed)
	harness.insertRollup(t, httpapi.EnvironmentTest, subscribedCustomerID, subscribed, dollars(40))
	disabledCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "disabled")
	harness.insertRollup(t, httpapi.EnvironmentTest, disabledCustomerID, september, dollars(10))
	unnamedCustomerID := harness.createCustomer(t, httpapi.EnvironmentTest, &marginPlanID, "active")
	harness.insertRevenue(t, httpapi.EnvironmentTest, unnamedCustomerID, "subscription", dollars(70), subscribed)
	liveCustomerID := harness.createCustomer(t, httpapi.EnvironmentLive, withoutPlan, "active")

	states, err := harness.loader.LoadMany(t.Context(), httpapi.EnvironmentTest, []uuid.UUID{disabledCustomerID, subscribedCustomerID, marginCustomerID, liveCustomerID}, harness.clock.Now())
	if err != nil {
		t.Fatalf("LoadMany() error = %v", err)
	}
	namedCustomerIDs := []uuid.UUID{marginCustomerID, subscribedCustomerID, disabledCustomerID}
	slices.SortFunc(namedCustomerIDs, func(first, second uuid.UUID) int { return slices.Compare(first[:], second[:]) })
	want := make([]signals.CustomerState, len(namedCustomerIDs))
	for index, customerID := range namedCustomerIDs {
		want[index] = harness.loadTest(t, customerID)
	}
	if diff := cmp.Diff(want, states); diff != "" {
		t.Errorf("LoadMany() mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadManyWithoutCustomerIDs(t *testing.T) {
	harness := newHarness(t)
	harness.createCustomer(t, httpapi.EnvironmentTest, withoutPlan, "active")

	states, err := harness.loader.LoadMany(t.Context(), httpapi.EnvironmentTest, nil, harness.clock.Now())
	if err != nil {
		t.Fatalf("LoadMany() error = %v", err)
	}
	if len(states) != 0 {
		t.Errorf("LoadMany() = %+v, want no states", states)
	}
}

func TestLoadAllWithoutCustomers(t *testing.T) {
	harness := newHarness(t)
	states, err := harness.loader.LoadAll(t.Context(), httpapi.EnvironmentTest, harness.clock.Now())
	if err != nil {
		t.Fatalf("LoadAll() error = %v", err)
	}
	if len(states) != 0 {
		t.Errorf("LoadAll() = %+v, want no states", states)
	}
}
