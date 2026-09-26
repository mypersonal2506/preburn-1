package signals_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/signals"
)

func TestSignalsMarshalJSON(t *testing.T) {
	estimate := money.Amount(250_000_000)
	tests := []struct {
		name     string
		computed signals.Signals
		want     string
	}{
		{
			name: "costed request with features",
			computed: signals.Signals{
				PeriodRevenueNet:     dollars(100),
				CostAllowance:        dollars(60),
				CostToDate:           dollars(10),
				Reserved:             dollars(5),
				ElapsedFraction:      0.5,
				AllowanceRemaining:   dollars(45),
				Pace:                 (10.0 / 60.0) / 0.5,
				ProjectedMargin:      0.8,
				RequestEstimatedCost: &estimate,
				PeriodDecisionCount:  7,
				Features: map[string]signals.FeatureSignals{
					"text_to_video": {CostToDate: dollars(6), Reserved: dollars(5), DecisionCount: 4},
				},
			},
			want: `{"period_revenue_net":"100.000000000","cost_allowance":"60.000000000",` +
				`"cost_to_date":"10.000000000","reserved":"5.000000000","elapsed_fraction":"0.5000",` +
				`"allowance_remaining":"45.000000000","pace":"0.3333","projected_margin":"0.8000",` +
				`"request_estimated_cost":"0.250000000","period_decision_count":7,` +
				`"features":{"text_to_video":{"cost_to_date":"6.000000000","reserved":"5.000000000","period_decision_count":4}}}`,
		},
		{
			name: "infinite ratios and a request without a costed estimate",
			computed: signals.Signals{
				CostToDate:         dollars(1),
				ElapsedFraction:    signals.MinimumElapsedFraction,
				AllowanceRemaining: dollars(-1),
				Pace:               math.Inf(1),
				ProjectedMargin:    math.Inf(-1),
			},
			want: `{"period_revenue_net":"0.000000000","cost_allowance":"0.000000000",` +
				`"cost_to_date":"1.000000000","reserved":"0.000000000","elapsed_fraction":"0.0100",` +
				`"allowance_remaining":"-1.000000000","pace":"inf","projected_margin":"-inf",` +
				`"request_estimated_cost":null,"period_decision_count":0,"features":{}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := json.Marshal(test.computed)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(encoded) != test.want {
				t.Errorf("json.Marshal() =\n%s\nwant\n%s", encoded, test.want)
			}
		})
	}
}
