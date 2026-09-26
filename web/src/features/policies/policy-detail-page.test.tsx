import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { PolicyResponse } from "@/client";
import {
	getDashboardCustomerQueryKey,
	getPolicyQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { acmeCustomer } from "@/features/decisions/decisions-test-support";
import {
	answersInOrder,
	decisionPage,
	editorAnswers,
	POLICIES_PATH,
	paceDenyPolicy,
	requestBodies,
	routedDecision,
} from "@/features/policies/policies-test-support";
import { setEnvironment } from "@/lib/environment-store";
import {
	type ApiAnswers,
	answerByEnvironment,
	holdAnswer,
	jsonAnswer,
	problemAnswer,
	renderApp,
	stubApi,
} from "@/routes/-render-app";

const POLICY_PATH = `${POLICIES_PATH}/${paceDenyPolicy.id}`;

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

function detailAnswers(policyAnswer: () => Response): ApiAnswers {
	return {
		...editorAnswers([paceDenyPolicy]),
		[`GET ${POLICY_PATH}`]: policyAnswer,
		"GET /api/v1/dashboard/decisions": jsonAnswer(
			decisionPage([routedDecision]),
		),
		"GET /api/v1/pricing/models": jsonAnswer({ items: [], next_cursor: null }),
	};
}

function withStatus(
	status: PolicyResponse["status"],
	version: number,
): PolicyResponse {
	return { ...paceDenyPolicy, status, version };
}

async function chooseMenuItem(name: string): Promise<void> {
	const user = userEvent.setup();
	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(await screen.findByRole("menuitem", { name }));
}

test("the header shows the name, status, version and update time", async () => {
	stubApi(detailAnswers(jsonAnswer(paceDenyPolicy)));
	renderApp(`/policies/${paceDenyPolicy.id}`);

	expect(
		await screen.findByRole("heading", { name: "Heavy users" }),
	).toBeInTheDocument();
	expect(screen.getByText("Active")).toBeInTheDocument();
	expect(screen.getByText(/Version 3, updated/)).toHaveTextContent(
		"Version 3, updated 2 hours ago",
	);
	expect(screen.getByRole("button", { name: "above 2.0x" })).toBeVisible();
});

test("Disable, Enable and Archive patch the status and update the header", async () => {
	const user = userEvent.setup();
	const requests = stubApi({
		...detailAnswers(jsonAnswer(paceDenyPolicy)),
		[`PATCH ${POLICY_PATH}`]: answersInOrder([
			jsonAnswer(withStatus("disabled", 4)),
			jsonAnswer(withStatus("active", 5)),
			jsonAnswer(withStatus("archived", 6)),
		]),
	});
	renderApp(`/policies/${paceDenyPolicy.id}`);
	await screen.findByRole("heading", { name: "Heavy users" });

	await chooseMenuItem("Disable");
	expect(await screen.findByText(/Version 4/)).toBeInTheDocument();
	expect(screen.getByText("Disabled")).toBeInTheDocument();

	await chooseMenuItem("Enable");
	expect(await screen.findByText(/Version 5/)).toBeInTheDocument();
	expect(screen.getByText("Active")).toBeInTheDocument();

	await chooseMenuItem("Archive");
	const dialog = await screen.findByRole("dialog", { name: "Archive policy?" });
	await user.click(within(dialog).getByRole("button", { name: "Archive" }));
	expect(await screen.findByText(/Version 6/)).toBeInTheDocument();
	expect(screen.getByText("Archived")).toBeInTheDocument();

	expect(await requestBodies(requests, "PATCH", POLICY_PATH)).toEqual([
		{ status: "disabled" },
		{ status: "active" },
		{ status: "archived" },
	]);
});

test("Save patches the edited document without the status", async () => {
	const user = userEvent.setup();
	const savedPolicy: PolicyResponse = {
		...paceDenyPolicy,
		action: { ...paceDenyPolicy.action, outcome: "allow" },
		version: 4,
	};
	const requests = stubApi({
		...detailAnswers(jsonAnswer(paceDenyPolicy)),
		[`PATCH ${POLICY_PATH}`]: jsonAnswer(savedPolicy),
	});
	renderApp(`/policies/${paceDenyPolicy.id}`);

	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
	await user.click(await screen.findByRole("button", { name: "deny" }));
	const outcomes = await screen.findByRole("group", { name: "Outcome" });
	await user.click(within(outcomes).getByRole("button", { name: /Allow/ }));
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Policy saved")).toBeInTheDocument();
	expect(await requestBodies(requests, "PATCH", POLICY_PATH)).toEqual([
		{
			name: "Heavy users",
			level: "everyone",
			plan_id: null,
			customer_id: null,
			feature: null,
			when: { all: [{ signal: "pace", operator: "gt", value: "2.0000" }] },
			action: {
				outcome: "allow",
				route_chain: null,
				overrides: null,
				limit: null,
			},
			enforcement: "soft",
			on_unreachable: "allow",
			on_uncosted: "allow",
		},
	]);
	await waitFor(() =>
		expect(
			screen.queryByRole("button", { name: "Save" }),
		).not.toBeInTheDocument(),
	);
});

test("a save answered after a switch to live leaves the live page on Policy not found", async () => {
	const user = userEvent.setup();
	const heldSave = holdAnswer();
	stubApi({
		...detailAnswers(
			answerByEnvironment({
				test: jsonAnswer(paceDenyPolicy),
				live: problemAnswer(404, "not_found"),
			}),
		),
		[`PATCH ${POLICY_PATH}`]: heldSave.answer,
	});
	const { queryClient } = renderApp(`/policies/${paceDenyPolicy.id}`);
	await user.click(await screen.findByRole("button", { name: "deny" }));
	const outcomes = await screen.findByRole("group", { name: "Outcome" });
	await user.click(within(outcomes).getByRole("button", { name: /Allow/ }));
	await user.keyboard("{Escape}");
	await user.click(screen.getByRole("button", { name: "Save" }));
	await user.click(screen.getByRole("radio", { name: "Live" }));
	expect(await screen.findByText("Policy not found")).toBeInTheDocument();

	heldSave.release(
		jsonAnswer({
			...paceDenyPolicy,
			action: { ...paceDenyPolicy.action, outcome: "allow" },
			version: 4,
		}),
	);
	await waitFor(() => {
		expect(queryClient.isMutating()).toBe(0);
	});

	expect(screen.getByText("Policy not found")).toBeInTheDocument();
	expect(
		queryClient.getQueryData(
			getPolicyQueryKey({ path: { policy_id: paceDenyPolicy.id } }),
		),
	).toBeUndefined();
});

test("a customer policy stays clean when its customer refetches", async () => {
	const customerPolicy: PolicyResponse = {
		...paceDenyPolicy,
		level: "customer",
		customer_id: acmeCustomer.id,
	};
	let elapsedFraction = "0.5000";
	stubApi({
		...detailAnswers(jsonAnswer(customerPolicy)),
		[`GET /api/v1/dashboard/customers/${acmeCustomer.id}`]: () =>
			jsonAnswer({
				...acmeCustomer,
				signals: { ...acmeCustomer.signals, elapsed_fraction: elapsedFraction },
			})(),
	});
	const { queryClient } = renderApp(`/policies/${paceDenyPolicy.id}`);
	expect(
		await screen.findByRole("button", { name: "Acme Studio" }),
	).toBeVisible();

	elapsedFraction = "0.5100";
	await act(async () => {
		await queryClient.refetchQueries({
			queryKey: getDashboardCustomerQueryKey({
				path: { customer_id: acmeCustomer.id },
			}),
		});
		await new Promise((resolve) => setTimeout(resolve, 0));
	});

	expect(
		screen.queryByRole("button", { name: "Save" }),
	).not.toBeInTheDocument();
});

test("the recent decisions link to each decision and the filtered list", async () => {
	stubApi(detailAnswers(jsonAnswer(paceDenyPolicy)));
	renderApp(`/policies/${paceDenyPolicy.id}`);

	const table = await screen.findByRole("table", { name: "Recent decisions" });
	expect(
		await within(table).findByRole("link", { name: /hour ago/ }),
	).toHaveAttribute("href", `/decisions/${routedDecision.id}`);
	expect(within(table).getByText("Acme Studio")).toBeInTheDocument();
	expect(screen.getByRole("link", { name: "View all" })).toHaveAttribute(
		"href",
		`/decisions?policy_id=${paceDenyPolicy.id}`,
	);
});

test("an unknown policy reads Policy not found", async () => {
	stubApi(detailAnswers(problemAnswer(404, "not_found")));
	renderApp(`/policies/${paceDenyPolicy.id}`);

	expect(await screen.findByText("Policy not found")).toBeInTheDocument();
});
