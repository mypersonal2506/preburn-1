package policies_test

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/catalog"
	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

const omitted = ""

var (
	knownPlanID       = uuid.MustParse("01926a3c-0000-7000-8000-000000000001")
	knownCustomerID   = uuid.MustParse("01926a3c-0000-7000-8000-000000000003")
	unknownCustomerID = uuid.MustParse("01926a3c-0000-7000-8000-000000000004")

	falVeoFast    = catalogfiles.ModelKey{Provider: "fal_ai", Model: "fal-ai/veo3.1/fast"}
	runwayVeoFast = catalogfiles.ModelKey{Provider: "runwayml", Model: "veo3.1_fast"}
	runwayGen     = catalogfiles.ModelKey{Provider: "runwayml", Model: "gen4.5"}
	deepgramNova  = catalogfiles.ModelKey{Provider: "deepgram", Model: "nova-3"}

	testParameterMappings = map[catalogfiles.ModelKey]map[string]catalogfiles.Parameter{
		falVeoFast: {
			"duration":   {ProviderParameter: "duration", ValueType: catalogfiles.ValueTypeString, AllowedStrings: []string{"4s", "6s", "8s"}, Effect: catalogfiles.EffectSets, Meter: "output_seconds"},
			"resolution": {ProviderParameter: "resolution", ValueType: catalogfiles.ValueTypeString, AllowedStrings: []string{"720p", "1080p", "4k"}, Effect: catalogfiles.EffectPrices, Meter: "output_seconds"},
			"audio":      {ProviderParameter: "generate_audio", ValueType: catalogfiles.ValueTypeBoolean, Effect: catalogfiles.EffectPrices, Meter: "output_seconds"},
		},
		runwayVeoFast: {
			"duration": {ProviderParameter: "duration", ValueType: catalogfiles.ValueTypeInteger, AllowedIntegers: []int64{4, 6, 8}, Effect: catalogfiles.EffectSets, Meter: "output_seconds"},
			"audio":    {ProviderParameter: "audio", ValueType: catalogfiles.ValueTypeBoolean, Effect: catalogfiles.EffectPrices, Meter: "output_seconds"},
		},
		runwayGen: {
			"duration": {ProviderParameter: "duration", ValueType: catalogfiles.ValueTypeInteger, Minimum: new(int64(2)), Maximum: new(int64(10)), Effect: catalogfiles.EffectSets, Meter: "output_seconds"},
		},
		deepgramNova: {},
	}

	testValidationContext = policies.ValidationContext{
		CustomerExists:    func(customerID uuid.UUID) bool { return customerID == knownCustomerID },
		ParameterMappings: testParameterMappings,
	}

	baseDocument = map[string]string{
		"name":           `"Slow down heavy video"`,
		"level":          `"everyone"`,
		"when":           `{"all": [{"signal": "pace", "operator": "gt", "value": "1.5"}]}`,
		"action":         `{"outcome": "deny"}`,
		"enforcement":    `"soft"`,
		"on_unreachable": `"allow"`,
		"on_uncosted":    `"allow"`,
		"status":         `"active"`,
	}
)

func TestValidateAppliesEveryDocumentRule(t *testing.T) {
	t.Parallel()
	knownPlan := quoted(identifiers.Encode(identifiers.PrefixPlan, knownPlanID))
	knownCustomer := quoted(identifiers.Encode(identifiers.PrefixCustomer, knownCustomerID))
	unknownCustomer := quoted(identifiers.Encode(identifiers.PrefixCustomer, unknownCustomerID))
	condition := `{"signal": "pace", "operator": "gt", "value": "1.5"}`
	falTarget := `{"provider": "fal_ai", "model": "fal-ai/veo3.1/fast"}`
	runwayTarget := `{"provider": "runwayml", "model": "veo3.1_fast"}`
	tests := []struct {
		name      string
		changes   map[string]string
		wantPaths []string
	}{
		{name: "everyone level", changes: nil, wantPaths: nil},
		{name: "plan level with a plan of the environment", changes: map[string]string{"level": `"plan"`, "plan_id": knownPlan}, wantPaths: nil},
		{name: "customer level with a customer of the environment", changes: map[string]string{"level": `"customer"`, "customer_id": knownCustomer}, wantPaths: nil},
		{name: "everyone level with a plan id", changes: map[string]string{"plan_id": knownPlan}, wantPaths: []string{"plan_id"}},
		{name: "everyone level with a customer id", changes: map[string]string{"customer_id": knownCustomer}, wantPaths: []string{"customer_id"}},
		{name: "plan level without a plan id", changes: map[string]string{"level": `"plan"`}, wantPaths: []string{"plan_id"}},
		{name: "plan level with a null plan id", changes: map[string]string{"level": `"plan"`, "plan_id": "null"}, wantPaths: []string{"plan_id"}},
		{name: "plan level with a customer id", changes: map[string]string{"level": `"plan"`, "plan_id": knownPlan, "customer_id": knownCustomer}, wantPaths: []string{"customer_id"}},
		{name: "customer level without a customer id", changes: map[string]string{"level": `"customer"`}, wantPaths: []string{"customer_id"}},
		{name: "customer level with an unknown customer", changes: map[string]string{"level": `"customer"`, "customer_id": unknownCustomer}, wantPaths: []string{"customer_id"}},
		{name: "customer level with a plan id", changes: map[string]string{"level": `"customer"`, "customer_id": knownCustomer, "plan_id": knownPlan}, wantPaths: []string{"plan_id"}},
		{name: "unknown level", changes: map[string]string{"level": `"team"`}, wantPaths: []string{"level"}},
		{name: "feature name", changes: map[string]string{"feature": `"text_to_video"`}, wantPaths: nil},
		{name: "feature of 64 characters", changes: map[string]string{"feature": quoted("f" + strings.Repeat("a", 63))}, wantPaths: nil},
		{name: "feature of 65 characters", changes: map[string]string{"feature": quoted("f" + strings.Repeat("a", 64))}, wantPaths: []string{"feature"}},
		{name: "feature with an uppercase letter", changes: map[string]string{"feature": `"Video"`}, wantPaths: []string{"feature"}},
		{name: "feature starting with a digit", changes: map[string]string{"feature": `"1video"`}, wantPaths: []string{"feature"}},
		{name: "empty feature", changes: map[string]string{"feature": `""`}, wantPaths: []string{"feature"}},
		{name: "name of 120 characters", changes: map[string]string{"name": quoted(strings.Repeat("n", 120))}, wantPaths: nil},
		{name: "name of 121 characters", changes: map[string]string{"name": quoted(strings.Repeat("n", 121))}, wantPaths: []string{"name"}},
		{name: "empty name", changes: map[string]string{"name": `""`}, wantPaths: []string{"name"}},
		{name: "name of spaces", changes: map[string]string{"name": `"   "`}, wantPaths: []string{"name"}},
		{name: "name with a control character", changes: map[string]string{"name": `"slow\u0007video"`}, wantPaths: []string{"name"}},
		{name: "condition groups 3 deep", changes: map[string]string{"when": `{"all": [{"any": [{"all": [` + condition + `]}]}]}`}, wantPaths: nil},
		{name: "condition groups 4 deep", changes: map[string]string{"when": `{"all": [{"any": [{"all": [{"any": [` + condition + `]}]}]}]}`}, wantPaths: []string{"when.all[0].any[0].all[0]"}},
		{name: "empty all", changes: map[string]string{"when": `{"all": []}`}, wantPaths: nil},
		{name: "empty any", changes: map[string]string{"when": `{"any": []}`}, wantPaths: []string{"when.any"}},
		{name: "nested empty any", changes: map[string]string{"when": `{"all": [` + condition + `, {"any": []}]}`}, wantPaths: []string{"when.all[1].any"}},
		{name: "unknown signal", changes: map[string]string{"when": `{"all": [{"signal": "margin", "operator": "gt", "value": "0.4"}]}`}, wantPaths: []string{"when.all[0].signal"}},
		{name: "signal that is not a policy signal", changes: map[string]string{"when": `{"all": [{"signal": "cost_allowance", "operator": "gt", "value": "1"}]}`}, wantPaths: []string{"when.all[0].signal"}},
		{name: "unknown operator", changes: map[string]string{"when": `{"all": [{"signal": "pace", "operator": "between", "value": "1.5"}]}`}, wantPaths: []string{"when.all[0].operator"}},
		{name: "every operator", changes: map[string]string{"when": `{"all": [` + operatorConditions() + `]}`}, wantPaths: nil},
		{name: "route with a chain", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + falTarget + `]}`}, wantPaths: nil},
		{name: "route without a chain", changes: map[string]string{"action": `{"outcome": "route"}`}, wantPaths: []string{"action.route_chain"}},
		{name: "route with an empty chain", changes: map[string]string{"action": `{"outcome": "route", "route_chain": []}`}, wantPaths: []string{"action.route_chain"}},
		{name: "route with 5 targets", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + repeated(falTarget, 5) + `]}`}, wantPaths: nil},
		{name: "route with 6 targets", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + repeated(falTarget, 6) + `]}`}, wantPaths: []string{"action.route_chain"}},
		{name: "route target without a model", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": ""}]}`}, wantPaths: []string{"action.route_chain[0].model"}},
		{name: "route target with a provider of 201 characters", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": ` + quoted(strings.Repeat("p", 201)) + `, "model": "gen4.5"}]}`}, wantPaths: []string{"action.route_chain[0].provider"}},
		{name: "route chain on allow", changes: map[string]string{"action": `{"outcome": "allow", "route_chain": [` + falTarget + `]}`}, wantPaths: []string{"action.route_chain"}},
		{name: "route chain on cap", changes: map[string]string{"action": `{"outcome": "cap", "route_chain": [` + falTarget + `], "limit": {"kind": "count", "value": "3"}}`}, wantPaths: []string{"action.route_chain"}},
		{name: "route override every target allows", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + falTarget + `, ` + runwayTarget + `], "overrides": {"audio": false}}`}, wantPaths: nil},
		{name: "route override a target lacks", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + falTarget + `, ` + runwayTarget + `], "overrides": {"resolution": "720p"}}`}, wantPaths: []string{"action.overrides.resolution"}},
		{name: "route override value a target rejects", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + falTarget + `, ` + runwayTarget + `], "overrides": {"duration": "8s"}}`}, wantPaths: []string{"action.overrides.duration"}},
		{name: "route override for a model without parameters", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "deepgram", "model": "nova-3"}], "overrides": {"audio": true}}`}, wantPaths: []string{"action.overrides.audio"}},
		{name: "route override for a model missing from the mappings", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [{"provider": "openai", "model": "gpt-6-sol"}], "overrides": {"audio": true}}`}, wantPaths: []string{"action.overrides.audio"}},
		{name: "cap without overrides or limit", changes: map[string]string{"action": `{"outcome": "cap"}`}, wantPaths: []string{"action"}},
		{name: "cap with empty overrides and no limit", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {}}`}, wantPaths: []string{"action"}},
		{name: "cap override that one model allows", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": 5}}`}, wantPaths: nil},
		{name: "cap override string that one model allows", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": "6s", "resolution": "720p"}}`}, wantPaths: nil},
		{name: "cap override key no model has", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"fps": 24}}`}, wantPaths: []string{"action.overrides.fps"}},
		{name: "cap override value no model allows", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"duration": 12}}`}, wantPaths: []string{"action.overrides.duration"}},
		{name: "cap override of the wrong type", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"audio": "false"}}`}, wantPaths: []string{"action.overrides.audio"}},
		{name: "cap with a count limit", changes: map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "count", "value": "3"}}`}, wantPaths: nil},
		{name: "cap with an amount limit", changes: map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "amount", "value": "25.50"}}`}, wantPaths: nil},
		{name: "cap with overrides and a limit", changes: map[string]string{"action": `{"outcome": "cap", "overrides": {"audio": false}, "limit": {"kind": "count", "value": "0"}}`}, wantPaths: nil},
		{name: "cap with an unknown limit kind", changes: map[string]string{"action": `{"outcome": "cap", "limit": {"kind": "tokens", "value": "3"}}`}, wantPaths: []string{"action.limit.kind"}},
		{name: "limit on allow", changes: map[string]string{"action": `{"outcome": "allow", "limit": {"kind": "count", "value": "3"}}`}, wantPaths: []string{"action.limit"}},
		{name: "limit on route", changes: map[string]string{"action": `{"outcome": "route", "route_chain": [` + falTarget + `], "limit": {"kind": "count", "value": "3"}}`}, wantPaths: []string{"action.limit"}},
		{name: "limit on deny", changes: map[string]string{"action": `{"outcome": "deny", "limit": {"kind": "amount", "value": "1"}}`}, wantPaths: []string{"action.limit"}},
		{name: "overrides on allow", changes: map[string]string{"action": `{"outcome": "allow", "overrides": {"audio": false}}`}, wantPaths: []string{"action.overrides"}},
		{name: "overrides on deny", changes: map[string]string{"action": `{"outcome": "deny", "overrides": {"audio": false}}`}, wantPaths: []string{"action.overrides"}},
		{name: "empty overrides on allow", changes: map[string]string{"action": `{"outcome": "allow", "overrides": {}}`}, wantPaths: nil},
		{name: "unknown outcome", changes: map[string]string{"action": `{"outcome": "throttle"}`}, wantPaths: []string{"action.outcome"}},
		{name: "hard enforcement", changes: map[string]string{"enforcement": `"hard"`}, wantPaths: nil},
		{name: "unknown enforcement", changes: map[string]string{"enforcement": `"strict"`}, wantPaths: []string{"enforcement"}},
		{name: "deny when unreachable and uncosted", changes: map[string]string{"on_unreachable": `"deny"`, "on_uncosted": `"deny"`}, wantPaths: nil},
		{name: "route when unreachable", changes: map[string]string{"on_unreachable": `"route"`}, wantPaths: []string{"on_unreachable"}},
		{name: "cap when uncosted", changes: map[string]string{"on_uncosted": `"cap"`}, wantPaths: []string{"on_uncosted"}},
		{name: "disabled", changes: map[string]string{"status": `"disabled"`}, wantPaths: nil},
		{name: "archived", changes: map[string]string{"status": `"archived"`}, wantPaths: nil},
		{name: "unknown status", changes: map[string]string{"status": `"paused"`}, wantPaths: []string{"status"}},
		{
			name: "several problems",
			changes: map[string]string{
				"name":        `""`,
				"level":       `"plan"`,
				"feature":     `"Video"`,
				"when":        `{"any": [{"signal": "margin", "operator": "gt", "value": "1"}, {"any": []}]}`,
				"action":      `{"outcome": "cap"}`,
				"enforcement": `"strict"`,
			},
			wantPaths: []string{"name", "plan_id", "feature", "when.any[0].signal", "when.any[1].any", "action", "enforcement"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, decodeErrors := policies.DecodeDocument([]byte(documentJSON(test.changes)))
			if len(decodeErrors) > 0 {
				t.Fatalf("DecodeDocument reported %v, want a well-formed document", decodeErrors)
			}

			fieldErrors := policies.Validate(policies.Policy{Document: document}, testValidationContext)

			if diff := cmp.Diff(test.wantPaths, fieldErrorPaths(fieldErrors)); diff != "" {
				t.Errorf("Validate paths mismatch (-want +got):\n%s\nerrors: %v", diff, fieldErrors)
			}
			assertMessages(t, fieldErrors)
		})
	}
}

func TestValidateChecksPoliciesBuiltInGo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		change    func(policy *policies.Policy)
		wantPaths []string
	}{
		{name: "valid policy", change: func(*policies.Policy) {}, wantPaths: nil},
		{
			name: "money value for a ratio signal",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindMoney, Amount: 1}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name: "count value for a money signal",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NameCostToDate, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindCount, Count: 1}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name: "infinite ratio",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorLessThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: math.Inf(1)}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name: "negative infinite ratio",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NameProjectedMargin, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: math.Inf(-1)}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name: "NaN ratio",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorLessThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: math.NaN()}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name: "negative count",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = policies.Condition{Signal: signals.NamePeriodDecisionCount, Operator: policies.OperatorLessThan, Value: signals.Value{Kind: signals.KindCount, Count: -1}}
			},
			wantPaths: []string{"when.all[0].value"},
		},
		{
			name:      "nil member",
			change:    func(policy *policies.Policy) { policy.When.Members[0] = nil },
			wantPaths: []string{"when.all[0]"},
		},
		{
			name: "member held by pointer",
			change: func(policy *policies.Policy) {
				policy.When.Members[0] = &policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: 1}}
			},
			wantPaths: []string{"when.all[0]"},
		},
		{
			name:      "unknown quantifier",
			change:    func(policy *policies.Policy) { policy.When.Quantifier = "most" },
			wantPaths: []string{"when"},
		},
		{
			name: "negative count limit",
			change: func(policy *policies.Policy) {
				policy.Action = policies.Action{Outcome: policies.OutcomeCap, Limit: &policies.Limit{Kind: policies.LimitKindCount, Count: -1}}
			},
			wantPaths: []string{"action.limit.value"},
		},
		{
			name: "negative amount limit",
			change: func(policy *policies.Policy) {
				policy.Action = policies.Action{Outcome: policies.OutcomeCap, Limit: &policies.Limit{Kind: policies.LimitKindAmount, Amount: -1}}
			},
			wantPaths: []string{"action.limit.value"},
		},
		{
			name: "override of an unknown value type",
			change: func(policy *policies.Policy) {
				policy.Action = policies.Action{Outcome: policies.OutcomeCap, Overrides: map[string]policies.OverrideValue{"audio": {Type: "float"}}}
			},
			wantPaths: []string{"action.overrides.audio"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := validPolicy()
			test.change(&policy)

			fieldErrors := policies.Validate(policy, testValidationContext)

			if diff := cmp.Diff(test.wantPaths, fieldErrorPaths(fieldErrors)); diff != "" {
				t.Errorf("Validate paths mismatch (-want +got):\n%s\nerrors: %v", diff, fieldErrors)
			}
			assertMessages(t, fieldErrors)
		})
	}
}

func TestValidateTellsARejectedOverrideKeyFromARejectedValue(t *testing.T) {
	t.Parallel()
	falTarget := `{"provider": "fal_ai", "model": "fal-ai/veo3.1/fast"}`
	runwayTarget := `{"provider": "runwayml", "model": "veo3.1_fast"}`
	tests := []struct {
		name        string
		action      string
		wantPath    string
		wantMessage string
	}{
		{
			name:        "route key a target lacks",
			action:      `{"outcome": "route", "route_chain": [` + falTarget + `, ` + runwayTarget + `], "overrides": {"resolution": "720p"}}`,
			wantPath:    "action.overrides.resolution",
			wantMessage: "expected a parameter that every route target supports",
		},
		{
			name:        "route value a target rejects",
			action:      `{"outcome": "route", "route_chain": [` + falTarget + `, ` + runwayTarget + `], "overrides": {"duration": 6}}`,
			wantPath:    "action.overrides.duration",
			wantMessage: "expected a value that every route target allows",
		},
		{
			name:        "cap key no model has",
			action:      `{"outcome": "cap", "overrides": {"fps": 24}}`,
			wantPath:    "action.overrides.fps",
			wantMessage: "expected a parameter of a model in the parameter mappings",
		},
		{
			name:        "cap value no model allows",
			action:      `{"outcome": "cap", "overrides": {"resolution": "8k"}}`,
			wantPath:    "action.overrides.resolution",
			wantMessage: "expected a value that a model with this parameter allows",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, decodeErrors := policies.DecodeDocument([]byte(documentJSON(map[string]string{"action": test.action})))
			if len(decodeErrors) > 0 {
				t.Fatalf("DecodeDocument reported %v, want a well-formed document", decodeErrors)
			}

			fieldErrors := policies.Validate(policies.Policy{Document: document}, testValidationContext)

			want := []policies.FieldError{{Path: test.wantPath, Message: test.wantMessage}}
			if diff := cmp.Diff(want, fieldErrors); diff != "" {
				t.Errorf("Validate mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestValidateUsesTheShippedParameterMappings(t *testing.T) {
	t.Parallel()
	shipped, err := catalogfiles.Load(catalog.Files)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	validationContext := policies.ValidationContext{ParameterMappings: shipped.ParameterMappings}
	tests := []struct {
		name      string
		action    string
		wantPaths []string
	}{
		{name: "cap to a duration one model offers", action: `{"outcome": "cap", "overrides": {"duration": "5"}}`, wantPaths: nil},
		{name: "cap to an image size", action: `{"outcome": "cap", "overrides": {"image_size": "square"}}`, wantPaths: nil},
		{name: "cap to a duration no model offers", action: `{"outcome": "cap", "overrides": {"duration": "7s"}}`, wantPaths: []string{"action.overrides.duration"}},
		{name: "route to two models sharing a resolution", action: `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1/lite"}, {"provider": "fal_ai", "model": "fal-ai/wan/v2.2-a14b/text-to-video"}], "overrides": {"resolution": "720p"}}`, wantPaths: nil},
		{name: "route to a model without that resolution", action: `{"outcome": "route", "route_chain": [{"provider": "fal_ai", "model": "fal-ai/veo3.1/lite"}, {"provider": "fal_ai", "model": "fal-ai/wan/v2.2-a14b/text-to-video"}], "overrides": {"resolution": "1080p"}}`, wantPaths: []string{"action.overrides.resolution"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, decodeErrors := policies.DecodeDocument([]byte(documentJSON(map[string]string{"action": test.action})))
			if len(decodeErrors) > 0 {
				t.Fatalf("DecodeDocument reported %v, want a well-formed document", decodeErrors)
			}

			fieldErrors := policies.Validate(policies.Policy{Document: document}, validationContext)

			if diff := cmp.Diff(test.wantPaths, fieldErrorPaths(fieldErrors)); diff != "" {
				t.Errorf("Validate paths mismatch (-want +got):\n%s\nerrors: %v", diff, fieldErrors)
			}
		})
	}
}

func TestValidateAsksForCustomersOnlyAtTheirLevel(t *testing.T) {
	t.Parallel()
	validationContext := policies.ValidationContext{
		CustomerExists: func(uuid.UUID) bool {
			t.Error("CustomerExists called for an everyone level policy")
			return true
		},
		ParameterMappings: testParameterMappings,
	}
	policy := validPolicy()
	policy.Scope.PlanID = &knownPlanID
	policy.Scope.CustomerID = &knownCustomerID

	fieldErrors := policies.Validate(policy, validationContext)

	if diff := cmp.Diff([]string{"plan_id", "customer_id"}, fieldErrorPaths(fieldErrors)); diff != "" {
		t.Errorf("Validate paths mismatch (-want +got):\n%s", diff)
	}
}

func validPolicy() policies.Policy {
	return policies.Policy{Document: policies.Document{
		Name:  "Slow down heavy video",
		Scope: policies.Scope{Level: policies.LevelEveryone},
		When: policies.ConditionGroup{Quantifier: policies.QuantifierAll, Members: []policies.GroupMember{
			policies.Condition{Signal: signals.NamePace, Operator: policies.OperatorGreaterThan, Value: signals.Value{Kind: signals.KindRatio, Ratio: 1.5}},
		}},
		Action:        policies.Action{Outcome: policies.OutcomeDeny},
		Enforcement:   policies.EnforcementSoft,
		OnUnreachable: policies.OutcomeAllow,
		OnUncosted:    policies.OutcomeAllow,
		Status:        policies.StatusActive,
	}}
}

func documentJSON(changes map[string]string) string {
	fields := maps.Clone(baseDocument)
	for key, value := range changes {
		if value == omitted {
			delete(fields, key)
			continue
		}
		fields[key] = value
	}
	entries := make([]string, 0, len(fields))
	for _, key := range slices.Sorted(maps.Keys(fields)) {
		entries = append(entries, fmt.Sprintf("%q: %s", key, fields[key]))
	}
	return "{" + strings.Join(entries, ", ") + "}"
}

func operatorConditions() string {
	conditions := make([]string, 0, 6)
	for _, operator := range []string{"lt", "lte", "gt", "gte", "eq", "ne"} {
		conditions = append(conditions, fmt.Sprintf(`{"signal": "period_decision_count", "operator": %q, "value": "10"}`, operator))
	}
	return strings.Join(conditions, ", ")
}

func repeated(element string, count int) string {
	return strings.Join(slices.Repeat([]string{element}, count), ", ")
}

func quoted(text string) string {
	return fmt.Sprintf("%q", text)
}

func fieldErrorPaths(fieldErrors []policies.FieldError) []string {
	var paths []string
	for _, fieldError := range fieldErrors {
		paths = append(paths, fieldError.Path)
	}
	return paths
}

func assertMessages(t *testing.T, fieldErrors []policies.FieldError) {
	t.Helper()
	for _, fieldError := range fieldErrors {
		if !strings.HasPrefix(fieldError.Message, "expected ") {
			t.Errorf("error at %q has message %q, want a message starting with expected", fieldError.Path, fieldError.Message)
		}
	}
}
