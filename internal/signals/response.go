package signals

import (
	"encoding/json"

	"github.com/preburn/preburn/internal/money"
)

// Response is the JSON representation of Signals, which the API returns and
// decisions store.
type Response struct {
	PeriodRevenueNet     string                     `json:"period_revenue_net" doc:"Net revenue attributed to the period in USD."`
	CostAllowance        string                     `json:"cost_allowance" doc:"AI cost the plan allows in the period in USD."`
	CostToDate           string                     `json:"cost_to_date" doc:"Settled AI cost of the period in USD."`
	Reserved             string                     `json:"reserved" doc:"AI cost held by open reservations in USD."`
	ElapsedFraction      string                     `json:"elapsed_fraction" doc:"Elapsed share of the period with 4 decimals, at least 0.0100."`
	AllowanceRemaining   string                     `json:"allowance_remaining" doc:"Cost allowance minus settled and reserved cost in USD, negative once over the allowance."`
	Pace                 string                     `json:"pace" doc:"Share of the allowance spent divided by the elapsed fraction, with 4 decimals, or inf for cost against a zero allowance."`
	ProjectedMargin      string                     `json:"projected_margin" doc:"Margin the period ends with at the current pace, with 4 decimals, or -inf for cost without revenue."`
	RequestEstimatedCost *string                    `json:"request_estimated_cost" doc:"Rated cost of the request's usage estimate in USD, null without a request or when the estimate is uncosted."`
	PeriodDecisionCount  int64                      `json:"period_decision_count" doc:"Decisions in the period."`
	Features             map[string]FeatureResponse `json:"features" doc:"Values per feature."`
}

// FeatureResponse is the JSON representation of FeatureSignals.
type FeatureResponse struct {
	CostToDate          string `json:"cost_to_date" doc:"Settled AI cost of the feature in the period in USD."`
	Reserved            string `json:"reserved" doc:"AI cost held by open reservations of the feature in USD."`
	PeriodDecisionCount int64  `json:"period_decision_count" doc:"Decisions for the feature in the period."`
}

// SchemaName returns SignalsResponse, the name of the schema of Response in
// the OpenAPI document.
func (Response) SchemaName() string {
	return "SignalsResponse"
}

// SchemaName returns FeatureSignalsResponse, the name of the schema of
// FeatureResponse in the OpenAPI document.
func (FeatureResponse) SchemaName() string {
	return "FeatureSignalsResponse"
}

// NewResponse returns the JSON representation of signals: money as amount
// strings with 9 decimals, ratios with 4 decimals or as inf and -inf, and
// counts as integers.
func NewResponse(signals Signals) Response {
	response := Response{
		PeriodRevenueNet:    money.FormatAmount(signals.PeriodRevenueNet),
		CostAllowance:       money.FormatAmount(signals.CostAllowance),
		CostToDate:          money.FormatAmount(signals.CostToDate),
		Reserved:            money.FormatAmount(signals.Reserved),
		ElapsedFraction:     money.FormatSignal(signals.ElapsedFraction),
		AllowanceRemaining:  money.FormatAmount(signals.AllowanceRemaining),
		Pace:                money.FormatSignal(signals.Pace),
		ProjectedMargin:     money.FormatSignal(signals.ProjectedMargin),
		PeriodDecisionCount: signals.PeriodDecisionCount,
		Features:            make(map[string]FeatureResponse, len(signals.Features)),
	}
	if signals.RequestEstimatedCost != nil {
		estimate := money.FormatAmount(*signals.RequestEstimatedCost)
		response.RequestEstimatedCost = &estimate
	}
	for feature, featureSignals := range signals.Features {
		response.Features[feature] = FeatureResponse{
			CostToDate:          money.FormatAmount(featureSignals.CostToDate),
			Reserved:            money.FormatAmount(featureSignals.Reserved),
			PeriodDecisionCount: featureSignals.DecisionCount,
		}
	}
	return response
}

// MarshalJSON writes signals in the form of NewResponse.
func (signals Signals) MarshalJSON() ([]byte, error) {
	return json.Marshal(NewResponse(signals))
}
