package policies_test

import (
	"math"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/preburn/preburn/internal/catalogfiles"
	"github.com/preburn/preburn/internal/money"
	"github.com/preburn/preburn/internal/policies"
	"github.com/preburn/preburn/internal/signals"
)

var (
	requestCustomerID = uuid.MustParse("01926a3c-0000-7000-8000-00000000c001")
	otherCustomerID   = uuid.MustParse("01926a3c-0000-7000-8000-00000000c002")
	requestPlanID     = uuid.MustParse("01926a3c-0000-7000-8000-00000000a001")
	otherPlanID       = uuid.MustParse("01926a3c-0000-7000-8000-00000000a002")
	policyUpdatedAt   = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	baseRequest = policies.ResolutionRequest{
		CustomerID:      requestCustomerID,
		PlanID:          &requestPlanID,
		Feature:         "text_to_video",
		ModelParameters: testParameterMappings[falVeoFast],
	}

	baseSignals = signals.Signals{
		PeriodRevenueNet:     money.Amount(20_000_000_000),
		CostToDate:           money.Amount(2_000_000_000),
		ElapsedFraction:      0.5,
		AllowanceRemaining:   money.Amount(1_000_000_000),
		Pace:                 1.5,
		ProjectedMargin:      math.Inf(-1),
		RequestEstimatedCost: new(money.Amount(500_000_000)),
		PeriodDecisionCount:  10,
	}
)

func TestResolvePicksThePolicyByLevelOutcomeAndRecency(t *testing.T) {
	t.Parallel()
	laterUpdate := func(policy policies.Policy) policies.Policy {
		policy.UpdatedAt = policyUpdatedAt.Add(time.Minute)
		return policy
	}
	withStatus := func(policy policies.Policy, status policies.Status) policies.Policy {
		policy.Status = status
		return policy
	}
	withFeature := func(policy policies.Policy, feature string) policies.Policy {
		policy.Feature = &feature
		return policy
	}
	withPlan := func(policy policies.Policy, planID uuid.UUID) policies.Policy {
		policy.Scope.PlanID = &planID
		return policy
	}
	withCustomer := func(policy policies.Policy, customerID uuid.UUID) policies.Policy {
		policy.Scope.CustomerID = &customerID
		return policy
	}
	withoutPlan := baseRequest
	withoutPlan.PlanID = nil
	tests := []struct {
		name        string
		policies    []policies.Policy
		request     policies.ResolutionRequest
		wantPolicy  byte
		wantOutcome policies.Outcome
	}{
		{name: "no policies", policies: nil, request: baseRequest, wantPolicy: 0, wantOutcome: policies.OutcomeAllow},
		{
			name:        "customer level beats plan level and everyone",
			policies:    []policies.Policy{newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), newPolicy(2, policies.LevelPlan, policies.OutcomeDeny), newPolicy(3, policies.LevelCustomer, policies.OutcomeAllow)},
			request:     baseRequest,
			wantPolicy:  3,
			wantOutcome: policies.OutcomeAllow,
		},
		{
			name:        "plan level beats everyone",
			policies:    []policies.Policy{newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), newPolicy(2, policies.LevelPlan, policies.OutcomeRoute)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeRoute,
		},
		{
			name:        "deny beats cap within a level",
			policies:    []policies.Policy{newPolicy(1, policies.LevelPlan, policies.OutcomeCap), newPolicy(2, policies.LevelPlan, policies.OutcomeDeny), newPolicy(3, policies.LevelPlan, policies.OutcomeAllow)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeDeny,
		},
		{
			name:        "cap beats route within a level",
			policies:    []policies.Policy{newPolicy(1, policies.LevelEveryone, policies.OutcomeRoute), newPolicy(2, policies.LevelEveryone, policies.OutcomeCap)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeCap,
		},
		{
			name:        "route beats allow within a level",
			policies:    []policies.Policy{newPolicy(1, policies.LevelCustomer, policies.OutcomeAllow), newPolicy(2, policies.LevelCustomer, policies.OutcomeRoute)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeRoute,
		},
		{
			name:        "latest update wins a tie",
			policies:    []policies.Policy{newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), laterUpdate(newPolicy(2, policies.LevelEveryone, policies.OutcomeDeny)), newPolicy(3, policies.LevelEveryone, policies.OutcomeDeny)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeDeny,
		},
		{
			name:        "lowest id wins a tie on update time",
			policies:    []policies.Policy{newPolicy(3, policies.LevelEveryone, policies.OutcomeDeny), newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), newPolicy(2, policies.LevelEveryone, policies.OutcomeDeny)},
			request:     baseRequest,
			wantPolicy:  1,
			wantOutcome: policies.OutcomeDeny,
		},
		{
			name:        "disabled and archived policies are ignored",
			policies:    []policies.Policy{withStatus(newPolicy(1, policies.LevelCustomer, policies.OutcomeDeny), policies.StatusDisabled), withStatus(newPolicy(2, policies.LevelPlan, policies.OutcomeDeny), policies.StatusArchived), newPolicy(3, policies.LevelEveryone, policies.OutcomeRoute)},
			request:     baseRequest,
			wantPolicy:  3,
			wantOutcome: policies.OutcomeRoute,
		},
		{
			name:        "policy for the request feature applies",
			policies:    []policies.Policy{withFeature(newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), "text_to_video")},
			request:     baseRequest,
			wantPolicy:  1,
			wantOutcome: policies.OutcomeDeny,
		},
		{
			name:        "policy for another feature is ignored",
			policies:    []policies.Policy{withFeature(newPolicy(1, policies.LevelCustomer, policies.OutcomeDeny), "image_to_video"), newPolicy(2, policies.LevelEveryone, policies.OutcomeRoute)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeRoute,
		},
		{
			name:        "policy for every feature applies next to a feature policy",
			policies:    []policies.Policy{withFeature(newPolicy(1, policies.LevelEveryone, policies.OutcomeRoute), "text_to_video"), newPolicy(2, policies.LevelEveryone, policies.OutcomeDeny)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeDeny,
		},
		{
			name:        "policy for another plan is ignored",
			policies:    []policies.Policy{withPlan(newPolicy(1, policies.LevelPlan, policies.OutcomeDeny), otherPlanID)},
			request:     baseRequest,
			wantPolicy:  0,
			wantOutcome: policies.OutcomeAllow,
		},
		{
			name:        "plan policy is ignored for a customer without a plan",
			policies:    []policies.Policy{newPolicy(1, policies.LevelPlan, policies.OutcomeDeny), newPolicy(2, policies.LevelEveryone, policies.OutcomeRoute)},
			request:     withoutPlan,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeRoute,
		},
		{
			name:        "policy for another customer is ignored",
			policies:    []policies.Policy{withCustomer(newPolicy(1, policies.LevelCustomer, policies.OutcomeDeny), otherCustomerID)},
			request:     baseRequest,
			wantPolicy:  0,
			wantOutcome: policies.OutcomeAllow,
		},
		{
			name:        "policy whose conditions fail is ignored",
			policies:    []policies.Policy{withConditions(newPolicy(1, policies.LevelCustomer, policies.OutcomeDeny), policies.QuantifierAll, ratioCondition(signals.NamePace, policies.OperatorGreaterThan, 2)), newPolicy(2, policies.LevelEveryone, policies.OutcomeRoute)},
			request:     baseRequest,
			wantPolicy:  2,
			wantOutcome: policies.OutcomeRoute,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			resolution := policies.Resolve(test.policies, test.request, baseSignals)

			if resolution.Outcome != test.wantOutcome {
				t.Errorf("outcome = %s, want %s", resolution.Outcome, test.wantOutcome)
			}
			if test.wantPolicy == 0 {
				if resolution.MatchedPolicy != nil || resolution.Reason != policies.ReasonNoPolicyMatched {
					t.Errorf("resolution = %+v, want no matched policy and reason no_policy_matched", resolution)
				}
				return
			}
			if resolution.MatchedPolicy == nil || resolution.MatchedPolicy.ID != policyID(test.wantPolicy) || resolution.Reason != policies.ReasonPolicyMatched {
				t.Errorf("resolution = %+v, want policy %d matched with reason policy_matched", resolution, test.wantPolicy)
			}
		})
	}
}

func TestResolveComparesEachOperatorOnEachSignalKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		condition policies.Condition
		want      bool
	}{
		{name: "money lt above", condition: moneyCondition(signals.NameCostToDate, policies.OperatorLessThan, 2_000_000_001), want: true},
		{name: "money lt equal", condition: moneyCondition(signals.NameCostToDate, policies.OperatorLessThan, 2_000_000_000), want: false},
		{name: "money lte equal", condition: moneyCondition(signals.NameCostToDate, policies.OperatorLessThanOrEqual, 2_000_000_000), want: true},
		{name: "money lte below", condition: moneyCondition(signals.NameCostToDate, policies.OperatorLessThanOrEqual, 1_999_999_999), want: false},
		{name: "money gt below", condition: moneyCondition(signals.NameCostToDate, policies.OperatorGreaterThan, 1_999_999_999), want: true},
		{name: "money gt equal", condition: moneyCondition(signals.NameCostToDate, policies.OperatorGreaterThan, 2_000_000_000), want: false},
		{name: "money gte equal", condition: moneyCondition(signals.NameCostToDate, policies.OperatorGreaterThanOrEqual, 2_000_000_000), want: true},
		{name: "money gte above", condition: moneyCondition(signals.NameCostToDate, policies.OperatorGreaterThanOrEqual, 2_000_000_001), want: false},
		{name: "money eq equal", condition: moneyCondition(signals.NamePeriodRevenueNet, policies.OperatorEqual, 20_000_000_000), want: true},
		{name: "money eq one nano off", condition: moneyCondition(signals.NamePeriodRevenueNet, policies.OperatorEqual, 20_000_000_001), want: false},
		{name: "money ne one nano off", condition: moneyCondition(signals.NameAllowanceRemaining, policies.OperatorNotEqual, 1_000_000_001), want: true},
		{name: "money ne equal", condition: moneyCondition(signals.NameAllowanceRemaining, policies.OperatorNotEqual, 1_000_000_000), want: false},
		{name: "ratio lt above", condition: ratioCondition(signals.NamePace, policies.OperatorLessThan, 1.6), want: true},
		{name: "ratio lt equal", condition: ratioCondition(signals.NamePace, policies.OperatorLessThan, 1.5), want: false},
		{name: "ratio lte equal", condition: ratioCondition(signals.NamePace, policies.OperatorLessThanOrEqual, 1.5), want: true},
		{name: "ratio gt below", condition: ratioCondition(signals.NameElapsedFraction, policies.OperatorGreaterThan, 0.4999), want: true},
		{name: "ratio gt equal", condition: ratioCondition(signals.NameElapsedFraction, policies.OperatorGreaterThan, 0.5), want: false},
		{name: "ratio gte equal", condition: ratioCondition(signals.NameElapsedFraction, policies.OperatorGreaterThanOrEqual, 0.5), want: true},
		{name: "ratio eq equal", condition: ratioCondition(signals.NamePace, policies.OperatorEqual, 1.5), want: true},
		{name: "ratio ne equal", condition: ratioCondition(signals.NamePace, policies.OperatorNotEqual, 1.5), want: false},
		{name: "negative infinite ratio lt", condition: ratioCondition(signals.NameProjectedMargin, policies.OperatorLessThan, -1000), want: true},
		{name: "negative infinite ratio gt", condition: ratioCondition(signals.NameProjectedMargin, policies.OperatorGreaterThan, -1000), want: false},
		{name: "count lt above", condition: decisionCountCondition(policies.OperatorLessThan, 11), want: true},
		{name: "count lt equal", condition: decisionCountCondition(policies.OperatorLessThan, 10), want: false},
		{name: "count lte equal", condition: decisionCountCondition(policies.OperatorLessThanOrEqual, 10), want: true},
		{name: "count gt below", condition: decisionCountCondition(policies.OperatorGreaterThan, 9), want: true},
		{name: "count gte above", condition: decisionCountCondition(policies.OperatorGreaterThanOrEqual, 11), want: false},
		{name: "count eq equal", condition: decisionCountCondition(policies.OperatorEqual, 10), want: true},
		{name: "count ne other", condition: decisionCountCondition(policies.OperatorNotEqual, 0), want: true},
		{name: "request estimated cost gt", condition: moneyCondition(signals.NameRequestEstimatedCost, policies.OperatorGreaterThan, 400_000_000), want: true},
		{name: "request estimated cost eq", condition: moneyCondition(signals.NameRequestEstimatedCost, policies.OperatorEqual, 500_000_000), want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := withConditions(newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), policies.QuantifierAll, test.condition)

			resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, baseSignals)

			if matched := resolution.MatchedPolicy != nil; matched != test.want {
				t.Errorf("condition matched = %t, want %t", matched, test.want)
			}
		})
	}
}

func TestResolveFailsRequestEstimatedCostConditionsForUncostedRequests(t *testing.T) {
	t.Parallel()
	uncosted := baseSignals
	uncosted.RequestEstimatedCost = nil
	operators := []policies.Operator{
		policies.OperatorLessThan,
		policies.OperatorLessThanOrEqual,
		policies.OperatorGreaterThan,
		policies.OperatorGreaterThanOrEqual,
		policies.OperatorEqual,
		policies.OperatorNotEqual,
	}
	for _, operator := range operators {
		t.Run(string(operator), func(t *testing.T) {
			t.Parallel()
			policy := withConditions(newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), policies.QuantifierAll, moneyCondition(signals.NameRequestEstimatedCost, operator, 0))

			resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, uncosted)

			if resolution.MatchedPolicy != nil || resolution.Outcome != policies.OutcomeAllow {
				t.Errorf("resolution = %+v, want no match for an uncosted request", resolution)
			}
		})
	}
}

func TestResolveEvaluatesConditionGroups(t *testing.T) {
	t.Parallel()
	paceAbove := ratioCondition(signals.NamePace, policies.OperatorGreaterThan, 1)
	paceBelow := ratioCondition(signals.NamePace, policies.OperatorLessThan, 1)
	tests := []struct {
		name       string
		quantifier policies.Quantifier
		members    []policies.GroupMember
		want       bool
	}{
		{name: "empty all", quantifier: policies.QuantifierAll, members: nil, want: true},
		{name: "all true", quantifier: policies.QuantifierAll, members: []policies.GroupMember{paceAbove, paceAbove}, want: true},
		{name: "all with one false", quantifier: policies.QuantifierAll, members: []policies.GroupMember{paceAbove, paceBelow}, want: false},
		{name: "any with one true", quantifier: policies.QuantifierAny, members: []policies.GroupMember{paceBelow, paceAbove}, want: true},
		{name: "any all false", quantifier: policies.QuantifierAny, members: []policies.GroupMember{paceBelow, paceBelow}, want: false},
		{
			name:       "nested any inside all",
			quantifier: policies.QuantifierAll,
			members: []policies.GroupMember{
				paceAbove,
				policies.ConditionGroup{Quantifier: policies.QuantifierAny, Members: []policies.GroupMember{paceBelow, paceAbove}},
			},
			want: true,
		},
		{
			name:       "nested all failing inside any",
			quantifier: policies.QuantifierAny,
			members: []policies.GroupMember{
				paceBelow,
				policies.ConditionGroup{Quantifier: policies.QuantifierAll, Members: []policies.GroupMember{paceAbove, paceBelow}},
			},
			want: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := withConditions(newPolicy(1, policies.LevelEveryone, policies.OutcomeDeny), test.quantifier, test.members...)

			resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, baseSignals)

			if matched := resolution.MatchedPolicy != nil; matched != test.want {
				t.Errorf("group matched = %t, want %t", matched, test.want)
			}
		})
	}
}

func TestResolveReportsHardAllowanceConditions(t *testing.T) {
	t.Parallel()
	allowanceLow := moneyCondition(signals.NameAllowanceRemaining, policies.OperatorLessThan, 2_000_000_000)
	allowanceNegative := moneyCondition(signals.NameAllowanceRemaining, policies.OperatorLessThan, 0)
	paceAbove := ratioCondition(signals.NamePace, policies.OperatorGreaterThan, 1)
	paceFar := ratioCondition(signals.NamePace, policies.OperatorGreaterThan, 100)
	tests := []struct {
		name        string
		enforcement policies.Enforcement
		quantifier  policies.Quantifier
		members     []policies.GroupMember
		want        bool
	}{
		{name: "hard allowance condition", enforcement: policies.EnforcementHard, quantifier: policies.QuantifierAll, members: []policies.GroupMember{allowanceLow}, want: true},
		{name: "soft allowance condition", enforcement: policies.EnforcementSoft, quantifier: policies.QuantifierAll, members: []policies.GroupMember{allowanceLow}, want: false},
		{name: "hard condition on another signal", enforcement: policies.EnforcementHard, quantifier: policies.QuantifierAll, members: []policies.GroupMember{paceAbove}, want: false},
		{name: "hard allowance condition that failed inside any", enforcement: policies.EnforcementHard, quantifier: policies.QuantifierAny, members: []policies.GroupMember{allowanceNegative, paceAbove}, want: false},
		{name: "hard allowance condition that held inside any", enforcement: policies.EnforcementHard, quantifier: policies.QuantifierAny, members: []policies.GroupMember{allowanceLow, paceFar}, want: true},
		{
			name:        "hard allowance condition inside a failed nested group",
			enforcement: policies.EnforcementHard,
			quantifier:  policies.QuantifierAny,
			members: []policies.GroupMember{
				paceAbove,
				policies.ConditionGroup{Quantifier: policies.QuantifierAll, Members: []policies.GroupMember{allowanceLow, paceFar}},
			},
			want: false,
		},
		{
			name:        "hard allowance condition inside a held nested group",
			enforcement: policies.EnforcementHard,
			quantifier:  policies.QuantifierAll,
			members: []policies.GroupMember{
				paceAbove,
				policies.ConditionGroup{Quantifier: policies.QuantifierAny, Members: []policies.GroupMember{paceFar, allowanceLow}},
			},
			want: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := withConditions(newPolicy(1, policies.LevelEveryone, policies.OutcomeAllow), test.quantifier, test.members...)
			policy.Enforcement = test.enforcement

			resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, baseSignals)

			if resolution.MatchedPolicy == nil {
				t.Fatalf("resolution = %+v, want the policy matched", resolution)
			}
			if resolution.HardAllowanceCondition != test.want {
				t.Errorf("HardAllowanceCondition = %t, want %t", resolution.HardAllowanceCondition, test.want)
			}
		})
	}
}

func TestResolveWithoutAMatchAllows(t *testing.T) {
	t.Parallel()

	resolution := policies.Resolve(nil, baseRequest, baseSignals)

	want := policies.Resolution{
		Outcome:         policies.OutcomeAllow,
		Reason:          policies.ReasonNoPolicyMatched,
		Overrides:       map[string]policies.OverrideValue{},
		Enforcement:     policies.EnforcementSoft,
		OnUncosted:      policies.OutcomeAllow,
		FallbackOutcome: policies.OutcomeAllow,
	}
	if diff := cmp.Diff(want, resolution); diff != "" {
		t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveCarriesTheMatchedRoute(t *testing.T) {
	t.Parallel()
	policy := newPolicy(1, policies.LevelPlan, policies.OutcomeRoute)
	policy.Version = 4
	policy.Action.Overrides = map[string]policies.OverrideValue{"resolution": {Type: catalogfiles.ValueTypeString, String: "720p"}}
	policy.Enforcement = policies.EnforcementHard
	policy.OnUnreachable = policies.OutcomeDeny
	policy.OnUncosted = policies.OutcomeDeny

	resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, baseSignals)

	want := policies.Resolution{
		Outcome:         policies.OutcomeRoute,
		Reason:          policies.ReasonPolicyMatched,
		MatchedPolicy:   &policy,
		RouteChain:      []policies.RouteTarget{{Provider: "fal_ai", Model: "fal-ai/veo3.1/lite"}},
		Overrides:       map[string]policies.OverrideValue{"resolution": {Type: catalogfiles.ValueTypeString, String: "720p"}},
		Enforcement:     policies.EnforcementHard,
		OnUncosted:      policies.OutcomeDeny,
		FallbackOutcome: policies.OutcomeDeny,
	}
	if diff := cmp.Diff(want, resolution); diff != "" {
		t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
	}
}

func TestResolveKeepsOnlyCapOverridesTheRequestedModelSupports(t *testing.T) {
	t.Parallel()
	countLimit := &policies.Limit{Kind: policies.LimitKindCount, Count: 3}
	tests := []struct {
		name          string
		overrides     map[string]policies.OverrideValue
		limit         *policies.Limit
		wantOutcome   policies.Outcome
		wantReason    policies.Reason
		wantOverrides map[string]policies.OverrideValue
		wantLimit     *policies.Limit
	}{
		{
			name:          "supported override",
			overrides:     map[string]policies.OverrideValue{"duration": {Type: catalogfiles.ValueTypeString, String: "4s"}},
			wantOutcome:   policies.OutcomeCap,
			wantReason:    policies.ReasonPolicyMatched,
			wantOverrides: map[string]policies.OverrideValue{"duration": {Type: catalogfiles.ValueTypeString, String: "4s"}},
		},
		{
			name: "supported and unsupported overrides",
			overrides: map[string]policies.OverrideValue{
				"duration": {Type: catalogfiles.ValueTypeInteger, Integer: 5},
				"audio":    {Type: catalogfiles.ValueTypeBoolean, Boolean: false},
			},
			wantOutcome:   policies.OutcomeCap,
			wantReason:    policies.ReasonPolicyMatched,
			wantOverrides: map[string]policies.OverrideValue{"audio": {Type: catalogfiles.ValueTypeBoolean, Boolean: false}},
		},
		{
			name:          "limit without overrides",
			limit:         countLimit,
			wantOutcome:   policies.OutcomeCap,
			wantReason:    policies.ReasonPolicyMatched,
			wantOverrides: map[string]policies.OverrideValue{},
			wantLimit:     countLimit,
		},
		{
			name:          "unsupported override with a limit",
			overrides:     map[string]policies.OverrideValue{"steps": {Type: catalogfiles.ValueTypeInteger, Integer: 20}},
			limit:         countLimit,
			wantOutcome:   policies.OutcomeCap,
			wantReason:    policies.ReasonPolicyMatched,
			wantOverrides: map[string]policies.OverrideValue{},
			wantLimit:     countLimit,
		},
		{
			name:          "override key the model lacks and no limit",
			overrides:     map[string]policies.OverrideValue{"steps": {Type: catalogfiles.ValueTypeInteger, Integer: 20}},
			wantOutcome:   policies.OutcomeAllow,
			wantReason:    policies.ReasonCapNotApplicable,
			wantOverrides: map[string]policies.OverrideValue{},
		},
		{
			name:          "override value the model rejects and no limit",
			overrides:     map[string]policies.OverrideValue{"duration": {Type: catalogfiles.ValueTypeString, String: "5"}},
			wantOutcome:   policies.OutcomeAllow,
			wantReason:    policies.ReasonCapNotApplicable,
			wantOverrides: map[string]policies.OverrideValue{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			policy := newPolicy(1, policies.LevelEveryone, policies.OutcomeCap)
			policy.Action.Overrides = test.overrides
			policy.Action.Limit = test.limit

			resolution := policies.Resolve([]policies.Policy{policy}, baseRequest, baseSignals)

			if resolution.Outcome != test.wantOutcome || resolution.Reason != test.wantReason {
				t.Errorf("resolution = %s %s, want %s %s", resolution.Outcome, resolution.Reason, test.wantOutcome, test.wantReason)
			}
			if resolution.MatchedPolicy == nil || resolution.MatchedPolicy.ID != policy.ID {
				t.Errorf("matched policy = %+v, want policy %s", resolution.MatchedPolicy, policy.ID)
			}
			if diff := cmp.Diff(test.wantOverrides, resolution.Overrides); diff != "" {
				t.Errorf("overrides mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(test.wantLimit, resolution.Limit); diff != "" {
				t.Errorf("limit mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestApplicableOverridesKeepsSupportedKeysWithAllowedValues(t *testing.T) {
	t.Parallel()
	overrides := map[string]policies.OverrideValue{
		"duration":   {Type: catalogfiles.ValueTypeInteger, Integer: 12},
		"audio":      {Type: catalogfiles.ValueTypeBoolean, Boolean: true},
		"resolution": {Type: catalogfiles.ValueTypeString, String: "720p"},
		"steps":      {Type: catalogfiles.ValueTypeInteger, Integer: 20},
	}
	tests := []struct {
		name       string
		parameters map[string]catalogfiles.Parameter
		want       map[string]policies.OverrideValue
	}{
		{
			name:       "string and boolean parameters",
			parameters: testParameterMappings[falVeoFast],
			want: map[string]policies.OverrideValue{
				"audio":      {Type: catalogfiles.ValueTypeBoolean, Boolean: true},
				"resolution": {Type: catalogfiles.ValueTypeString, String: "720p"},
			},
		},
		{
			name:       "integer range that excludes the value",
			parameters: testParameterMappings[runwayGen],
			want:       map[string]policies.OverrideValue{},
		},
		{
			name:       "model without parameters",
			parameters: testParameterMappings[deepgramNova],
			want:       map[string]policies.OverrideValue{},
		},
		{
			name:       "model missing from the mappings",
			parameters: nil,
			want:       map[string]policies.OverrideValue{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			applicable := policies.ApplicableOverrides(overrides, test.parameters)

			if diff := cmp.Diff(test.want, applicable); diff != "" {
				t.Errorf("ApplicableOverrides mismatch (-want +got):\n%s", diff)
			}
		})
	}
	integerRange := policies.ApplicableOverrides(
		map[string]policies.OverrideValue{"duration": {Type: catalogfiles.ValueTypeInteger, Integer: 10}},
		testParameterMappings[runwayGen],
	)
	if len(integerRange) != 1 {
		t.Errorf("ApplicableOverrides kept %v, want duration 10 inside the range 2 to 10", integerRange)
	}
}

func newPolicy(number byte, level policies.Level, outcome policies.Outcome) policies.Policy {
	policy := policies.Policy{
		ID: policyID(number),
		Document: policies.Document{
			Name:          "Policy",
			Scope:         policies.Scope{Level: level},
			When:          policies.ConditionGroup{Quantifier: policies.QuantifierAll},
			Action:        policies.Action{Outcome: outcome},
			Enforcement:   policies.EnforcementSoft,
			OnUnreachable: policies.OutcomeAllow,
			OnUncosted:    policies.OutcomeAllow,
			Status:        policies.StatusActive,
		},
		Version:   1,
		UpdatedAt: policyUpdatedAt,
	}
	switch level {
	case policies.LevelPlan:
		policy.Scope.PlanID = &requestPlanID
	case policies.LevelCustomer:
		policy.Scope.CustomerID = &requestCustomerID
	case policies.LevelEveryone:
	}
	switch outcome {
	case policies.OutcomeRoute:
		policy.Action.RouteChain = []policies.RouteTarget{{Provider: "fal_ai", Model: "fal-ai/veo3.1/lite"}}
	case policies.OutcomeCap:
		policy.Action.Limit = &policies.Limit{Kind: policies.LimitKindCount, Count: 3}
	case policies.OutcomeAllow, policies.OutcomeDeny:
	}
	return policy
}

func withConditions(policy policies.Policy, quantifier policies.Quantifier, members ...policies.GroupMember) policies.Policy {
	policy.When = policies.ConditionGroup{Quantifier: quantifier, Members: members}
	return policy
}

func policyID(number byte) uuid.UUID {
	return uuid.UUID{15: number}
}

func moneyCondition(signal signals.Name, operator policies.Operator, nanos int64) policies.Condition {
	return policies.Condition{Signal: signal, Operator: operator, Value: signals.Value{Kind: signals.KindMoney, Amount: money.Amount(nanos)}}
}

func ratioCondition(signal signals.Name, operator policies.Operator, ratio float64) policies.Condition {
	return policies.Condition{Signal: signal, Operator: operator, Value: signals.Value{Kind: signals.KindRatio, Ratio: ratio}}
}

func decisionCountCondition(operator policies.Operator, count int64) policies.Condition {
	return policies.Condition{Signal: signals.NamePeriodDecisionCount, Operator: operator, Value: signals.Value{Kind: signals.KindCount, Count: count}}
}
