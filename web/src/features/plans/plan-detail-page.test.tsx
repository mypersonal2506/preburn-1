import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { toast } from "sonner";
import { afterEach, expect, test, vi } from "vitest";
import type { PlanResponse, PolicyResponse } from "@/client";
import {
	getDashboardOnboardingQueryKey,
	getPlanQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { sentBody } from "@/features/auth/auth-test-support";
import {
	archivedPlanPolicy,
	creatorDenyPolicy,
	creatorPlan,
	freePlan,
	knownFeatures,
	planPage,
	planRoute,
} from "@/features/plans/plans-test-support";
import {
	answersInOrder,
	policyPage,
} from "@/features/policies/policies-test-support";
import { setEnvironment } from "@/lib/environment-store";
import {
	type ApiAnswers,
	answerByEnvironment,
	holdAnswer,
	installationSettings,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

afterEach(() => {
	toast.dismiss();
	setEnvironment("test");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

function detailAnswers(
	plan: PlanResponse,
	patchAnswer: () => Response,
): ApiAnswers {
	return {
		...signedInAnswers,
		"GET /api/v1/plans": jsonAnswer(planPage([creatorPlan, freePlan])),
		"GET /api/v1/dashboard/features": jsonAnswer(knownFeatures),
		"GET /api/v1/policies": jsonAnswer(policyPage([])),
		[planRoute("GET", plan)]: jsonAnswer(plan),
		[planRoute("PATCH", plan)]: patchAnswer,
	};
}

async function findPoliciesTable(): Promise<HTMLElement> {
	const table = await screen.findByRole("table", {
		name: "Policies on this plan",
	});
	await waitFor(() => {
		expect(table).toHaveAttribute("aria-busy", "false");
	});
	return table;
}

function answersInTurn(plans: PlanResponse[]): () => Response {
	const remaining = [...plans];
	return () => {
		const plan = remaining.shift();
		if (plan === undefined) {
			throw new Error("plan answers exhausted");
		}
		return jsonAnswer(plan)();
	};
}

function isDefaultAnswer(): Response {
	return new Response(
		JSON.stringify({
			type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#plan_is_default",
			title: "Conflict",
			status: 409,
			detail: "the default plan of the environment cannot be archived",
			code: "plan_is_default",
		}),
		{ status: 409, headers: { "Content-Type": "application/problem+json" } },
	);
}

async function findSentence(plan: PlanResponse): Promise<HTMLElement> {
	await screen.findByRole("heading", { name: plan.name });
	return screen.getByRole("paragraph");
}

async function openBlank(
	user: UserEvent,
	phrase: string,
): Promise<HTMLElement> {
	await user.click(
		within(screen.getByRole("paragraph")).getByRole("button", { name: phrase }),
	);
	return screen.findByRole("dialog");
}

async function chooseMenuAction(
	user: UserEvent,
	action: string,
): Promise<void> {
	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(await screen.findByRole("menuitem", { name: action }));
}

test("shows the plan sentence, its customers and the filtered customer list link", async () => {
	stubApi(detailAnswers(creatorPlan, jsonAnswer(creatorPlan)));
	renderApp(`/plans/${creatorPlan.id}`);

	const sentence = await findSentence(creatorPlan);

	expect(sentence).toHaveTextContent(
		"Creator keeps 40.0% of revenue as margin.",
	);
	expect(screen.getByText("Customers on this plan")).toBeInTheDocument();
	expect(screen.getByText("12")).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "View customers" })).toHaveAttribute(
		"href",
		`/customers?plan_id=${creatorPlan.id}`,
	);
	expect(screen.getByRole("button", { name: /^Advanced/ })).toHaveTextContent(
		"1 custom hold time",
	);
	expect(screen.getByText("Active")).toBeInTheDocument();
	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
});

test("Policies on this plan lists the plan's policies page by page with their sentence", async () => {
	const user = userEvent.setup();
	const disabledPlanPolicy: PolicyResponse = {
		...creatorDenyPolicy,
		id: "pol_01jbvagescfn78y0938nkrka12",
		name: "Paused deny",
		status: "disabled",
	};
	const requests = stubApi({
		...detailAnswers(creatorPlan, jsonAnswer(creatorPlan)),
		"GET /api/v1/policies": answersInOrder([
			jsonAnswer(
				policyPage([creatorDenyPolicy, disabledPlanPolicy], "cursor-2"),
			),
			jsonAnswer(policyPage([archivedPlanPolicy])),
		]),
	});
	renderApp(`/plans/${creatorPlan.id}`);

	const table = await findPoliciesTable();
	const rows = within(table).getAllByRole("row").slice(1);

	expect(rows).toHaveLength(2);
	const [denyRow, pausedRow] = rows;
	if (denyRow === undefined || pausedRow === undefined) {
		throw new Error("policy rows missing");
	}
	expect(
		await within(denyRow).findByText(
			"For Creator customers using any feature, when pace is above 2.0x, deny the request.",
		),
	).toBeInTheDocument();
	expect(within(denyRow).getByText("Deny")).toBeInTheDocument();
	expect(
		within(denyRow).getByRole("link", { name: /Creator deny/ }),
	).toHaveAttribute("href", `/policies/${creatorDenyPolicy.id}`);
	expect(within(pausedRow).getByText("Paused deny")).toBeInTheDocument();
	expect(within(pausedRow).getByText("Disabled")).toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "Next" }));

	const nextTable = await findPoliciesTable();
	expect(await within(nextTable).findByText("Old deny")).toBeInTheDocument();
	expect(within(nextTable).getByText("Archived")).toBeInTheDocument();
	expect(within(nextTable).getAllByRole("row").slice(1)).toHaveLength(1);
	expect(
		requests
			.map((request) => new URL(request.url))
			.filter((url) => url.pathname === "/api/v1/policies")
			.map((url) => Object.fromEntries(url.searchParams)),
	).toEqual([
		{ plan_id: creatorPlan.id },
		{ plan_id: creatorPlan.id, cursor: "cursor-2" },
	]);
});

test("a plan without policies offers New policy", async () => {
	stubApi(detailAnswers(creatorPlan, jsonAnswer(creatorPlan)));
	renderApp(`/plans/${creatorPlan.id}`);

	const table = await findPoliciesTable();

	expect(
		within(table).getByText("No policies on this plan"),
	).toBeInTheDocument();
	expect(
		within(table).getByRole("link", { name: "New policy" }),
	).toHaveAttribute("href", "/policies/new");
});

test("the default plan carries a Default badge", async () => {
	stubApi({
		...detailAnswers(creatorPlan, jsonAnswer(creatorPlan)),
		"GET /api/v1/settings": jsonAnswer({
			...installationSettings,
			default_plan_id: creatorPlan.id,
		}),
	});
	renderApp(`/plans/${creatorPlan.id}`);
	await findSentence(creatorPlan);

	expect(await screen.findByText("Default")).toBeInTheDocument();
});

test("saving a plan marks the onboarding status stale", async () => {
	const user = userEvent.setup();
	stubApi(detailAnswers(creatorPlan, jsonAnswer(creatorPlan)));
	const { queryClient } = renderApp(`/plans/${creatorPlan.id}`);
	queryClient.setQueryData(getDashboardOnboardingQueryKey(), {
		first_check_at: null,
		has_api_key: true,
		has_plan: false,
		has_policy: false,
		has_revenue: false,
	});
	const sentence = await findSentence(creatorPlan);

	await user.click(within(sentence).getByRole("button", { name: "Creator" }));
	await user.type(
		within(await screen.findByRole("dialog")).getByRole("textbox", {
			name: "Plan name",
		}),
		" plus",
	);
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Plan saved")).toBeInTheDocument();
	expect(
		queryClient.getQueryState(getDashboardOnboardingQueryKey())?.isInvalidated,
	).toBe(true);
});

test("a save answered after a switch to live leaves the live page on Plan not found", async () => {
	const user = userEvent.setup();
	const heldSave = holdAnswer();
	stubApi({
		...detailAnswers(creatorPlan, jsonAnswer(creatorPlan)),
		[planRoute("GET", creatorPlan)]: answerByEnvironment({
			test: jsonAnswer(creatorPlan),
			live: problemAnswer(404, "not_found"),
		}),
		[planRoute("PATCH", creatorPlan)]: heldSave.answer,
	});
	const { queryClient } = renderApp(`/plans/${creatorPlan.id}`);
	const sentence = await findSentence(creatorPlan);
	await user.click(within(sentence).getByRole("button", { name: "Creator" }));
	await user.type(
		within(await screen.findByRole("dialog")).getByRole("textbox", {
			name: "Plan name",
		}),
		" plus",
	);
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Save" }));
	await user.click(screen.getByRole("radio", { name: "Live" }));
	expect(await screen.findByText("Plan not found")).toBeInTheDocument();

	heldSave.release(jsonAnswer({ ...creatorPlan, name: "Creator plus" }));
	await waitFor(() => {
		expect(queryClient.isMutating()).toBe(0);
	});

	expect(screen.getByText("Plan not found")).toBeInTheDocument();
	expect(
		queryClient.getQueryData(
			getPlanQueryKey({ path: { plan_id: creatorPlan.id } }),
		),
	).toBeUndefined();
});

test("switching to fixed allowance sends the allowance and keeps the target margin", async () => {
	const user = userEvent.setup();
	const savedPlan: PlanResponse = {
		...creatorPlan,
		mode: "fixed_allowance",
		allowance: "2.000000000",
	};
	const requests = stubApi(detailAnswers(creatorPlan, jsonAnswer(savedPlan)));
	renderApp(`/plans/${creatorPlan.id}`);
	const sentence = await findSentence(creatorPlan);

	const ruleDialog = await openBlank(user, "40.0%");
	await user.click(
		within(ruleDialog).getByRole("radio", { name: "Fixed allowance" }),
	);
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Allowance" }),
		"2",
	);
	await user.keyboard("{Escape}");
	expect(sentence).toHaveTextContent(
		"Creator customers get a fixed $2.00 of AI usage each period.",
	);
	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Plan saved")).toBeInTheDocument();
	expect(await sentBody(requests, planRoute("PATCH", creatorPlan))).toEqual({
		mode: "fixed_allowance",
		allowance: "2.000000000",
	});
	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
});

test("switching back to margin target shows the stored target margin and sends the mode", async () => {
	const user = userEvent.setup();
	const savedPlan: PlanResponse = {
		...freePlan,
		mode: "margin_target",
		allowance: null,
	};
	const requests = stubApi(detailAnswers(freePlan, jsonAnswer(savedPlan)));
	renderApp(`/plans/${freePlan.id}`);
	const sentence = await findSentence(freePlan);

	const ruleDialog = await openBlank(user, "a fixed $2.00");
	await user.click(
		within(ruleDialog).getByRole("radio", { name: "Margin target" }),
	);
	expect(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
	).toHaveValue("40");
	await user.keyboard("{Escape}");
	expect(sentence).toHaveTextContent("Free keeps 40.0% of revenue as margin.");
	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Plan saved")).toBeInTheDocument();
	expect(await sentBody(requests, planRoute("PATCH", freePlan))).toEqual({
		mode: "margin_target",
	});
});

test("hold time edits replace every hold time", async () => {
	const user = userEvent.setup();
	const requests = stubApi(detailAnswers(creatorPlan, jsonAnswer(creatorPlan)));
	renderApp(`/plans/${creatorPlan.id}`);
	await findSentence(creatorPlan);

	await user.click(screen.getByRole("button", { name: /^Advanced/ }));
	const advanced = await screen.findByRole("dialog", { name: "Advanced" });
	const holdTime = within(advanced).getByRole("textbox", { name: "Hold time" });
	expect(holdTime).toHaveValue("600");
	await user.click(within(advanced).getByRole("button", { name: "1 hour" }));
	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Plan saved")).toBeInTheDocument();
	expect(await sentBody(requests, planRoute("PATCH", creatorPlan))).toEqual({
		hold_times: { text_to_video: 3600 },
	});
});

test("Discard returns to the saved plan", async () => {
	const user = userEvent.setup();
	stubApi(detailAnswers(creatorPlan, jsonAnswer(creatorPlan)));
	renderApp(`/plans/${creatorPlan.id}`);
	const sentence = await findSentence(creatorPlan);

	await user.click(within(sentence).getByRole("button", { name: "Creator" }));
	await user.type(
		within(await screen.findByRole("dialog")).getByRole("textbox", {
			name: "Plan name",
		}),
		" plus",
	);
	await user.keyboard("{Escape}");
	expect(sentence).toHaveTextContent("Creator plus keeps");
	await user.click(screen.getByRole("button", { name: "Discard" }));

	expect(sentence).toHaveTextContent("Creator keeps");
	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
});

test("Archive asks for confirmation and shows why the default plan stays", async () => {
	const user = userEvent.setup();
	const requests = stubApi(detailAnswers(creatorPlan, isDefaultAnswer));
	renderApp(`/plans/${creatorPlan.id}`);
	await findSentence(creatorPlan);

	await chooseMenuAction(user, "Archive");
	const confirmation = await screen.findByRole("dialog", {
		name: "Archive Creator?",
	});
	await user.click(
		within(confirmation).getByRole("button", { name: "Archive" }),
	);

	expect(
		await within(confirmation).findByText(
			"the default plan of the environment cannot be archived",
		),
	).toBeInTheDocument();
	expect(await sentBody(requests, planRoute("PATCH", creatorPlan))).toEqual({
		status: "archived",
	});
});

test("Archive and Restore change the status", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		detailAnswers(
			creatorPlan,
			answersInTurn([{ ...creatorPlan, status: "archived" }, creatorPlan]),
		),
	);
	renderApp(`/plans/${creatorPlan.id}`);
	await findSentence(creatorPlan);

	await chooseMenuAction(user, "Archive");
	const confirmation = await screen.findByRole("dialog", {
		name: "Archive Creator?",
	});
	await user.click(
		within(confirmation).getByRole("button", { name: "Archive" }),
	);
	expect(await screen.findByText("Plan archived")).toBeInTheDocument();
	expect(screen.getByText("Archived")).toBeInTheDocument();
	expect(
		screen.queryByRole("dialog", { name: "Archive Creator?" }),
	).not.toBeInTheDocument();

	await chooseMenuAction(user, "Restore");
	expect(await screen.findByText("Plan restored")).toBeInTheDocument();
	const patchBodies = await Promise.all(
		requests
			.filter((request) => request.method === "PATCH")
			.map((request) => request.clone().json()),
	);
	expect(patchBodies).toEqual([{ status: "archived" }, { status: "active" }]);
});

test("an unknown plan shows not found", async () => {
	stubApi({
		...detailAnswers(creatorPlan, jsonAnswer(creatorPlan)),
		[planRoute("GET", creatorPlan)]: problemAnswer(404, "not_found"),
	});
	renderApp(`/plans/${creatorPlan.id}`);

	expect(await screen.findByText("Plan not found")).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "View plans" })).toHaveAttribute(
		"href",
		"/plans",
	);
});
