import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import { expect, test, vi } from "vitest";
import { CodePanel } from "@/components/code-panel";

const planJson = '{\n  "id": "pln_01jbvagescfn78y0938nkrkayd"\n}';
const pythonCheck = 'decision = preburn.check(customer_id="customer-42")';
const curlCheck = "curl -X POST 'http://localhost:8480/api/v1/check'";

test("copies the code", async () => {
	const user = userEvent.setup();
	render(
		<>
			<CodePanel snippets={[{ label: "JSON", code: planJson }]} />
			<Toaster />
		</>,
	);

	await user.click(screen.getByRole("button", { name: "Copy JSON" }));

	expect(await navigator.clipboard.readText()).toBe(planJson);
	expect(await screen.findByText("Copied")).toBeInTheDocument();
});

test("shows tabs for several snippets and copies the open one", async () => {
	const user = userEvent.setup();
	render(
		<CodePanel
			snippets={[
				{ label: "Python", code: pythonCheck },
				{ label: "curl", code: curlCheck },
			]}
		/>,
	);

	await user.click(screen.getByRole("tab", { name: "curl" }));
	await user.click(screen.getByRole("button", { name: "Copy curl" }));

	expect(await navigator.clipboard.readText()).toBe(curlCheck);
});

test("a single snippet has no tabs", () => {
	render(<CodePanel snippets={[{ label: "JSON", code: planJson }]} />);

	expect(screen.queryByRole("tablist")).toBeNull();
});

test("selects the code when the clipboard refuses", async () => {
	const user = userEvent.setup();
	vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
		new DOMException("write refused", "NotAllowedError"),
	);
	render(
		<>
			<CodePanel snippets={[{ label: "JSON", code: planJson }]} />
			<Toaster />
		</>,
	);

	await user.click(screen.getByRole("button", { name: "Copy JSON" }));

	expect(
		await screen.findByText("Copy failed, text selected"),
	).toBeInTheDocument();
	expect(window.getSelection()?.toString()).toBe(planJson);
});
