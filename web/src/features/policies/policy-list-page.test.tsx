import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import {
	creatorPlan,
	creatorRoutePolicy,
	POLICIES_PATH,
	paceDenyPolicy,
	policyPage,
	videoModelPage,
} from "@/features/policies/policies-test-support";
import {
	type ApiAnswers,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

afterEach(() => {
	vi.unstubAllGlobals();
});

function listAnswers(policyAnswer: () => Response): ApiAnswers {
	return {
		...signedInAnswers,
		"GET /api/v1/policies": policyAnswer,
		[`GET /api/v1/plans/${creatorPlan.id}`]: jsonAnswer(creatorPlan),
		"GET /api/v1/pricing/models": jsonAnswer(videoModelPage),
	};
}

function listQueries(requests: readonly Request[]): Record<string, string>[] {
	return requests
		.map((request) => new URL(request.url))
		.filter((url) => url.pathname === POLICIES_PATH)
		.map((url) => Object.fromEntries(url.searchParams));
}

async function findPolicyRows(): Promise<HTMLElement[]> {
	const table = await screen.findByRole("table", { name: "Policies" });
	await within(table).findByText(/Veo 3\.1 Lite/);
	return within(table).getAllByRole("row").slice(1);
}

test("the list opens on active policies with their sentence", async () => {
	const requests = stubApi(
		listAnswers(jsonAnswer(policyPage([creatorRoutePolicy, paceDenyPolicy]))),
	);
	renderApp("/policies");

	const [routeRow, denyRow] = await findPolicyRows();

	expect(listQueries(requests)).toEqual([{ status: "active" }]);
	expect(
		screen.getAllByRole("columnheader").map((header) => header.textContent),
	).toEqual(["Policy", "Applies to", "Outcome", "Status", "Updated"]);
	if (routeRow === undefined || denyRow === undefined) {
		throw new Error("policy rows missing");
	}
	expect(
		within(routeRow).getByText(
			"For Creator customers using text to video, when pace is above 2.0x, route to Veo 3.1 Lite.",
		),
	).toBeInTheDocument();
	expect(within(routeRow).getByText("Creator customers")).toBeInTheDocument();
	expect(within(routeRow).getByText("Route")).toBeInTheDocument();
	expect(within(denyRow).getByText("Deny")).toBeInTheDocument();
	expect(within(routeRow).getByText("Active")).toBeInTheDocument();
	expect(
		within(routeRow).getByRole("link", { name: /Cheaper video for Creator/ }),
	).toHaveAttribute("href", `/policies/${creatorRoutePolicy.id}`);
	expect(within(denyRow).getByText("All customers")).toBeInTheDocument();
	expect(within(denyRow).getByText("2 hours ago")).toBeInTheDocument();
});

test("the status chips filter the list and All drops the filter", async () => {
	const user = userEvent.setup();
	const requests = stubApi(listAnswers(jsonAnswer(policyPage([]))));
	const { router } = renderApp("/policies");
	const filters = await screen.findByRole("group", { name: "Policy filters" });

	await user.click(within(filters).getByRole("button", { name: "Disabled" }));
	expect(await screen.findByText("No disabled policies")).toBeInTheDocument();
	expect(router.state.location.search).toEqual({ status: "disabled" });

	await user.click(within(filters).getByRole("button", { name: "All" }));
	expect(await screen.findByText("No policies yet")).toBeInTheDocument();

	await waitFor(() =>
		expect(listQueries(requests)).toEqual([
			{ status: "active" },
			{ status: "disabled" },
			{},
		]),
	);
});

test("a failed list offers Try again", async () => {
	stubApi(listAnswers(problemAnswer(422, "validation_failed")));
	renderApp("/policies");

	expect(await screen.findByText("Something went wrong")).toBeInTheDocument();
	expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
});

test("an empty environment offers New policy", async () => {
	stubApi(listAnswers(jsonAnswer(policyPage([]))));
	renderApp("/policies");

	expect(await screen.findByText("No active policies")).toBeInTheDocument();
	expect(
		screen.getAllByRole("link", { name: "New policy" }).at(-1),
	).toHaveAttribute("href", "/policies/new");
});
