import { screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import {
	type ApiAnswers,
	jsonAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const TOKEN = crypto.randomUUID();

const pendingSetupAnswers: ApiAnswers = {
	...signedInAnswers,
	"GET /api/v1/setup/status": jsonAnswer({ setup_required: true }),
};

const inviteLinkAnswers: ApiAnswers = {
	...signedInAnswers,
	"POST /api/v1/auth/links/inspect": jsonAnswer({
		purpose: "invite",
		email: "jordan@example.com",
		display_name: "Jordan Lee",
	}),
};

afterEach(() => {
	vi.unstubAllGlobals();
});

test.each([
	{ path: "/login", heading: "Log in", answers: signedInAnswers },
	{ path: `/setup#${TOKEN}`, heading: "Set up", answers: pendingSetupAnswers },
	{
		path: `/link#${TOKEN}`,
		heading: "Accept invite",
		answers: inviteLinkAnswers,
	},
])(
	"the $heading screen shows the hidden mark before the wordmark",
	async ({ path, heading, answers }) => {
		stubApi(answers);
		renderApp(path);

		await screen.findByRole("heading", { name: heading });

		const mark = screen.getByText("Preburn").previousElementSibling;
		expect(mark).toHaveAttribute("data-slot", "preburn-mark");
		expect(mark).toHaveAttribute("aria-hidden", "true");
	},
);
