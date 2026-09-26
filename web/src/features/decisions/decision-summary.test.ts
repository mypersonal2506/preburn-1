import { expect, test } from "vitest";
import type { DecisionDetailResponse } from "@/client";
import {
	type DecisionSummaryNames,
	decisionSummary,
} from "@/features/decisions/decision-summary";

type SummaryCase = {
	outcome: DecisionDetailResponse["outcome"];
	reason: DecisionDetailResponse["reason"];
	overrides: DecisionDetailResponse["overrides"];
	policyName: string | null;
	expected: string;
};

const MODEL_NAMES: Omit<DecisionSummaryNames, "policyName"> = {
	requestedModelName: "Veo 3.1 Fast",
	servedModelName: "Kling 2.5 Turbo Pro",
};

const SUMMARY_CASES: SummaryCase[] = [
	{
		outcome: "allow",
		reason: "no_policy_matched",
		overrides: {},
		policyName: null,
		expected: "Allowed Veo 3.1 Fast because no policy matched.",
	},
	{
		outcome: "allow",
		reason: "policy_matched",
		overrides: {},
		policyName: "Free tier",
		expected: "Free tier allowed Veo 3.1 Fast.",
	},
	{
		outcome: "allow",
		reason: "cap_not_applicable",
		overrides: {},
		policyName: "Cap on pace",
		expected:
			"Cap on pace allowed Veo 3.1 Fast because its cap does not apply to the model.",
	},
	{
		outcome: "allow",
		reason: "uncosted_allowed",
		overrides: {},
		policyName: null,
		expected: "Allowed Veo 3.1 Fast without a price.",
	},
	{
		outcome: "route",
		reason: "policy_matched",
		overrides: {},
		policyName: "Heavy video users",
		expected: "Heavy video users routed Veo 3.1 Fast to Kling 2.5 Turbo Pro.",
	},
	{
		outcome: "route",
		reason: "policy_matched",
		overrides: { duration: 5, audio: false },
		policyName: "Heavy video users",
		expected:
			"Heavy video users routed Veo 3.1 Fast to Kling 2.5 Turbo Pro, at 5s, without audio.",
	},
	{
		outcome: "cap",
		reason: "policy_matched",
		overrides: { duration: "4s", audio: false },
		policyName: "Cap on pace",
		expected: "Cap on pace capped Veo 3.1 Fast to 4s, without audio.",
	},
	{
		outcome: "cap",
		reason: "policy_matched",
		overrides: {},
		policyName: "Twenty a month",
		expected: "Twenty a month capped Veo 3.1 Fast.",
	},
	{
		outcome: "cap",
		reason: "uncosted_allowed",
		overrides: { resolution: "720p" },
		policyName: "Cap on pace",
		expected: "Cap on pace capped Veo 3.1 Fast to 720p without a price.",
	},
	{
		outcome: "deny",
		reason: "policy_matched",
		overrides: {},
		policyName: "Stop losses",
		expected: "Stop losses denied Veo 3.1 Fast.",
	},
	{
		outcome: "deny",
		reason: "hard_limit_reached",
		overrides: {},
		policyName: "Stop at allowance",
		expected:
			"Stop at allowance denied Veo 3.1 Fast because the hard limit was reached.",
	},
	{
		outcome: "deny",
		reason: "route_chain_exhausted",
		overrides: {},
		policyName: "Cheaper model",
		expected:
			"Cheaper model denied Veo 3.1 Fast because no route target was priced within the limits.",
	},
	{
		outcome: "deny",
		reason: "uncosted_denied",
		overrides: {},
		policyName: "Stop losses",
		expected: "Stop losses denied Veo 3.1 Fast because it has no price.",
	},
];

test.each(SUMMARY_CASES)(
	"summarizes $outcome with $reason",
	({ outcome, reason, overrides, policyName, expected }) => {
		expect(
			decisionSummary(
				{ outcome, reason, overrides },
				{ ...MODEL_NAMES, policyName },
			),
		).toBe(expected);
	},
);
