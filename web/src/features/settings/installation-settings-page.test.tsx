import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";
import { afterEach, expect, test, vi } from "vitest";
import type { SettingsResponse } from "@/client";
import {
	detailedProblemAnswer,
	sentBody,
	sentRoutes,
} from "@/features/auth/auth-test-support";
import {
	freePlan,
	liveSettings,
	planPage,
} from "@/features/settings/settings-test-support";
import {
	type Environment,
	getEnvironment,
	setEnvironment,
} from "@/lib/environment-store";
import {
	type ApiAnswers,
	installationSettings,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const SETTINGS_ROUTE = "PATCH /api/v1/settings";

afterEach(() => {
	toast.dismiss();
	setEnvironment("test");
	window.localStorage.clear();
	vi.unstubAllGlobals();
});

function installationAnswers(savedSettings: SettingsResponse): ApiAnswers {
	const storedSettings: Record<Environment, SettingsResponse> = {
		test: installationSettings,
		live: liveSettings,
	};
	return {
		...signedInAnswers,
		"GET /api/v1/settings": () =>
			jsonAnswer(storedSettings[getEnvironment()])(),
		"GET /api/v1/plans": jsonAnswer(planPage),
		[SETTINGS_ROUTE]: () => {
			storedSettings[getEnvironment()] = savedSettings;
			return jsonAnswer(savedSettings)();
		},
	};
}

async function findInstallationCard(): Promise<HTMLElement> {
	const heading = await screen.findByRole("heading", { name: "Installation" });
	const card = heading.closest<HTMLElement>('[data-slot="card"]');
	if (card === null) {
		throw new Error("installation card missing");
	}
	return card;
}

function sentRequest(requests: readonly Request[], route: string): Request {
	const request = requests.find(
		(sent) => `${sent.method} ${new URL(sent.url).pathname}` === route,
	);
	if (request === undefined) {
		throw new Error(`request missing route=${route}`);
	}
	return request;
}

test("the settings header selects the Installation tab", async () => {
	stubApi(installationAnswers(installationSettings));
	renderApp("/settings/installation");

	expect(
		await screen.findByRole("heading", { level: 1, name: "Settings" }),
	).toBeInTheDocument();
	expect(screen.getByRole("tab", { name: "Installation" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	expect(screen.getByRole("tab", { name: "Members" })).toHaveAttribute(
		"href",
		"/settings/members",
	);
	const card = await findInstallationCard();
	expect(await within(card).findByLabelText("Name")).toHaveValue(
		installationSettings.installation_name,
	);
});

test("/settings opens the Installation tab", async () => {
	stubApi(installationAnswers(installationSettings));
	const { router } = renderApp("/settings");

	expect(
		await screen.findByRole("tab", { name: "Installation" }),
	).toHaveAttribute("aria-selected", "true");
	expect(router.state.location.pathname).toBe("/settings/installation");
});

test("the default plan saves for the current environment only", async () => {
	const user = userEvent.setup();
	setEnvironment("live");
	const savedLiveSettings: SettingsResponse = {
		...liveSettings,
		default_plan_id: freePlan.id,
	};
	const requests = stubApi(installationAnswers(savedLiveSettings));
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	expect(
		await within(card).findByText("Applies to the live environment only."),
	).toBeInTheDocument();
	const planPicker = await within(card).findByLabelText("Default plan");
	expect(await within(planPicker).findByText("Creator")).toBeInTheDocument();

	await user.click(planPicker);
	await user.click(await screen.findByRole("option", { name: /Free/ }));
	await user.click(within(card).getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Settings saved")).toBeInTheDocument();
	expect(await sentBody(requests, SETTINGS_ROUTE)).toEqual({
		installation_name: installationSettings.installation_name,
		default_plan_id: freePlan.id,
	});
	expect(
		sentRequest(requests, SETTINGS_ROUTE).headers.get("X-Preburn-Environment"),
	).toBe("live");

	await user.click(screen.getByRole("radio", { name: "Test" }));

	const testCard = await findInstallationCard();
	expect(
		await within(testCard).findByText("Applies to the test environment only."),
	).toBeInTheDocument();
	expect(within(testCard).getByLabelText("Default plan")).toHaveTextContent(
		"Select a plan",
	);
});

test("clearing the default plan sends null", async () => {
	const user = userEvent.setup();
	setEnvironment("live");
	const requests = stubApi(
		installationAnswers({ ...liveSettings, default_plan_id: null }),
	);
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	await within(await within(card).findByLabelText("Default plan")).findByText(
		"Creator",
	);

	await user.click(
		within(card).getByRole("button", { name: "Clear default plan" }),
	);
	await user.click(within(card).getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Settings saved")).toBeInTheDocument();
	expect(await sentBody(requests, SETTINGS_ROUTE)).toEqual({
		installation_name: installationSettings.installation_name,
		default_plan_id: null,
	});
});

test("a new name saves and shows in the sidebar", async () => {
	const user = userEvent.setup();
	const renamedSettings: SettingsResponse = {
		...installationSettings,
		installation_name: "Acme Studio",
	};
	const requests = stubApi(installationAnswers(renamedSettings));
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	const nameInput = await within(card).findByLabelText("Name");

	await user.clear(nameInput);
	await user.type(nameInput, "  Acme Studio ");
	await user.click(within(card).getByRole("button", { name: "Save" }));

	expect(await screen.findByText("Settings saved")).toBeInTheDocument();
	expect(await sentBody(requests, SETTINGS_ROUTE)).toEqual({
		installation_name: "Acme Studio",
		default_plan_id: null,
	});
	const sidebar = document.querySelector<HTMLElement>('[data-slot="sidebar"]');
	if (sidebar === null) {
		throw new Error("sidebar missing");
	}
	expect(await within(sidebar).findByText("Acme Studio")).toBeInTheDocument();
});

test("a blank name shows the name rule and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(installationAnswers(installationSettings));
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	const nameInput = await within(card).findByLabelText("Name");

	await user.clear(nameInput);
	await user.type(nameInput, "   ");
	await user.click(within(card).getByRole("button", { name: "Save" }));

	expect(
		await within(card).findByText("Use 1 to 80 characters"),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain(SETTINGS_ROUTE);
});

test("a plan that is no longer active shows plan_not_found in the form", async () => {
	const user = userEvent.setup();
	stubApi({
		...installationAnswers(installationSettings),
		[SETTINGS_ROUTE]: detailedProblemAnswer(422, "plan_not_found", {}),
	});
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	const planPicker = await within(card).findByLabelText("Default plan");

	await user.click(planPicker);
	await user.click(await screen.findByRole("option", { name: /Free/ }));
	await user.click(within(card).getByRole("button", { name: "Save" }));

	expect(
		await within(card).findByText(
			"The plan is archived or missing. Pick another.",
		),
	).toBeInTheDocument();
});

test("a failed load offers Try again", async () => {
	const user = userEvent.setup();
	const answers = installationAnswers(installationSettings);
	let settingsFail = true;
	const requests = stubApi({
		...answers,
		"GET /api/v1/settings": () =>
			settingsFail
				? problemAnswer(422, "environment_header_invalid")()
				: jsonAnswer(installationSettings)(),
	});
	renderApp("/settings/installation");
	const card = await findInstallationCard();
	const retry = await within(card).findByRole("button", { name: "Try again" });
	settingsFail = false;

	await user.click(retry);

	expect(await within(card).findByLabelText("Name")).toHaveValue(
		installationSettings.installation_name,
	);
	expect(
		sentRoutes(requests).filter((route) => route === "GET /api/v1/settings")
			.length,
	).toBeGreaterThan(1);
});
