import { screen, waitFor, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { PlanResponse } from "@/client";
import { getPlanQueryKey } from "@/client/@tanstack/react-query.gen";
import { sentBody, sentRoutes } from "@/features/auth/auth-test-support";
import {
	creatorPlan,
	freePlan,
	knownFeatures,
	planPage,
	planRoute,
} from "@/features/plans/plans-test-support";
import { setEnvironment } from "@/lib/environment-store";
import {
	type ApiAnswers,
	holdAnswer,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const CREATE_ROUTE = "POST /api/v1/plans";

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

function newPlanAnswers(createAnswer: () => Response): ApiAnswers {
	return {
		...signedInAnswers,
		"GET /api/v1/plans": jsonAnswer(planPage([creatorPlan, freePlan])),
		"GET /api/v1/dashboard/features": jsonAnswer(knownFeatures),
		[CREATE_ROUTE]: createAnswer,
	};
}

function createdAnswers(plan: PlanResponse): ApiAnswers {
	return {
		...newPlanAnswers(jsonAnswer(plan)),
		[planRoute("GET", plan)]: jsonAnswer(plan),
	};
}

async function findSentence(): Promise<HTMLElement> {
	await screen.findByRole("heading", { name: "New plan" });
	return screen.getByRole("paragraph");
}

async function typeName(user: UserEvent, name: string): Promise<void> {
	const nameDialog = await openBlank(user, "This plan");
	await user.type(
		within(nameDialog).getByRole("textbox", { name: "Plan name" }),
		name,
	);
	await user.keyboard("{Escape}");
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

test("a margin target plan posts its target margin and opens the plan", async () => {
	const user = userEvent.setup();
	const requests = stubApi(createdAnswers(creatorPlan));
	const { router } = renderApp("/plans/new");
	const sentence = await findSentence();

	expect(sentence).toHaveTextContent(
		"This plan keeps what share of revenue as margin.",
	);
	await typeName(user, "Creator");
	const ruleDialog = await openBlank(user, "what share");
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
		"40",
	);
	await user.keyboard("{Escape}");
	expect(sentence).toHaveTextContent(
		"Creator keeps 40.0% of revenue as margin.",
	);
	await user.click(screen.getByRole("button", { name: "Create plan" }));

	expect(
		await screen.findByRole("heading", { name: "Creator" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe(`/plans/${creatorPlan.id}`);
	expect(await sentBody(requests, CREATE_ROUTE)).toEqual({
		name: "Creator",
		mode: "margin_target",
		target_margin: "0.4000",
		hold_times: {},
	});
});

test("a plan created after a switch to live stays on the new plan page", async () => {
	const user = userEvent.setup();
	const heldCreate = holdAnswer();
	stubApi({
		...newPlanAnswers(jsonAnswer(creatorPlan)),
		[CREATE_ROUTE]: heldCreate.answer,
	});
	const { router, queryClient } = renderApp("/plans/new");
	await findSentence();
	await typeName(user, "Creator");
	const ruleDialog = await openBlank(user, "what share");
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
		"40",
	);
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Create plan" }));
	await user.click(screen.getByRole("radio", { name: "Live" }));

	heldCreate.release(jsonAnswer(creatorPlan));
	await waitFor(() => {
		expect(queryClient.isMutating()).toBe(0);
	});

	expect(router.state.location.pathname).toBe("/plans/new");
	expect(
		queryClient.getQueryData(
			getPlanQueryKey({ path: { plan_id: creatorPlan.id } }),
		),
	).toBeUndefined();
});

test("a fixed allowance plan posts its allowance", async () => {
	const user = userEvent.setup();
	const requests = stubApi(createdAnswers(freePlan));
	renderApp("/plans/new");
	const sentence = await findSentence();

	await typeName(user, "Free");
	const ruleDialog = await openBlank(user, "what share");
	await user.click(
		within(ruleDialog).getByRole("radio", { name: "Fixed allowance" }),
	);
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Allowance" }),
		"2",
	);
	await user.keyboard("{Escape}");
	expect(sentence).toHaveTextContent(
		"Free customers get a fixed $2.00 of AI usage each period.",
	);
	await user.click(screen.getByRole("button", { name: "Create plan" }));

	expect(
		await screen.findByRole("heading", { name: "Free" }),
	).toBeInTheDocument();
	expect(await sentBody(requests, CREATE_ROUTE)).toEqual({
		name: "Free",
		mode: "fixed_allowance",
		allowance: "2.000000000",
		hold_times: {},
	});
});

test("an incomplete sentence shows its problems and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(createdAnswers(creatorPlan));
	renderApp("/plans/new");
	const sentence = await findSentence();

	await user.click(screen.getByRole("button", { name: "Create plan" }));

	expect(
		within(screen.getByRole("alert"))
			.getAllByRole("listitem")
			.map((item) => item.textContent),
	).toEqual(["Enter a name", "Enter a target margin"]);
	expect(
		within(sentence).getByRole("button", { name: "This plan" }),
	).toHaveAttribute("aria-invalid", "true");
	expect(
		within(sentence).getByRole("button", { name: "what share" }),
	).toHaveAttribute("aria-invalid", "true");
	expect(sentRoutes(requests)).not.toContain(CREATE_ROUTE);
});

test("hold time rows validate duration bounds", async () => {
	const user = userEvent.setup();
	const requests = stubApi(createdAnswers(creatorPlan));
	renderApp("/plans/new");
	await findSentence();
	await typeName(user, "Creator");
	const ruleDialog = await openBlank(user, "what share");
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
		"40",
	);
	await user.keyboard("{Escape}");

	const advancedButton = screen.getByRole("button", { name: /^Advanced/ });
	expect(advancedButton).toHaveTextContent("Default hold times");
	await user.click(advancedButton);
	const advanced = await screen.findByRole("dialog", { name: "Advanced" });
	await user.click(
		within(advanced).getByRole("button", { name: "Add hold time" }),
	);
	await user.click(
		within(advanced).getByRole("button", { name: "Select a feature" }),
	);
	await user.click(
		await screen.findByRole("option", { name: "text to video" }),
	);
	await user.type(
		within(advanced).getByRole("textbox", { name: "Hold time" }),
		"10",
	);
	await user.click(screen.getByRole("button", { name: "Create plan" }));

	expect(
		within(screen.getByRole("alert")).getByRole("listitem"),
	).toHaveTextContent("Fix the hold times under Advanced");
	expect(sentRoutes(requests)).not.toContain(CREATE_ROUTE);

	expect(advancedButton).toHaveTextContent("1 custom hold time");
	await user.click(advancedButton);
	const reopened = await screen.findByRole("dialog", { name: "Advanced" });
	expect(
		within(reopened).getByText("Use 30 seconds to 24 hours"),
	).toBeInTheDocument();
	const holdTime = within(reopened).getByRole("textbox", { name: "Hold time" });
	expect(holdTime).toHaveAttribute("aria-invalid", "true");
	await user.clear(holdTime);
	await user.type(holdTime, "600");
	expect(
		within(reopened).queryByText("Use 30 seconds to 24 hours"),
	).not.toBeInTheDocument();
	await user.click(screen.getByRole("button", { name: "Create plan" }));

	await screen.findByRole("heading", { name: "Creator" });
	expect(await sentBody(requests, CREATE_ROUTE)).toEqual({
		name: "Creator",
		mode: "margin_target",
		target_margin: "0.4000",
		hold_times: { text_to_video: 600 },
	});
});

test("a removed hold time row is not sent", async () => {
	const user = userEvent.setup();
	const requests = stubApi(createdAnswers(creatorPlan));
	renderApp("/plans/new");
	await findSentence();
	await typeName(user, "Creator");
	const ruleDialog = await openBlank(user, "what share");
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
		"40",
	);
	await user.keyboard("{Escape}");

	await user.click(screen.getByRole("button", { name: /^Advanced/ }));
	const advanced = await screen.findByRole("dialog", { name: "Advanced" });
	await user.click(
		within(advanced).getByRole("button", { name: "Add hold time" }),
	);
	await user.click(
		within(advanced).getByRole("button", { name: "Remove hold time" }),
	);
	expect(
		within(advanced).queryByRole("textbox", { name: "Hold time" }),
	).not.toBeInTheDocument();
	await user.click(screen.getByRole("button", { name: "Create plan" }));

	await screen.findByRole("heading", { name: "Creator" });
	expect(await sentBody(requests, CREATE_ROUTE)).toEqual({
		name: "Creator",
		mode: "margin_target",
		target_margin: "0.4000",
		hold_times: {},
	});
});

test("409 plan_name_taken shows on the name blank", async () => {
	const user = userEvent.setup();
	stubApi(newPlanAnswers(problemAnswer(409, "plan_name_taken")));
	renderApp("/plans/new");
	const sentence = await findSentence();
	await typeName(user, "Creator");
	const ruleDialog = await openBlank(user, "what share");
	await user.type(
		within(ruleDialog).getByRole("textbox", { name: "Target margin" }),
		"40",
	);
	await user.keyboard("{Escape}");

	await user.click(screen.getByRole("button", { name: "Create plan" }));

	const nameBlank = within(sentence).getByRole("button", { name: "Creator" });
	await vi.waitFor(() => {
		expect(nameBlank).toHaveAttribute("aria-invalid", "true");
	});
	expect(
		screen.queryByRole("textbox", { name: "Name" }),
	).not.toBeInTheDocument();
	expect(
		within(screen.getByRole("alert")).getByRole("listitem"),
	).toHaveTextContent("Another plan has this name");
	const nameDialog = await openBlank(user, "Creator");
	expect(nameDialog).toHaveTextContent("Another plan has this name");
	await user.type(
		within(nameDialog).getByRole("textbox", { name: "Plan name" }),
		" plus",
	);

	expect(
		within(sentence).getByRole("button", { name: "Creator plus" }),
	).toHaveAttribute("aria-invalid", "false");
	expect(screen.queryByRole("alert")).not.toBeInTheDocument();
});
