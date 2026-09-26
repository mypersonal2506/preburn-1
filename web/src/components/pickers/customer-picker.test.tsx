import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type {
	CustomerMarginResponse,
	PageBodyCustomerMarginResponse,
} from "@/client";
import { CustomerPicker } from "@/components/pickers/customer-picker";
import {
	jsonResponse,
	renderWithQueries,
	stubApi,
} from "@/components/pickers/picker-test-support";

const acme: CustomerMarginResponse = {
	id: "cust_01jbvagescfn78y0938nkrkay1",
	external_id: "org_acme",
	display_name: "Acme Inc",
	plan_id: "pln_01jbvagescfn78y0938nkrkay1",
	plan_name: "Creator",
	target_margin: "0.4000",
	revenue: "30.000000000",
	cost: "12.000000000",
	margin: "0.6000",
	pace: "0.8000",
	period_start: "2026-09-01T00:00:00Z",
	period_end: "2026-10-01T00:00:00Z",
};

const unnamedCustomer: CustomerMarginResponse = {
	id: "cust_01jbvagescfn78y0938nkrkay2",
	external_id: "org_7731",
	display_name: null,
	plan_id: null,
	plan_name: null,
	target_margin: null,
	revenue: "0.000000000",
	cost: "0.500000000",
	margin: null,
	pace: "inf",
	period_start: "2026-09-01T00:00:00Z",
	period_end: "2026-10-01T00:00:00Z",
};

function customerPage(
	items: CustomerMarginResponse[],
): PageBodyCustomerMarginResponse {
	return { items, next_cursor: null };
}

afterEach(() => {
	vi.unstubAllGlobals();
});

test("the search goes to the server and options show the plan", async () => {
	const user = userEvent.setup();
	const requestedUrls = stubApi((url) =>
		jsonResponse(
			customerPage(
				url.searchParams.get("search") === "acme"
					? [acme]
					: [acme, unnamedCustomer],
			),
		),
	);
	renderWithQueries(<CustomerPicker value={null} onChange={vi.fn()} />);

	await user.click(screen.getByRole("button", { name: "Select a customer" }));
	expect(
		await screen.findByRole("option", { name: /org_7731/ }),
	).toBeInTheDocument();
	await user.keyboard("acme");

	await waitFor(() => {
		expect(
			screen.queryByRole("option", { name: /org_7731/ }),
		).not.toBeInTheDocument();
	});
	expect(screen.getByRole("option", { name: /Acme Inc/ })).toHaveTextContent(
		"Creator",
	);
	expect(requestedUrls[0]?.pathname).toBe("/api/v1/dashboard/customers");
	expect(requestedUrls.at(-1)?.searchParams.get("search")).toBe("acme");
});

test("selecting a customer hands back the customer", async () => {
	const user = userEvent.setup();
	const onChange = vi.fn();
	stubApi(() => jsonResponse(customerPage([acme, unnamedCustomer])));
	renderWithQueries(<CustomerPicker value={null} onChange={onChange} />);

	await user.click(screen.getByRole("button", { name: "Select a customer" }));
	await user.click(await screen.findByRole("option", { name: /org_7731/ }));

	expect(onChange).toHaveBeenCalledExactlyOnceWith(unnamedCustomer);
});

test("the trigger shows the display name, or the external id without one", () => {
	stubApi(() => jsonResponse(customerPage([])));
	const { rerender } = renderWithQueries(
		<CustomerPicker value={acme} onChange={vi.fn()} />,
	);

	expect(screen.getByRole("button", { name: "Acme Inc" })).toBeInTheDocument();

	rerender(<CustomerPicker value={unnamedCustomer} onChange={vi.fn()} />);
	expect(screen.getByRole("button", { name: "org_7731" })).toBeInTheDocument();
});
