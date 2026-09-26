package signals_test

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/preburn/preburn/internal/signals"
)

func TestNamesAndKinds(t *testing.T) {
	want := []struct {
		name signals.Name
		kind signals.Kind
	}{
		{name: "period_revenue_net", kind: signals.KindMoney},
		{name: "cost_to_date", kind: signals.KindMoney},
		{name: "elapsed_fraction", kind: signals.KindRatio},
		{name: "allowance_remaining", kind: signals.KindMoney},
		{name: "pace", kind: signals.KindRatio},
		{name: "projected_margin", kind: signals.KindRatio},
		{name: "request_estimated_cost", kind: signals.KindMoney},
		{name: "period_decision_count", kind: signals.KindCount},
	}
	names := signals.Names()
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %d names", names, len(want))
	}
	for index, expected := range want {
		if names[index] != expected.name {
			t.Errorf("Names()[%d] = %q, want %q", index, names[index], expected.name)
		}
		kind, known := expected.name.Kind()
		if !known || kind != expected.kind {
			t.Errorf("Name(%q).Kind() = %q, %t, want %q, true", expected.name, kind, known, expected.kind)
		}
	}
}

func TestNamesReturnsACopy(t *testing.T) {
	names := signals.Names()
	names[0] = "changed"
	if signals.Names()[0] != signals.NamePeriodRevenueNet {
		t.Errorf("Names()[0] = %q after the caller changed its slice, want %q", signals.Names()[0], signals.NamePeriodRevenueNet)
	}
}

func TestUnknownNamesHaveNoKind(t *testing.T) {
	for _, name := range []signals.Name{"", "cost_allowance", "reserved", "Pace", "margin"} {
		if kind, known := name.Kind(); known {
			t.Errorf("Name(%q).Kind() = %q, true, want false", name, kind)
		}
	}
}

func TestSignalsValue(t *testing.T) {
	estimate := dollars(3)
	computed := signals.Signals{
		PeriodRevenueNet:     dollars(100),
		CostAllowance:        dollars(60),
		CostToDate:           dollars(10),
		Reserved:             dollars(5),
		ElapsedFraction:      0.5,
		AllowanceRemaining:   dollars(45),
		Pace:                 math.Inf(1),
		ProjectedMargin:      -0.25,
		RequestEstimatedCost: &estimate,
		PeriodDecisionCount:  7,
	}
	want := map[signals.Name]signals.Value{
		signals.NamePeriodRevenueNet:     {Kind: signals.KindMoney, Amount: dollars(100)},
		signals.NameCostToDate:           {Kind: signals.KindMoney, Amount: dollars(10)},
		signals.NameElapsedFraction:      {Kind: signals.KindRatio, Ratio: 0.5},
		signals.NameAllowanceRemaining:   {Kind: signals.KindMoney, Amount: dollars(45)},
		signals.NamePace:                 {Kind: signals.KindRatio, Ratio: math.Inf(1)},
		signals.NameProjectedMargin:      {Kind: signals.KindRatio, Ratio: -0.25},
		signals.NameRequestEstimatedCost: {Kind: signals.KindMoney, Amount: dollars(3)},
		signals.NamePeriodDecisionCount:  {Kind: signals.KindCount, Count: 7},
	}
	for _, name := range signals.Names() {
		got, found := computed.Value(name)
		if !found {
			t.Errorf("Value(%q) found = false, want true", name)
			continue
		}
		if diff := cmp.Diff(want[name], got); diff != "" {
			t.Errorf("Value(%q) mismatch (-want +got):\n%s", name, diff)
		}
	}
}

func TestSignalsValueWithoutRequestEstimate(t *testing.T) {
	computed := signals.Signals{PeriodRevenueNet: dollars(100)}
	if value, found := computed.Value(signals.NameRequestEstimatedCost); found {
		t.Errorf("Value(%q) = %+v, true, want false for a request without a costed estimate", signals.NameRequestEstimatedCost, value)
	}
}

func TestSignalsValueOfUnknownName(t *testing.T) {
	computed := signals.Signals{CostAllowance: dollars(60), Reserved: dollars(5)}
	for _, name := range []signals.Name{"cost_allowance", "reserved", "unknown"} {
		if value, found := computed.Value(name); found {
			t.Errorf("Value(%q) = %+v, true, want false", name, value)
		}
	}
}
