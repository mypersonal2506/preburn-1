import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeAll, expect, test, vi } from "vitest";
import {
	type ApiAnswers,
	jsonAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const GALLERY_SECTIONS = [
	"Headers and metrics",
	"Status and labels",
	"Details",
	"Tables",
	"Inputs",
	"Pickers",
	"Sentence",
	"Live data",
];

const GALLERY_RENDER_TIMEOUT_MILLISECONDS = 5000;

const emptyPage = jsonAnswer({ items: [], next_cursor: null });

const galleryAnswers: ApiAnswers = {
	...signedInAnswers,
	"GET /api/v1/pricing/models": emptyPage,
	"GET /api/v1/pricing/meters": emptyPage,
	"GET /api/v1/plans": emptyPage,
	"GET /api/v1/dashboard/customers": emptyPage,
	"GET /api/v1/dashboard/features": emptyPage,
	"GET /api/v1/policies/parameter-mappings": jsonAnswer({ models: [] }),
};

beforeAll(async () => {
	await import("@/features/gallery/component-gallery");
});

afterEach(() => {
	vi.unstubAllGlobals();
	vi.unstubAllEnvs();
});

test("the gallery shows every group of shared components in development", async () => {
	stubApi(galleryAnswers);
	renderApp("/gallery");

	expect(
		await screen.findByRole(
			"heading",
			{ name: "Gallery", level: 1 },
			{ timeout: GALLERY_RENDER_TIMEOUT_MILLISECONDS },
		),
	).toBeInTheDocument();
	for (const section of GALLERY_SECTIONS) {
		expect(
			screen.getByRole("heading", { name: section, level: 2 }),
		).toBeInTheDocument();
	}
});

test("the gallery's sample table moves through its pages", async () => {
	const user = userEvent.setup();
	stubApi(galleryAnswers);
	renderApp("/gallery");

	const tablesHeading = await screen.findByRole(
		"heading",
		{ name: "Tables", level: 2 },
		{ timeout: GALLERY_RENDER_TIMEOUT_MILLISECONDS },
	);
	const tablesCard = tablesHeading.closest<HTMLElement>('[data-slot="card"]');
	if (tablesCard === null) {
		throw new Error("tables card missing");
	}
	const samples = within(tablesCard).getByRole("table", {
		name: "Sample customers",
	});
	expect(within(samples).getByText("Acme")).toBeInTheDocument();

	await user.click(within(tablesCard).getByRole("button", { name: "Next" }));

	expect(within(samples).getByText("Lumber Co")).toBeInTheDocument();
});

test("production builds answer not found for the gallery", async () => {
	vi.stubEnv("PROD", true);
	vi.stubEnv("DEV", false);
	stubApi(signedInAnswers);
	renderApp("/gallery");

	expect(
		await screen.findByRole("heading", { name: "Page not found" }),
	).toBeInTheDocument();
	expect(
		screen.queryByRole("heading", { name: "Gallery", level: 1 }),
	).not.toBeInTheDocument();
});
