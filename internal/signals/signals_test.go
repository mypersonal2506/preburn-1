package signals_test

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/plans"
	"github.com/preburn/preburn/internal/signals"
)

const nanosPerDollar = 1_000_000_000

var (
	september = monthPeriod(time.September)
	midPeriod = time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC)
)

func TestCompute(t *testing.T) {
	quarterDollar := money.Amount(250_000_000)
	tests := []struct {
		name                 string
		state                signals.CustomerState
		counter              signals.CounterSnapshot
		requestEstimatedCost *money.Amount
		now                  time.Time
		want                 signals.Signals
	}{
		{
			name:    "margin target mode",
			state:   marginState(4000, dollars(100)),
			counter: signals.CounterSnapshot{Settled: dollars(10), Reserved: dollars(5), Count: 7},
			now:     midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:    dollars(100),
				CostAllowance:       dollars(60),
				CostToDate:          dollars(10),
				Reserved:            dollars(5),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(45),
				Pace:                (10.0 / 60.0) / 0.5,
				ProjectedMargin:     0.8,
				PeriodDecisionCount: 7,
			},
		},
		{
			name:    "fixed allowance mode ignores revenue and target margin",
			state:   fixedAllowanceState(dollars(20), 5000, dollars(100)),
			counter: signals.CounterSnapshot{Settled: dollars(10), Count: 2},
			now:     midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:    dollars(100),
				CostAllowance:       dollars(20),
				CostToDate:          dollars(10),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(10),
				Pace:                1,
				ProjectedMargin:     0.8,
				PeriodDecisionCount: 2,
			},
		},
		{
			name:    "fixed allowance of zero with cost",
			state:   fixedAllowanceState(0, 0, 0),
			counter: signals.CounterSnapshot{Settled: dollars(1), Reserved: dollars(2), Count: 3},
			now:     midPeriod,
			want: signals.Signals{
				CostToDate:          dollars(1),
				Reserved:            dollars(2),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(-3),
				Pace:                math.Inf(1),
				ProjectedMargin:     math.Inf(-1),
				PeriodDecisionCount: 3,
			},
		},
		{
			name:    "fixed allowance of zero without cost",
			state:   fixedAllowanceState(0, 0, 0),
			counter: signals.CounterSnapshot{Reserved: dollars(2), Count: 1},
			now:     midPeriod,
			want: signals.Signals{
				Reserved:            dollars(2),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(-2),
				Pace:                0,
				ProjectedMargin:     0,
				PeriodDecisionCount: 1,
			},
		},
		{
			name:    "no revenue with cost",
			state:   marginState(4000, 0),
			counter: signals.CounterSnapshot{Settled: dollars(1), Count: 1},
			now:     midPeriod,
			want: signals.Signals{
				CostToDate:          dollars(1),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(-1),
				Pace:                math.Inf(1),
				ProjectedMargin:     math.Inf(-1),
				PeriodDecisionCount: 1,
			},
		},
		{
			name:  "no revenue without cost",
			state: marginState(4000, 0),
			now:   midPeriod,
			want: signals.Signals{
				ElapsedFraction: 0.5,
				Pace:            0,
				ProjectedMargin: 0,
			},
		},
		{
			name:    "negative net revenue floors the allowance at zero",
			state:   marginState(4000, dollars(-50)),
			counter: signals.CounterSnapshot{Reserved: dollars(1)},
			now:     midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:   dollars(-50),
				Reserved:           dollars(1),
				ElapsedFraction:    0.5,
				AllowanceRemaining: dollars(-1),
				Pace:               0,
				ProjectedMargin:    0,
			},
		},
		{
			name:    "elapsed fraction floor at the period start",
			state:   marginState(4000, dollars(100)),
			counter: signals.CounterSnapshot{Settled: dollars(10)},
			now:     september.Start,
			want: signals.Signals{
				PeriodRevenueNet:   dollars(100),
				CostAllowance:      dollars(60),
				CostToDate:         dollars(10),
				ElapsedFraction:    signals.MinimumElapsedFraction,
				AllowanceRemaining: dollars(50),
				Pace:               (10.0 / 60.0) / 0.01,
				ProjectedMargin:    1 - (10.0/0.01)/100.0,
			},
		},
		{
			name:  "elapsed fraction above the floor",
			state: marginState(4000, dollars(100)),
			now:   september.Start.Add(18 * time.Hour),
			want: signals.Signals{
				PeriodRevenueNet:   dollars(100),
				CostAllowance:      dollars(60),
				ElapsedFraction:    0.025,
				AllowanceRemaining: dollars(60),
				ProjectedMargin:    1,
			},
		},
		{
			name:                 "request estimated cost",
			state:                marginState(4000, dollars(100)),
			requestEstimatedCost: &quarterDollar,
			now:                  midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:     dollars(100),
				CostAllowance:        dollars(60),
				ElapsedFraction:      0.5,
				AllowanceRemaining:   dollars(60),
				ProjectedMargin:      1,
				RequestEstimatedCost: &quarterDollar,
			},
		},
		{
			name:  "per-feature values",
			state: fixedAllowanceState(dollars(20), 0, 0),
			counter: signals.CounterSnapshot{
				Settled:  dollars(10),
				Reserved: dollars(5),
				Count:    7,
				Features: map[string]signals.FeatureCounter{
					"text_to_video": {Settled: dollars(6), Reserved: dollars(5), Count: 4},
					"chat":          {Settled: dollars(4), Count: 3},
				},
			},
			now: midPeriod,
			want: signals.Signals{
				CostAllowance:       dollars(20),
				CostToDate:          dollars(10),
				Reserved:            dollars(5),
				ElapsedFraction:     0.5,
				AllowanceRemaining:  dollars(5),
				Pace:                1,
				ProjectedMargin:     math.Inf(-1),
				PeriodDecisionCount: 7,
				Features: map[string]signals.FeatureSignals{
					"text_to_video": {CostToDate: dollars(6), Reserved: dollars(5), DecisionCount: 4},
					"chat":          {CostToDate: dollars(4), DecisionCount: 3},
				},
			},
		},
		{
			name:  "margin allowance rounds down to the nano",
			state: marginState(1234, 7),
			now:   midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:   7,
				CostAllowance:      6,
				ElapsedFraction:    0.5,
				AllowanceRemaining: 6,
				ProjectedMargin:    1,
			},
		},
		{
			name:  "margin allowance of a large revenue stays exact",
			state: marginState(3333, 9_000_000_000_000_000_000),
			now:   midPeriod,
			want: signals.Signals{
				PeriodRevenueNet:   9_000_000_000_000_000_000,
				CostAllowance:      6_000_300_000_000_000_000,
				ElapsedFraction:    0.5,
				AllowanceRemaining: 6_000_300_000_000_000_000,
				ProjectedMargin:    1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := signals.Compute(test.state, test.counter, test.requestEstimatedCost, test.now)
			if err != nil {
				t.Fatalf("Compute() error = %v", err)
			}
			if diff := cmp.Diff(test.want, got, cmpopts.EquateApprox(0, 1e-12), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Compute() mismatch (-want +got):\n%s", diff)
			}
			if math.IsNaN(got.ElapsedFraction) || math.IsNaN(got.Pace) || math.IsNaN(got.ProjectedMargin) {
				t.Errorf("Compute() = %+v, want no NaN field", got)
			}
		})
	}
}

func TestComputeCopiesTheRequestEstimatedCost(t *testing.T) {
	estimate := dollars(1)
	got, err := signals.Compute(marginState(0, 0), signals.CounterSnapshot{}, &estimate, midPeriod)
	if err != nil {
		t.Fatalf("Compute() error = %v", err)
	}
	estimate = dollars(2)
	if *got.RequestEstimatedCost != dollars(1) {
		t.Errorf("RequestEstimatedCost = %d after the caller changed its estimate, want %d", *got.RequestEstimatedCost, dollars(1))
	}
}

func TestComputeOverflow(t *testing.T) {
	tests := []struct {
		name    string
		state   signals.CustomerState
		counter signals.CounterSnapshot
	}{
		{
			name:    "settled and reserved beyond int64",
			state:   fixedAllowanceState(0, 0, 0),
			counter: signals.CounterSnapshot{Settled: math.MaxInt64, Reserved: math.MaxInt64},
		},
		{
			name:    "negative settled above the largest allowance",
			state:   fixedAllowanceState(math.MaxInt64, 0, 0),
			counter: signals.CounterSnapshot{Settled: -1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := signals.Compute(test.state, test.counter, nil, midPeriod)
			if !errors.Is(err, money.ErrOverflow) {
				t.Errorf("Compute() error = %v, want %v", err, money.ErrOverflow)
			}
		})
	}
}

func TestComputeRejectsAnUnknownPlanMode(t *testing.T) {
	state := marginState(4000, dollars(100))
	state.PlanMode = ""
	if _, err := signals.Compute(state, signals.CounterSnapshot{}, nil, midPeriod); err == nil {
		t.Error("Compute() error = nil, want an error for an unknown plan mode")
	}
}

func marginState(targetMargin money.BasisPoints, netRevenue money.Amount) signals.CustomerState {
	planID := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	return signals.CustomerState{
		CustomerID:   uuid.MustParse("01900000-0000-7000-8000-000000000002"),
		PlanID:       &planID,
		PlanMode:     plans.ModeMarginTarget,
		TargetMargin: targetMargin,
		Period:       september,
		NetRevenue:   netRevenue,
	}
}

func fixedAllowanceState(allowance money.Amount, targetMargin money.BasisPoints, netRevenue money.Amount) signals.CustomerState {
	state := marginState(targetMargin, netRevenue)
	state.PlanMode = plans.ModeFixedAllowance
	state.Allowance = allowance
	return state
}

func dollars(amount int64) money.Amount {
	return money.Amount(amount * nanosPerDollar)
}
