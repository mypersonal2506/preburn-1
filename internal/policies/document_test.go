package policies_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

func TestDecodeDocumentReadsEveryField(t *testing.T) {
	t.Parallel()
	body := fmt.Sprintf(`{
		"name": "Route heavy video",
		"level": "plan",
		"plan_id": %q,
		"customer_id": null,
		"feature": "text_to_video",
		"when": {"all": [
			{"signal": "pace", "operator": "gt", "value": "1.5"},
			{"any": [
				{"signal": "allowance_remaining", "operator": "lte", "value": "-0.25"},
				{"signal": "period_decision_count", "operator": "gte", "value": "100"}
			]}
		]},
		"action": {
			"outcome": "route",
			"route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1/lite"}, {"provider": "runwayml", "model": "gen4_turbo"}],
			"overrides": {"duration": "4s", "audio": false, "steps": 20},
			"limit": null
		},
		"enforcement": "hard",
		"on_unreachable": "deny",
		"on_uncosted": "allow",
		"status": "disabled"
	}`, identifiers.Encode(identifiers.PrefixPlan, knownPlanID))

	document, fieldErrors := policies.DecodeDocument([]byte(body))

	if len(fieldErrors) > 0 {
		t.Fatalf("DecodeDocument reported %v, want none", fieldErrors)
	}
	want := policies.Document{
		Name:    "Route heavy video",
		Scope:   policies.Scope{Level: policies.LevelPlan, PlanID: &knownPlanID},
		Feature: new("text_to_video"),
		When: policies.ConditionGroup{Quantifier: policies.QuantifierAll, Members: []policies.GroupMember{
			policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: 1.5}},
			policies.ConditionGroup{Quantifier: policies.QuantifierAny, Members: []policies.GroupMember{
				policies.Condition{Signal: signals.NameAllowanceRemaining, Operator: policies.OperatorLessThanOrEqual, Value: signals.Value{Kind: signals.KindMoney, Amount: -250_000_000}},
				policies.Condition{Signal: signals.NamePeriodDecisionCount, Operator: policies.OperatorGreaterThanOrEqual, Value: signals.Value{Kind: signals.KindCount, Count: 100}},
			}},
		}},
		Action: policies.Action{
			Outcome: policies.OutcomeRoute,
			RouteChain: []policies.RouteTarget{
				{Provider: "fal_ai", Model: "fal-ai/veo3.1/lite"},
				{Provider: "runwayml", Model: "gen4_turbo"},
			},
			Overrides: map[string]policies.OverrideValue{
				"duration": {Type: catalogfiles.ValueTypeString, String: "4s"},
				"audio":    {Type: catalogfiles.ValueTypeBoolean, Boolean: false},
				"steps":    {Type: catalogfiles.ValueTypeInteger, Integer: 20},
			},
		},
		Enforcement:   policies.EnforcementHard,
		OnUnreachable: policies.OutcomeDeny,
		OnUncosted:    policies.OutcomeAllow,
		Status:        policies.StatusDisabled,
	}
	if diff := cmp.Diff(want, document); diff != "" {
		t.Errorf("DecodeDocument mismatch (-want +got):\n%s", diff)
	}
}

func TestDecodeDocumentReadsLimitsAndCustomerScope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		limit     string
		wantLimit policies.Limit
	}{
		{name: "count", limit: `{"kind": "count", "value": "3"}`, wantLimit: policies.Limit{Kind: policies.LimitKindCount, Count: 3}},
		{name: "zero count", limit: `{"kind": "count", "value": "0"}`, wantLimit: policies.Limit{Kind: policies.LimitKindCount, Count: 0}},
		{name: "amount", limit: `{"kind": "amount", "value": "25.5"}`, wantLimit: policies.Limit{Kind: policies.LimitKindAmount, Amount: 25_500_000_000}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := documentJSON(map[string]string{
				"level":       `"customer"`,
				"customer_id": quoted(identifiers.Encode(identifiers.PrefixCustomer, knownCustomerID)),
				"action":      `{"outcome": "cap", "limit": ` + test.limit + `}`,
			})

			document, fieldErrors := policies.DecodeDocument([]byte(body))

			if len(fieldErrors) > 0 {
				t.Fatalf("DecodeDocument reported %v, want none", fieldErrors)
			}
			wantScope := policies.Scope{Level: policies.LevelCustomer, CustomerID: &knownCustomerID}
			if diff := cmp.Diff(wantScope, document.Scope); diff != "" {
				t.Errorf("scope mismatch (-want +got):\n%s", diff)
			}
			wantAction := policies.Action{Outcome: policies.OutcomeCap, Limit: &test.wantLimit}
			if diff := cmp.Diff(wantAction, document.Action); diff != "" {
				t.Errorf("action mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDecodeDocumentReportsEachProblemAtItsPath(t *testing.T) {
	t.Parallel()
	condition := func(signal string, value string) string {
		return `{"all": [{"signal": ` + quoted(signal) + `, "operator": "gt", "value": ` + value + `}]}`
	}
	tests := []struct {
		name      string
		body      string
		wantPaths []string
	}{
		{name: "not JSON", body: `{"name": `, wantPaths: []string{""}},
		{name: "content after the document", body: documentJSON(nil) + ` {}`, wantPaths: []string{""}},
		{name: "array", body: `[]`, wantPaths: []string{""}},
		{name: "null", body: `null`, wantPaths: []string{""}},
		{name: "missing name", body: documentJSON(map[string]string{"name": omitted}), wantPaths: []string{"name"}},
		{name: "name as a number", body: documentJSON(map[string]string{"name": `12`}), wantPaths: []string{"name"}},
		{name: "server assigned field", body: documentJSON(map[string]string{"policy_id": `"pol_x"`}), wantPaths: []string{"policy_id"}},
		{name: "unexpected field", body: documentJSON(map[string]string{"priority": `1`}), wantPaths: []string{"priority"}},
		{name: "missing level", body: documentJSON(map[string]string{"level": omitted}), wantPaths: []string{"level"}},
		{name: "plan id of a customer", body: documentJSON(map[string]string{"plan_id": quoted(identifiers.Encode(identifiers.PrefixCustomer, knownCustomerID))}), wantPaths: []string{"plan_id"}},
		{name: "plan id as a number", body: documentJSON(map[string]string{"plan_id": `7`}), wantPaths: []string{"plan_id"}},
		{name: "malformed customer id", body: documentJSON(map[string]string{"customer_id": `"cust_1"`}), wantPaths: []string{"customer_id"}},
		{name: "feature as a number", body: documentJSON(map[string]string{"feature": `3`}), wantPaths: []string{"feature"}},
		{name: "missing when", body: documentJSON(map[string]string{"when": omitted}), wantPaths: []string{"when"}},
		{name: "when as an array", body: documentJSON(map[string]string{"when": `[]`}), wantPaths: []string{"when"}},
		{name: "group without all or any", body: documentJSON(map[string]string{"when": `{}`}), wantPaths: []string{"when"}},
		{name: "group with all and any", body: documentJSON(map[string]string{"when": `{"all": [], "any": []}`}), wantPaths: []string{"when"}},
		{name: "group with another field", body: documentJSON(map[string]string{"when": `{"all": [], "mode": "strict"}`}), wantPaths: []string{"when.mode"}},
		{name: "members as an object", body: documentJSON(map[string]string{"when": `{"all": {}}`}), wantPaths: []string{"when.all"}},
		{name: "member as a string", body: documentJSON(map[string]string{"when": `{"all": ["pace"]}`}), wantPaths: []string{"when.all[0]"}},
		{name: "condition with another field", body: documentJSON(map[string]string{"when": `{"all": [{"signal": "pace", "operator": "gt", "value": "1", "note": "x"}]}`}), wantPaths: []string{"when.all[0].note"}},
		{name: "condition without a signal", body: documentJSON(map[string]string{"when": `{"all": [{"operator": "gt", "value": "1"}]}`}), wantPaths: []string{"when.all[0].signal"}},
		{name: "condition without an operator", body: documentJSON(map[string]string{"when": `{"all": [{"signal": "pace", "value": "1"}]}`}), wantPaths: []string{"when.all[0].operator"}},
		{name: "condition without a value", body: documentJSON(map[string]string{"when": `{"all": [{"signal": "pace", "operator": "gt"}]}`}), wantPaths: []string{"when.all[0].value"}},
		{name: "money value with two dots", body: documentJSON(map[string]string{"when": condition("cost_to_date", `"1.2.3"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "money value with 10 decimals", body: documentJSON(map[string]string{"when": condition("cost_to_date", `"1.0000000001"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "money value as a JSON number", body: documentJSON(map[string]string{"when": condition("cost_to_date", `1.5`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "money value inf", body: documentJSON(map[string]string{"when": condition("allowance_remaining", `"inf"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "ratio value with 5 decimals", body: documentJSON(map[string]string{"when": condition("pace", `"0.12345"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "ratio value inf", body: documentJSON(map[string]string{"when": condition("pace", `"inf"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "ratio value -inf", body: documentJSON(map[string]string{"when": condition("projected_margin", `"-inf"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "ratio value as a JSON number", body: documentJSON(map[string]string{"when": condition("pace", `1.5`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "count value with decimals", body: documentJSON(map[string]string{"when": condition("period_decision_count", `"1.5"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "negative count value", body: documentJSON(map[string]string{"when": condition("period_decision_count", `"-1"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "count value with a plus sign", body: documentJSON(map[string]string{"when": condition("period_decision_count", `"+1"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "count value with a leading zero", body: documentJSON(map[string]string{"when": condition("period_decision_count", `"07"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "count value above int64", body: documentJSON(map[string]string{"when": condition("period_decision_count", `"9223372036854775808"`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "count value as a JSON number", body: documentJSON(map[string]string{"when": condition("period_decision_count", `3`)}), wantPaths: []string{"when.all[0].value"}},
		{name: "missing action", body: documentJSON(map[string]string{"action": omitted}), wantPaths: []string{"action"}},
		{name: "action as a string", body: documentJSON(map[string]string{"action": `"deny"`}), wantPaths: []string{"action"}},
		{name: "action without an outcome", body: documentJSON(map[string]string{"action": `{}`}), wantPaths: []string{"action.outcome"}},
		{name: "action with another field", body: documentJSON(map[string]string{"action": `{"outcome": "deny", "message": "no"}`}), wantPaths: []string{"action.message"}},
		{name: "route chain as an object", body: documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": {}}`}), wantPaths: []string{"action.route_chain"}},
		{name: "route target as a string", body: documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": ["fal_ai"]}`}), wantPaths: []string{"action.route_chain[0]"}},
		{name: "route target without a model", body: documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai"}]}`}), wantPaths: []string{"action.route_chain[0].model"}},
		{name: "route target with another field", body: documentJSON(map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "m", "weight": 1}]}`}), wantPaths: []string{"action.route_chain[0].weight"}},
		{name: "overrides as an array", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "overrides": []}`}), wantPaths: []string{"action.overrides"}},
		{name: "override as a fraction", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": 5.5}}`}), wantPaths: []string{"action.overrides.duration"}},
		{name: "override as an exponent", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": 5e1}}`}), wantPaths: []string{"action.overrides.duration"}},
		{name: "override as null", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": null}}`}), wantPaths: []string{"action.overrides.duration"}},
		{name: "override as an object", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": {"seconds": 5}}}`}), wantPaths: []string{"action.overrides.duration"}},
		{name: "limit as a number", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": 3}`}), wantPaths: []string{"action.limit"}},
		{name: "limit without a kind", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": {"value": "3"}}`}), wantPaths: []string{"action.limit.kind"}},
		{name: "count limit with decimals", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "count", "value": "1.5"}}`}), wantPaths: []string{"action.limit.value"}},
		{name: "count limit as a JSON number", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "count", "value": 3}}`}), wantPaths: []string{"action.limit.value"}},
		{name: "negative amount limit", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "amount", "value": "-1"}}`}), wantPaths: []string{"action.limit.value"}},
		{name: "limit with another field", body: documentJSON(map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "count", "value": "3", "period": "day"}}`}), wantPaths: []string{"action.limit.period"}},
		{name: "missing enforcement", body: documentJSON(map[string]string{"enforcement": omitted}), wantPaths: []string{"enforcement"}},
		{name: "on_unreachable as a boolean", body: documentJSON(map[string]string{"on_unreachable": `true`}), wantPaths: []string{"on_unreachable"}},
		{name: "missing on_uncosted", body: documentJSON(map[string]string{"on_uncosted": omitted}), wantPaths: []string{"on_uncosted"}},
		{name: "null status", body: documentJSON(map[string]string{"status": `null`}), wantPaths: []string{"status"}},
		{
			name: "several problems",
			body: documentJSON(map[string]string{
				"name":   omitted,
				"extra":  `1`,
				"when":   `{"any": [{"signal": "pace", "operator": "gt", "value": "inf"}, 3, {"all": [{"signal": "cost_to_date", "operator": "lt", "value": 1}]}]}`,
				"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai"}], "overrides": {"steps": 1.5}}`,
				"status": `3`,
			}),
			wantPaths: []string{
				"extra",
				"name",
				"when.any[0].value",
				"when.any[1]",
				"when.any[2].all[0].value",
				"action.route_chain[0].model",
				"action.overrides.steps",
				"status",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, fieldErrors := policies.DecodeDocument([]byte(test.body))

			if diff := cmp.Diff(test.wantPaths, fieldErrorPaths(fieldErrors)); diff != "" {
				t.Errorf("DecodeDocument paths mismatch (-want +got):\n%s\nerrors: %v", diff, fieldErrors)
			}
			assertMessages(t, fieldErrors)
		})
	}
}

func TestDecodeDocumentMessagesNeverRepeatTheRejectedValue(t *testing.T) {
	t.Parallel()
	const rejected = "zqxj_rejected_value"
	body := documentJSON(map[string]string{
		"name":           quoted(strings.Repeat(rejected, 10)),
		"level":          quoted(rejected),
		"plan_id":        quoted(rejected),
		"feature":        quoted(rejected),
		"when":           `{"all": [{"signal": "pace", "operator": ` + quoted(rejected) + `, "value": ` + quoted(rejected) + `}, {"signal": ` + quoted(rejected) + `, "operator": "gt", "value": "1"}]}`,
		"action":         `{"outcome": "cap", "overrides": {"duration": ` + quoted(rejected) + `}, "limit": {"kind": "amount", "value": ` + quoted(rejected) + `}}`,
		"enforcement":    quoted(rejected),
		"on_unreachable": quoted(rejected),
		"status":         quoted(rejected),
	})

	document, decodeErrors := policies.DecodeDocument([]byte(body))
	validationErrors := policies.Validate(policies.Policy{Document: document}, testValidationContext)

	fieldErrors := slices.Concat(decodeErrors, validationErrors)
	if len(fieldErrors) < 10 {
		t.Fatalf("got %d errors, want one per rejected field: %v", len(fieldErrors), fieldErrors)
	}
	for _, fieldError := range fieldErrors {
		if strings.Contains(fieldError.Message, rejected) {
			t.Errorf("error at %q repeats the rejected value: %q", fieldError.Path, fieldError.Message)
		}
	}
}

func TestConditionGroupRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	when := policies.ConditionGroup{Quantifier: policies.QuantifierAll, Members: []policies.GroupMember{
		policies.Condition{Signal: signals.NameCostToDate, Operator: policies.OperatorGreaterThanOrEqual, Value: signals.Value{Kind: signals.KindMoney, Amount: 2_500_000_000}},
		policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: 1.5}},
		policies.Condition{Signal: signals.NameProjectedMargin, Operator: policies.OperatorLessThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: -0.25}},
		policies.ConditionGroup{Quantifier: policies.QuantifierAny, Members: []policies.GroupMember{
			policies.Condition{Signal: signals.NamePeriodDecisionCount, Operator: policies.OperatorNotEqual, Value: signals.Value{Kind: signals.KindCount, Count: 100}},
		}},
		policies.ConditionGroup{Quantifier: policies.QuantifierAll},
	}}

	encoded, err := json.Marshal(when)

	if err != nil {
		t.Fatalf("marshal condition group: %v", err)
	}
	wantJSON := `{"all":[` +
		`{"signal":"cost_to_date","operator":"gte","value":"2.500000000"},` +
		`{"signal":"pace","operator":"gt","value":"1.5000"},` +
		`{"signal":"projected_margin","operator":"lt","value":"-0.2500"},` +
		`{"any":[{"signal":"period_decision_count","operator":"ne","value":"100"}]},` +
		`{"all":[]}]}`
	if string(encoded) != wantJSON {
		t.Errorf("marshal condition group = %s, want %s", encoded, wantJSON)
	}
	var decoded policies.ConditionGroup
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal condition group: %v", err)
	}
	if diff := cmp.Diff(when, decoded, cmpopts.EquateEmpty()); diff != "" {
		t.Errorf("round trip mismatch (-want +got):\n%s", diff)
	}
}

func TestActionRoundTripsThroughJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   policies.Action
		wantJSON string
	}{
		{
			name:     "deny",
			action:   policies.Action{Outcome: policies.OutcomeDeny},
			wantJSON: `{"outcome":"deny","route_chain":null,"overrides":null,"limit":null}`,
		},
		{
			name: "route",
			action: policies.Action{
				Outcome:    policies.OutcomeRoute,
				RouteChain: []policies.RouteTarget{{Provider: "fal_ai", Model: "fal-ai/veo3.1/lite"}},
				Overrides: map[string]policies.OverrideValue{
					"audio":    {Type: catalogfiles.ValueTypeBoolean, Boolean: true},
					"duration": {Type: catalogfiles.ValueTypeString, String: "4s"},
					"steps":    {Type: catalogfiles.ValueTypeInteger, Integer: 20},
				},
			},
			wantJSON: `{"outcome":"route","route_chain":[{"provider":"fal_ai","model":"fal-ai/veo3.1/lite"}],"overrides":{"audio":true,"duration":"4s","steps":20},"limit":null}`,
		},
		{
			name:     "cap with a count limit",
			action:   policies.Action{Outcome: policies.OutcomeCap, Limit: &policies.Limit{Kind: policies.LimitKindCount, Count: 3}},
			wantJSON: `{"outcome":"cap","route_chain":null,"overrides":null,"limit":{"kind":"count","value":"3"}}`,
		},
		{
			name: "cap with overrides and an amount limit",
			action: policies.Action{
				Outcome:   policies.OutcomeCap,
				Overrides: map[string]policies.OverrideValue{"duration": {Type: catalogfiles.ValueTypeInteger, Integer: 5}},
				Limit:     &policies.Limit{Kind: policies.LimitKindAmount, Amount: money.Amount(2_000_000_000)},
			},
			wantJSON: `{"outcome":"cap","route_chain":null,"overrides":{"duration":5},"limit":{"kind":"amount","value":"2.000000000"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(test.action)

			if err != nil {
				t.Fatalf("marshal action: %v", err)
			}
			if string(encoded) != test.wantJSON {
				t.Errorf("marshal action = %s, want %s", encoded, test.wantJSON)
			}
			var decoded policies.Action
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatalf("unmarshal action: %v", err)
			}
			if diff := cmp.Diff(test.action, decoded); diff != "" {
				t.Errorf("round trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestUnmarshalRejectsMalformedStoredDocuments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   string
		target any
	}{
		{name: "condition group without all or any", data: `{}`, target: &policies.ConditionGroup{}},
		{name: "condition with a JSON number", data: `{"all": [{"signal": "pace", "operator": "gt", "value": 1}]}`, target: &policies.ConditionGroup{}},
		{name: "action without an outcome", data: `{"route_chain": null}`, target: &policies.Action{}},
		{name: "action as an array", data: `[]`, target: &policies.Action{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := json.Unmarshal([]byte(test.data), test.target)

			if !errors.Is(err, policies.ErrInvalidDocument) {
				t.Errorf("unmarshal error = %v, want ErrInvalidDocument", err)
			}
		})
	}
}

func TestMarshalRejectsUnknownValueKinds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value any
	}{
		{name: "condition value without a kind", value: policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan}},
		{name: "override of an unknown type", value: policies.OverrideValue{Type: "float"}},
		{name: "limit of an unknown kind", value: policies.Limit{Kind: "tokens"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := json.Marshal(test.value)

			if !errors.Is(err, policies.ErrInvalidDocument) {
				t.Errorf("marshal error = %v, want ErrInvalidDocument", err)
			}
		})
	}
}
