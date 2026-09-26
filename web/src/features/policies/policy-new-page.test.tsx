import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type {
	ModelResponse,
	OnboardingResponse,
	PolicyResponse,
} from "@/client";
import {
	getDashboardOnboardingQueryKey,
	getPolicyQueryKey,
} from "@/client/@tanstack/react-query.gen";
import {
	creatorPlan,
	decisionPage,
	editorAnswers,
	kling,
	POLICIES_PATH,
	paceDenyPolicy,
	policyInvalidAnswer,
	requestBodies,
	veoLite,
	videoModelPage,
	videoParameterMappings,
} from "@/features/policies/policies-test-support";
import type { RouteTarget } from "@/features/policies/policy-phrases";
import { setEnvironment } from "@/lib/environment-store";
import {
	answerByEnvironment,
	holdAnswer,
	installationSettings,
	jsonAnswer,
	problemAnswer,
	renderApp,
	stubApi,
} from "@/routes/-render-app";

const veoFast: RouteTarget = {
	provider: "fal_ai",
	model: "fal-ai/veo3.1/fast",
};

const onboardingState: OnboardingResponse = {
	first_check_at: "2026-09-20T08:00:00Z",
	has_api_key: true,
	has_plan: true,
	has_policy: false,
	has_revenue: false,
};

const QUERY_RETRY_WAIT_MILLISECONDS = 7_000;

const createdPolicy: PolicyResponse = {
	...paceDenyPolicy,
	id: "pol_01jbvagescfn78y0938nkrka09",
	name: "Deny requests for everyone",
	when: {
		all: [
			{ signal: "allowance_remaining", operator: "lte", value: "0.000000000" },
		],
	},
	enforcement: "hard",
	version: 1,
};

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
	vi.useRealTimers();
	vi.unstubAllGlobals();
});

function pricedVideoModel(
	target: RouteTarget,
	displayName: string,
	secondPrice: string,
): ModelResponse {
	return {
		...target,
		display_name: displayName,
		status: "active",
		key_prices: [
			{ meter: "output_seconds", unit_price: secondPrice, unit_quantity: 1 },
		],
	};
}

async function chooseStarter(label: string): Promise<void> {
	const user = userEvent.setup();
	const starters = await screen.findByRole("group", { name: "Starters" });
	await user.click(within(starters).getByRole("button", { name: label }));
}

test("the Stop at allowance chip saves exactly its document", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...editorAnswers([]),
		"POST /api/v1/policies": jsonAnswer(createdPolicy),
		"GET /api/v1/dashboard/decisions": jsonAnswer(decisionPage([])),
	});
	const { router, queryClient } = renderApp("/policies/new");
	queryClient.setQueryData(getDashboardOnboardingQueryKey(), onboardingState);

	await chooseStarter("Stop at allowance");
	expect(
		screen.getByRole("button", { name: "allowance left" }),
	).toBeInTheDocument();
	expect(screen.getByLabelText("Name")).toHaveValue(
		"Deny requests for everyone",
	);
	await user.click(screen.getByRole("button", { name: "Create policy" }));

	await waitFor(() =>
		expect(router.state.location.pathname).toBe(
			`/policies/${createdPolicy.id}`,
		),
	);
	expect(await requestBodies(requests, "POST", POLICIES_PATH)).toEqual([
		{
			name: "Deny requests for everyone",
			level: "everyone",
			plan_id: null,
			customer_id: null,
			feature: null,
			when: {
				all: [
					{
						signal: "allowance_remaining",
						operator: "lte",
						value: "0.000000000",
					},
				],
			},
			action: {
				outcome: "deny",
				route_chain: null,
				overrides: null,
				limit: null,
			},
			enforcement: "hard",
			on_unreachable: "allow",
			on_uncosted: "allow",
			status: "active",
		},
	]);
	expect(await screen.findByText("Policy created")).toBeInTheDocument();
	expect(
		queryClient.getQueryState(getDashboardOnboardingQueryKey())?.isInvalidated,
	).toBe(true);
});

test("a policy created after a switch to live stays on the new policy page", async () => {
	const user = userEvent.setup();
	const heldCreate = holdAnswer();
	stubApi({
		...editorAnswers([]),
		"POST /api/v1/policies": heldCreate.answer,
	});
	const { router, queryClient } = renderApp("/policies/new");
	await chooseStarter("Stop at allowance");
	await user.click(screen.getByRole("button", { name: "Create policy" }));
	await user.click(screen.getByRole("radio", { name: "Live" }));

	heldCreate.release(jsonAnswer(createdPolicy));
	await waitFor(() => {
		expect(queryClient.isMutating()).toBe(0);
	});

	expect(router.state.location.pathname).toBe("/policies/new");
	expect(
		queryClient.getQueryData(
			getPolicyQueryKey({ path: { policy_id: createdPolicy.id } }),
		),
	).toBeUndefined();
});

test("a switch to live resets a draft that names a test plan", async () => {
	const user = userEvent.setup();
	stubApi({
		...editorAnswers([]),
		"GET /api/v1/settings": answerByEnvironment({
			test: jsonAnswer({
				...installationSettings,
				default_plan_id: creatorPlan.id,
			}),
			live: jsonAnswer(installationSettings),
		}),
		[`GET /api/v1/plans/${creatorPlan.id}`]: answerByEnvironment({
			test: jsonAnswer(creatorPlan),
			live: problemAnswer(404, "not_found"),
		}),
		"GET /api/v1/pricing/models": jsonAnswer(videoModelPage),
		"GET /api/v1/policies/parameter-mappings": jsonAnswer(
			videoParameterMappings,
		),
	});
	renderApp("/policies/new");
	await screen.findByText("Acme AI");

	await chooseStarter("Cheaper model");
	await user.keyboard("{Escape}");
	expect(
		await screen.findByRole("button", { name: "Creator" }),
	).toBeInTheDocument();

	await user.click(screen.getByRole("radio", { name: "Live" }));

	expect(
		await screen.findByRole("button", { name: "which plan" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("heading", { name: "Something went wrong" }),
	).not.toBeInTheDocument();
});

test("the Cheaper model chip lists the cheapest models first", async () => {
	stubApi({
		...editorAnswers([]),
		"GET /api/v1/pricing/models": jsonAnswer({
			items: [
				pricedVideoModel(veoFast, "Veo 3.1 Fast", "0.150000000"),
				pricedVideoModel(kling, "Kling 2.5 Turbo Pro", "0.070000000"),
				pricedVideoModel(veoLite, "Veo 3.1 Lite", "0.050000000"),
			],
			next_cursor: null,
		}),
		"GET /api/v1/policies/parameter-mappings": jsonAnswer({ models: [] }),
	});
	renderApp("/policies/new");

	await chooseStarter("Cheaper model");
	await screen.findByRole("option", { name: /Veo 3\.1 Fast/ });

	expect(
		screen.getAllByRole("option").map((option) => option.textContent),
	).toEqual([
		"Veo 3.1 Lite$0.05 per second",
		"Kling 2.5 Turbo Pro$0.07 per second",
		"Veo 3.1 Fast$0.15 per second",
	]);
});

test("a 422 at a condition value underlines that blank and lists the message", async () => {
	const user = userEvent.setup();
	stubApi({
		...editorAnswers([]),
		"POST /api/v1/policies": policyInvalidAnswer([
			{ location: "body.when.all[0].value", message: "must be 0 or more" },
		]),
	});
	renderApp("/policies/new");

	await chooseStarter("Stop at allowance");
	const comparison = screen.getByRole("button", { name: "at most $0.00" });
	expect(comparison).toHaveAttribute("aria-invalid", "false");
	await user.click(screen.getByRole("button", { name: "Create policy" }));

	await waitFor(() =>
		expect(comparison).toHaveAttribute("aria-invalid", "true"),
	);
	expect(screen.getByText("must be 0 or more")).toBeInTheDocument();
	expect(
		screen.getByRole("button", { name: "allowance left" }),
	).toHaveAttribute("aria-invalid", "false");
});

test("saving with empty blanks lists them and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...editorAnswers([]),
		"GET /api/v1/pricing/models": jsonAnswer({ items: [], next_cursor: null }),
		"GET /api/v1/policies/parameter-mappings": jsonAnswer({ models: [] }),
	});
	renderApp("/policies/new");

	await chooseStarter("Cheaper model");
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Create policy" }));

	expect(await screen.findByText("Choose a plan")).toBeInTheDocument();
	expect(screen.getByText("Choose a model")).toBeInTheDocument();
	expect(screen.getByRole("button", { name: "which plan" })).toHaveAttribute(
		"aria-invalid",
		"true",
	);
	expect(await requestBodies(requests, "POST", POLICIES_PATH)).toEqual([]);
});

test("the facts show the matching customers and the other active policies", async () => {
	stubApi(editorAnswers([paceDenyPolicy]));
	renderApp("/policies/new");

	expect(await screen.findByText("3 customers")).toBeInTheDocument();
	expect(
		await screen.findByText("1 applies to all customers"),
	).toBeInTheDocument();
});

test("Duplicate starts from a copy of the policy's document", async () => {
	stubApi({
		...editorAnswers([]),
		[`GET ${POLICIES_PATH}/${paceDenyPolicy.id}`]: jsonAnswer(paceDenyPolicy),
	});
	renderApp(`/policies/new?duplicate=${paceDenyPolicy.id}`);

	expect(await screen.findByLabelText("Name")).toHaveValue(
		"Copy of Heavy users",
	);
	expect(screen.getByRole("button", { name: "above 2.0x" })).toBeVisible();
});

test("building a route sentence posts its plan, chain and setting", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...editorAnswers([]),
		"GET /api/v1/plans": jsonAnswer({
			items: [creatorPlan],
			next_cursor: null,
		}),
		[`GET /api/v1/plans/${creatorPlan.id}`]: jsonAnswer(creatorPlan),
		"GET /api/v1/pricing/models": jsonAnswer(videoModelPage),
		"GET /api/v1/policies/parameter-mappings": jsonAnswer(
			videoParameterMappings,
		),
		"POST /api/v1/policies": policyInvalidAnswer([]),
	});
	renderApp("/policies/new");

	await user.click(
		await screen.findByRole("button", { name: "all customers" }),
	);
	const levels = await screen.findByRole("group", { name: "Applies to" });
	await user.click(
		within(levels).getByRole("button", { name: "Customers on a plan" }),
	);
	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.click(await screen.findByRole("option", { name: /Creator/ }));
	await user.keyboard("{Escape}");
	expect(await screen.findByRole("button", { name: "Creator" })).toBeVisible();

	await user.click(screen.getByRole("button", { name: "allow" }));
	const outcomes = await screen.findByRole("group", { name: "Outcome" });
	await user.click(within(outcomes).getByRole("button", { name: /Route/ }));
	await user.click(
		await screen.findByRole("option", { name: /Veo 3\.1 Lite/ }),
	);
	expect(
		await screen.findByRole("button", { name: "Veo 3.1 Lite" }),
	).toBeVisible();

	await user.click(screen.getByRole("button", { name: "Add clause" }));
	await user.click(
		await screen.findByRole("menuitem", { name: "Fallback model" }),
	);
	await user.click(await screen.findByRole("option", { name: /Kling/ }));
	await user.click(screen.getByRole("button", { name: "Add clause" }));
	await user.click(await screen.findByRole("menuitem", { name: "Setting" }));
	(await screen.findByRole("combobox", { name: "Value" })).focus();
	await user.keyboard("{Enter}");
	await user.click(await screen.findByRole("option", { name: "8s" }));
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Create policy" }));

	await waitFor(async () =>
		expect(await requestBodies(requests, "POST", POLICIES_PATH)).toHaveLength(
			1,
		),
	);
	const [body] = await requestBodies(requests, "POST", POLICIES_PATH);
	expect(body).toMatchObject({
		name: "Route requests for Creator customers",
		level: "plan",
		plan_id: creatorPlan.id,
		action: {
			outcome: "route",
			route_chain: [veoLite, kling],
			overrides: { duration: "8s" },
			limit: null,
		},
	});
	expect(
		screen.getByText(
			(_content, element) =>
				element?.tagName === "P" &&
				element.textContent?.startsWith(
					"For Creator customers using any feature, always route to Veo 3.1 Lite, or Kling 2.5 Turbo Pro if that has no price, at 8s.",
				) === true,
		),
	).toBeInTheDocument();
});

test("the Cap on pace chip adds a setting and a limit to the cap", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...editorAnswers([]),
		"GET /api/v1/policies/parameter-mappings": jsonAnswer(
			videoParameterMappings,
		),
		"POST /api/v1/policies": policyInvalidAnswer([]),
	});
	renderApp("/policies/new");

	await chooseStarter("Cap on pace");
	const settings = await screen.findByRole("dialog", {
		name: "which settings",
	});
	await user.click(
		await within(settings).findByRole("button", { name: "Add setting" }),
	);
	within(settings).getByRole("combobox", { name: "Value" }).focus();
	await user.keyboard("{Enter}");
	await user.click(await screen.findByRole("option", { name: "4s" }));
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Add clause" }));
	await user.click(await screen.findByRole("menuitem", { name: "Limit" }));
	await user.type(
		await screen.findByRole("textbox", { name: "Most per period" }),
		"20",
	);
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Create policy" }));

	await waitFor(async () =>
		expect(await requestBodies(requests, "POST", POLICIES_PATH)).toHaveLength(
			1,
		),
	);
	const [body] = await requestBodies(requests, "POST", POLICIES_PATH);
	expect(body).toMatchObject({
		when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
		action: {
			outcome: "cap",
			route_chain: null,
			overrides: { duration: "4s" },
			limit: { kind: "count", value: "20" },
		},
	});
	expect(
		screen.getByRole("button", { name: "20 requests" }),
	).toBeInTheDocument();
});

test("a failed settings load shows in the sentence and Try again loads them", async () => {
	vi.useFakeTimers({ shouldAdvanceTime: true });
	const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
	let mappingsAnswer = problemAnswer(500, "internal_error");
	stubApi({
		...editorAnswers([]),
		"GET /api/v1/policies/parameter-mappings": () => mappingsAnswer(),
	});
	renderApp("/policies/new");

	await chooseStarter("Cap on pace");
	await act(async () => {
		await vi.advanceTimersByTimeAsync(QUERY_RETRY_WAIT_MILLISECONDS);
	});

	const settings = await screen.findByRole("dialog", {
		name: "which settings",
	});
	expect(
		await within(settings).findByText("Settings did not load"),
	).toBeInTheDocument();
	expect(
		within(screen.getByRole("paragraph")).getByText("Settings did not load"),
	).toBeInTheDocument();

	mappingsAnswer = jsonAnswer(videoParameterMappings);
	await user.click(within(settings).getByRole("button", { name: "Try again" }));

	expect(
		await within(settings).findByRole("button", { name: "Add setting" }),
	).toBeInTheDocument();
	expect(screen.queryByText("Settings did not load")).not.toBeInTheDocument();
});
