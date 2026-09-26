import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { PageBodyPlanResponse, PlanResponse } from "@/client";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";
import { PlanPicker } from "@/components/pickers/plan-picker";

const creatorPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay1",
	name: "Creator",
	mode: "margin_target",
	target_margin: "0.4000",
	allowance: null,
	customer_count: 12,
	hold_times: {},
	status: "active",
	created_at: "2026-09-01T10:00:00Z",
};

const freePlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay2",
	name: "Free",
	mode: "fixed_allowance",
	target_margin: "0.0000",
	allowance: "2.000000000",
	customer_count: 1,
	hold_times: {},
	status: "active",
	created_at: "2026-09-02T10:00:00Z",
};

const legacyPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay3",
	name: "Legacy",
	mode: "margin_target",
	target_margin: "0.2000",
	allowance: null,
	customer_count: 3,
	hold_times: {},
	status: "archived",
	created_at: "2026-08-01T10:00:00Z",
};

function planPage(
	items: PlanResponse[],
	nextCursor: string | null = null,
): PageBodyPlanResponse {
	return { items, next_cursor: nextCursor };
}

afterEach(() => {
	vi.unstubAllGlobals();
});

test("lists the active plans with name, rule and customer count", async () => {
	const user = userEvent.setup();
	stubApi(() => jsonResponse(planPage([creatorPlan, freePlan, legacyPlan])));
	renderWithQueries(<PlanPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(
		await screen.findByRole("option", { name: /Creator/ }),
	).toHaveTextContent("40.0% margin target, 12 customers");
	expect(screen.getByRole("option", { name: /Free/ })).toHaveTextContent(
		"$2.00 allowance, 1 customer",
	);
	expect(
		screen.queryByRole("option", { name: /Legacy/ }),
	).not.toBeInTheDocument();
});

test("loads every page of plans", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi((url) =>
		jsonResponse(
			url.searchParams.get("cursor") === "page-two"
				? planPage([freePlan])
				: planPage([creatorPlan], "page-two"),
		),
	);
	renderWithQueries(<PlanPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a plan" }));

	expect(await screen.findByRole("option", { name: /Free/ })).toBeVisible();
	expect(screen.getByRole("option", { name: /Creator/ })).toBeVisible();
	expect(requestedUrls).toHaveLength(2);
	expect(requestedUrls[0]?.searchParams.get("limit")).toBe("100");
	expect(requestedUrls[1]?.searchParams.get("cursor")).toBe("page-two");
});

test("the trigger shows the name of the selected plan, archived included", async () => {
	stubApi(() => jsonResponse(planPage([creatorPlan, legacyPlan])));
	renderWithQueries(<PlanPicker value={legacyPlan.id} onChange={vi.fn()} />);

	expect(screen.getByRole("button", { name: "Loading" })).toBeInTheDocument();
	expect(
		await screen.findByRole("button", { name: "Legacy" }),
	).toBeInTheDocument();
});

test("selecting a plan hands back the plan", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(planPage([creatorPlan, freePlan])));
	renderWithQueries(<PlanPicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a plan" }));
	await user.click(await screen.findByRole("option", { name: /Free/ }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith(freePlan);
});
