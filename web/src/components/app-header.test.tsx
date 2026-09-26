import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { acmeDetail } from "@/features/customers/customers-test-support";
import { routedDecisionDetail } from "@/features/decisions/decisions-test-support";
import { creatorPlan } from "@/features/plans/plans-test-support";
import { paceDenyPolicy } from "@/features/policies/policies-test-support";
import { setEnvironment } from "@/lib/environment-store";
import {
	jsonAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

test("a detail page shows the breadcrumb and its document title", async () => {
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	const { router } = renderApp("/policies/pol_01jbvagescfn78y0938nkrkayd");

	const breadcrumb = await screen.findByRole("navigation", {
		name: "breadcrumb",
	});
	expect(within(breadcrumb).getByText("Policy")).toHaveAttribute(
		"aria-current",
		"page",
	);
	await waitFor(() => {
		expect(document.title).toBe("Policy - Preburn");
	});

	await user.click(within(breadcrumb).getByRole("link", { name: "Policies" }));

	expect(
		await screen.findByRole("heading", { name: "Policies" }),
	).toBeInTheDocument();
	expect(router.state.location.pathname).toBe("/policies");
	await waitFor(() => {
		expect(document.title).toBe("Policies - Preburn");
	});
});

test.each([
	[
		"policy",
		`/policies/${paceDenyPolicy.id}`,
		`GET /api/v1/policies/${paceDenyPolicy.id}`,
		paceDenyPolicy,
		"Policies",
		"Heavy users",
	],
	[
		"plan",
		`/plans/${creatorPlan.id}`,
		`GET /api/v1/plans/${creatorPlan.id}`,
		creatorPlan,
		"Plans",
		"Creator",
	],
	[
		"customer",
		`/customers/${acmeDetail.id}`,
		`GET /api/v1/dashboard/customers/${acmeDetail.id}`,
		acmeDetail,
		"Customers",
		"Acme Studio",
	],
	[
		"decision",
		`/decisions/${routedDecisionDetail.id}`,
		`GET /api/v1/dashboard/decisions/${routedDecisionDetail.id}`,
		routedDecisionDetail,
		"Decisions",
		"Routed: text to video",
	],
])(
	"a %s detail page names its record in the breadcrumb",
	async (_kind, path, route, record, section, name) => {
		stubApi({ ...signedInAnswers, [route]: jsonAnswer(record) });
		renderApp(path);

		const breadcrumb = await screen.findByRole("navigation", {
			name: "breadcrumb",
		});

		expect(await within(breadcrumb).findByText(name)).toHaveAttribute(
			"aria-current",
			"page",
		);
		expect(
			within(breadcrumb).getByRole("link", { name: section }),
		).toBeVisible();
	},
);

test.each([
	["/", "Overview"],
	["/customers", "Customers"],
	["/decisions", "Decisions"],
	["/policies", "Policies"],
	["/plans", "Plans"],
	["/account", "Account"],
])("the list page %s shows only its title", async (path, title) => {
	stubApi(signedInAnswers);
	renderApp(path);

	expect(
		await screen.findByRole("heading", { name: title }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("navigation", { name: "breadcrumb" }),
	).not.toBeInTheDocument();
});

test("the Live data badge shows in live only", async () => {
	stubApi(signedInAnswers);
	renderApp("/");
	await screen.findByRole("heading", { name: "Overview" });

	expect(screen.queryByText("Live data")).not.toBeInTheDocument();

	act(() => {
		setEnvironment("live");
	});

	expect(await screen.findByText("Live data")).toBeInTheDocument();
});
