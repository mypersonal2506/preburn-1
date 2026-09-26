import { describe, expect, test } from "vitest";
import {
	type PolicyNames,
	type PolicySummaryDocument,
	policySummary,
} from "@/features/policies/policy-summary";

const PLAN_ID = "pln_01jbvagescfn78y0938nkrkayd";
const CUSTOMER_ID = "cust_01jbvagescfn78y0938nkrkayd";

const names: PolicyNames = {
	plan: (planId) => {
		if (planId !== PLAN_ID) {
			throw new Error(`unexpected plan plan_id=${planId}`);
		}
		return "Creator";
	},
	customer: (customerId) => {
		if (customerId !== CUSTOMER_ID) {
			throw new Error(`unexpected customer customer_id=${customerId}`);
		}
		return "Sam Rivera";
	},
	model: (target) => `${target.model} display`,
};

const everyone: PolicySummaryDocument = {
	level: "everyone",
	plan_id: null,
	customer_id: null,
	feature: null,
	when: { all: [] },
	action: { outcome: "allow", route_chain: null, overrides: null, limit: null },
};

describe("policySummary", () => {
	test.each<[string, PolicySummaryDocument, string]>([
		[
			"allow always",
			everyone,
			"For all customers using any feature, always allow the request.",
		],
		[
			"deny with one condition for a plan and a feature",
			{
				...everyone,
				level: "plan",
				plan_id: PLAN_ID,
				feature: "text_to_video",
				when: {
					all: [
						{
							signal: "allowance_remaining",
							operator: "lte",
							value: "0.000000000",
						},
					],
				},
				action: { outcome: "deny" },
			},
			"For Creator customers using text to video, when allowance left is at most $0.00, deny the request.",
		],
		[
			"route with two fallbacks and overrides for a customer",
			{
				...everyone,
				level: "customer",
				customer_id: CUSTOMER_ID,
				when: {
					all: [{ signal: "pace", operator: "gt", value: "2.0000" }],
				},
				action: {
					outcome: "route",
					route_chain: [
						{ provider: "fal_ai", model: "kling" },
						{ provider: "fal_ai", model: "veo-lite" },
						{ provider: "runwayml", model: "gen4" },
					],
					overrides: { duration: "5", audio: false },
					limit: null,
				},
			},
			"For Sam Rivera using any feature, when pace is above 2.0x, route to kling display, or veo-lite display if that has no price, or gen4 display if that has no price, at 5s, without audio.",
		],
		[
			"cap with overrides and a limit",
			{
				...everyone,
				action: {
					outcome: "cap",
					overrides: { duration: "4s", audio: false },
					limit: { kind: "count", value: "20" },
				},
			},
			"For all customers using any feature, always cap to 4s, no audio and allow at most 20 requests this period.",
		],
		[
			"cap with a count limit only",
			{
				...everyone,
				action: {
					outcome: "cap",
					overrides: null,
					limit: { kind: "count", value: "1" },
				},
			},
			"For all customers using any feature, always cap and allow at most 1 request this period.",
		],
		[
			"cap with an amount limit only",
			{
				...everyone,
				action: {
					outcome: "cap",
					limit: { kind: "amount", value: "25.000000000" },
				},
			},
			"For all customers using any feature, always cap and allow at most $25.00 of AI cost this period.",
		],
		[
			"inline conditions joined by or",
			{
				...everyone,
				when: {
					any: [
						{ signal: "projected_margin", operator: "lt", value: "-0.5000" },
						{ signal: "period_decision_count", operator: "gte", value: "1000" },
						{ signal: "elapsed_fraction", operator: "ne", value: "0.2500" },
					],
				},
				action: { outcome: "deny" },
			},
			"For all customers using any feature, when projected margin is below -50.0% or requests this period is at least 1,000 or period elapsed is not 25.0%, deny the request.",
		],
		[
			"four conditions grouped",
			{
				...everyone,
				when: {
					all: [
						{ signal: "pace", operator: "gt", value: "2.0000" },
						{ signal: "cost_to_date", operator: "gt", value: "5.000000000" },
						{
							signal: "period_revenue_net",
							operator: "eq",
							value: "0.000000000",
						},
						{
							signal: "request_estimated_cost",
							operator: "gt",
							value: "0.500000000",
						},
					],
				},
				action: { outcome: "deny" },
			},
			"For all customers using any feature, when all of 4 conditions match, deny the request.",
		],
		[
			"a nested group",
			{
				...everyone,
				when: {
					any: [{ all: [{ signal: "pace", operator: "gt", value: "2.0000" }] }],
				},
				action: { outcome: "deny" },
			},
			"For all customers using any feature, when any of 1 condition match, deny the request.",
		],
	])("%s", (_case, document, summary) => {
		expect(policySummary(document, names)).toBe(summary);
	});

	test("a plan policy without a plan fails loud", () => {
		expect(() =>
			policySummary({ ...everyone, level: "plan", plan_id: null }, names),
		).toThrow("policy plan missing");
	});

	test("a route policy without a route chain fails loud", () => {
		expect(() =>
			policySummary(
				{ ...everyone, action: { outcome: "route", route_chain: null } },
				names,
			),
		).toThrow("policy route chain missing");
	});
});
