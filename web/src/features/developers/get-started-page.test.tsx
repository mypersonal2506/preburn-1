import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import type { OnboardingResponse } from "@/client";
import {
	firstDecisionEvent,
	ONBOARDING_ROUTE,
	onboardingAllDone,
	onboardingNothingDone,
	onboardingWithApiKey,
} from "@/features/developers/developers-test-support";
import {
	FakeEventSource,
	latestEventSource,
} from "@/lib/event-source-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

beforeEach(() => {
	FakeEventSource.opened = [];
	vi.stubGlobal("EventSource", FakeEventSource);
});

afterEach(() => {
	vi.unstubAllGlobals();
});

function onboardingAnswers(onboarding: OnboardingResponse): ApiAnswers {
	return { ...signedInAnswers, [ONBOARDING_ROUTE]: jsonAnswer(onboarding) };
}

async function findStep(title: string): Promise<HTMLElement> {
	const steps = await screen.findByRole("list", { name: "Setup steps" });
	const heading = within(steps).getByRole("heading", { name: title });
	const step = heading.closest("li");
	if (step === null) {
		throw new Error(`setup step missing title=${title}`);
	}
	return step;
}

test("shows the progress and expands only the first open step", async () => {
	stubApi(onboardingAnswers(onboardingWithApiKey));
	renderApp("/developers/get-started");

	const firstCheckStep = await findStep("Send a first check");

	expect(screen.getByText("1 of 5 done")).toBeInTheDocument();
	expect(
		screen.getByRole("progressbar", { name: "Setup progress" }),
	).toHaveAttribute("aria-valuenow", "20");
	const apiKeyStep = await findStep("Create an API key");
	expect(within(apiKeyStep).getByText("Done")).toBeInTheDocument();
	expect(firstCheckStep).toHaveAttribute("aria-current", "step");
	expect(
		within(firstCheckStep).getByRole("tab", { name: "curl" }),
	).toHaveAttribute("aria-selected", "true");
	expect(within(firstCheckStep).getByText(/curl -X POST/)).toHaveTextContent(
		`${window.location.origin}/api/v1/check`,
	);
	expect(within(firstCheckStep).getByText(/curl -X POST/)).toHaveTextContent(
		"Authorization: Bearer YOUR_API_KEY",
	);
	for (const title of ["Create a plan", "Create a policy", "Record revenue"]) {
		const step = await findStep(title);
		expect(step).not.toHaveAttribute("aria-current");
		expect(within(step).queryByRole("link")).not.toBeInTheDocument();
		expect(within(step).queryByText("Done")).not.toBeInTheDocument();
	}
});

test("the Python tab of the first check uses this dashboard's origin", async () => {
	const user = userEvent.setup();
	stubApi(onboardingAnswers(onboardingWithApiKey));
	renderApp("/developers/get-started");
	const firstCheckStep = await findStep("Send a first check");

	await user.click(within(firstCheckStep).getByRole("tab", { name: "Python" }));

	const pythonCode = within(firstCheckStep).getByText(/from preburn import/);
	expect(pythonCode).toHaveTextContent('api_key="YOUR_API_KEY"');
	expect(pythonCode).toHaveTextContent(`base_url="${window.location.origin}"`);
	expect(pythonCode).toHaveTextContent("preburn.check(");
});

test("the first check step completes when a streamed decision arrives", async () => {
	const requests = stubApi(onboardingAnswers(onboardingWithApiKey));
	renderApp("/developers/get-started");
	const firstCheckStep = await findStep("Send a first check");
	expect(
		within(firstCheckStep).getByText("Waiting for the first check"),
	).toBeInTheDocument();
	expect(FakeEventSource.opened).toHaveLength(1);
	const source = latestEventSource();

	act(() => source.open());
	act(() => source.sendDecision("1726488000000-0", firstDecisionEvent));

	expect(await screen.findByText("2 of 5 done")).toBeInTheDocument();
	const completedStep = await findStep("Send a first check");
	expect(within(completedStep).getByText("Done")).toBeInTheDocument();
	expect(completedStep).not.toHaveAttribute("aria-current");
	const planStep = await findStep("Create a plan");
	expect(planStep).toHaveAttribute("aria-current", "step");
	expect(
		within(planStep).getByRole("link", { name: "Create plan" }),
	).toHaveAttribute("href", "/plans/new");
	expect(source.closed).toBe(true);
	expect(
		requests.filter(
			(request) =>
				new URL(request.url).pathname === "/api/v1/dashboard/onboarding",
		),
	).toHaveLength(1);
});

test("the API key step opens the create modal", async () => {
	const user = userEvent.setup();
	stubApi(onboardingAnswers(onboardingNothingDone));
	renderApp("/developers/get-started");
	const apiKeyStep = await findStep("Create an API key");

	expect(apiKeyStep).toHaveAttribute("aria-current", "step");
	expect(FakeEventSource.opened).toHaveLength(0);

	await user.click(
		within(apiKeyStep).getByRole("button", { name: "Create API key" }),
	);

	expect(
		await screen.findByRole("dialog", { name: "Create API key" }),
	).toBeVisible();
});

test("the revenue step shows the revenue request", async () => {
	stubApi(
		onboardingAnswers({
			...onboardingAllDone,
			has_revenue: false,
		}),
	);
	renderApp("/developers/get-started");

	const revenueStep = await findStep("Record revenue");

	expect(screen.getByText("4 of 5 done")).toBeInTheDocument();
	const revenueCode = within(revenueStep).getByText(/curl -X POST/);
	expect(revenueCode).toHaveTextContent(
		`${window.location.origin}/api/v1/revenue`,
	);
	expect(revenueCode).toHaveTextContent('"kind": "subscription"');
});

test("with every step done it shows You're set up with where to go next", async () => {
	stubApi(onboardingAnswers(onboardingAllDone));
	renderApp("/developers/get-started");

	expect(
		await screen.findByRole("heading", { name: "Get started" }),
	).toBeInTheDocument();
	expect(await screen.findByText("You're set up")).toBeInTheDocument();
	expect(screen.getByText("5 of 5 done")).toBeInTheDocument();
	const main = screen.getByRole("main");
	expect(within(main).getByRole("link", { name: "Overview" })).toHaveAttribute(
		"href",
		"/",
	);
	expect(within(main).getByRole("link", { name: "Decisions" })).toHaveAttribute(
		"href",
		"/decisions",
	);
	expect(
		screen.queryByRole("list", { name: "Setup steps" }),
	).not.toBeInTheDocument();
});

test("a failed onboarding request shows the error with Try again", async () => {
	stubApi({
		...signedInAnswers,
		[ONBOARDING_ROUTE]: problemAnswer(403, "scope_forbidden"),
	});
	renderApp("/developers/get-started");

	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
	expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
});
