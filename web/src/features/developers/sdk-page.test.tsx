import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import { renderApp, signedInAnswers, stubApi } from "@/routes/-render-app";

const INSTALL_LINE =
	'pip install "preburn @ git+https://github.com/preburn/sdk-python@v0.1.0"';
const OPENAI_INSTALL_LINE =
	'pip install "preburn[openai] @ git+https://github.com/preburn/sdk-python@v0.1.0"';

afterEach(() => {
	vi.unstubAllGlobals();
});

function findSection(title: string): HTMLElement {
	const heading = screen.getByRole("heading", { name: title });
	const section = heading.closest<HTMLElement>('[data-slot="card"]');
	if (section === null) {
		throw new Error(`section card missing title=${title}`);
	}
	return section;
}

test("the Python tab installs from the git tag and shows check, report and the OpenAI wrapper", async () => {
	stubApi(signedInAnswers);
	renderApp("/developers/sdk");

	expect(await screen.findByRole("tab", { name: "Python" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	expect(within(findSection("Install")).getByText(INSTALL_LINE)).toBeVisible();
	const checkAndReport = within(findSection("Check and report")).getByText(
		/from preburn import Preburn/,
	);
	expect(checkAndReport).toHaveTextContent('api_key="YOUR_API_KEY"');
	expect(checkAndReport).toHaveTextContent(
		`base_url="${window.location.origin}"`,
	);
	expect(checkAndReport).toHaveTextContent("decision = preburn.check(");
	expect(checkAndReport).toHaveTextContent("decision.raise_for_denial()");
	expect(checkAndReport).toHaveTextContent("preburn.release(decision)");
	expect(checkAndReport).toHaveTextContent("preburn.report(decision, usage)");
	const wrapper = findSection("OpenAI wrapper");
	expect(within(wrapper).getByText(OPENAI_INSTALL_LINE)).toBeVisible();
	expect(within(wrapper).getByText(/wrap_openai/)).toHaveTextContent(
		"client = wrap_openai(OpenAI(), preburn)",
	);
	expect(within(wrapper).getByText(/wrap_openai/)).toHaveTextContent(
		"preburn=CallContext(",
	);
});

test("the curl tab lives in the URL and shows check and report", async () => {
	const user = userEvent.setup();
	stubApi(signedInAnswers);
	const { router } = renderApp("/developers/sdk");

	await user.click(await screen.findByRole("tab", { name: "curl" }));

	expect(router.state.location.search).toEqual({ language: "curl" });
	const check = within(findSection("Check")).getByText(/curl -X POST/);
	expect(check).toHaveTextContent(`${window.location.origin}/api/v1/check`);
	expect(check).toHaveTextContent("Authorization: Bearer YOUR_API_KEY");
	expect(check).toHaveTextContent('"usage_estimate": {');
	const report = within(findSection("Report")).getByText(/curl -X POST/);
	expect(report).toHaveTextContent(`${window.location.origin}/api/v1/report`);
	expect(report).toHaveTextContent('"decision_source": "server"');
	expect(
		screen.queryByRole("heading", { name: "OpenAI wrapper" }),
	).not.toBeInTheDocument();

	await user.click(screen.getByRole("tab", { name: "Python" }));

	expect(router.state.location.search).toEqual({});
});

test("opens on the curl tab from the URL", async () => {
	stubApi(signedInAnswers);
	renderApp("/developers/sdk?language=curl");

	expect(await screen.findByRole("tab", { name: "curl" })).toHaveAttribute(
		"aria-selected",
		"true",
	);
	expect(screen.getByRole("heading", { name: "Report" })).toBeInTheDocument();
});
