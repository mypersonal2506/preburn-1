import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { PageMenu } from "@/components/page-menu";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";

const plan = { id: "pln_01jbvagescfn78y0938nkrkayd", name: "Creator" };

const renamePlanCurl = [
	`curl -X PATCH '${window.location.origin}/api/v1/plans/pln_01jbvagescfn78y0938nkrkayd' \\`,
	'  -H "Authorization: Bearer $PREBURN_API_KEY" \\',
	"  -H 'Content-Type: application/json' \\",
	"  -d '{",
	`  "name": "Creator'\\''s plan"`,
	"}'",
].join("\n");

function renderPlanMenu() {
	render(
		<PageMenu
			record={plan}
			apiRequest={{
				method: "PATCH",
				path: `/api/v1/plans/${plan.id}`,
				body: { name: "Creator's plan" },
			}}
		/>,
	);
}

test("View JSON shows the record", async () => {
	const user = userEvent.setup();
	renderPlanMenu();

	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(await screen.findByRole("menuitem", { name: "View JSON" }));

	const dialog = await screen.findByRole("dialog", { name: "JSON" });
	expect(dialog).toHaveTextContent('"name": "Creator"');
});

test("Copy as API request copies a curl command with a placeholder key", async () => {
	const user = userEvent.setup();
	renderPlanMenu();

	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(
		await screen.findByRole("menuitem", { name: "Copy as API request" }),
	);

	expect(await navigator.clipboard.readText()).toBe(renamePlanCurl);
	expect(screen.queryByRole("dialog")).toBeNull();
});

test("a GET request has no body", async () => {
	const user = userEvent.setup();
	render(
		<PageMenu
			apiRequest={{ method: "GET", path: `/api/v1/plans/${plan.id}` }}
		/>,
	);

	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(
		await screen.findByRole("menuitem", { name: "Copy as API request" }),
	);

	expect(await navigator.clipboard.readText()).toBe(
		[
			`curl -X GET '${window.location.origin}/api/v1/plans/pln_01jbvagescfn78y0938nkrkayd' \\`,
			'  -H "Authorization: Bearer $PREBURN_API_KEY"',
		].join("\n"),
	);
});

test("shows the request when the clipboard refuses", async () => {
	const user = userEvent.setup();
	renderPlanMenu();
	vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
		new DOMException("write refused", "NotAllowedError"),
	);

	await user.click(screen.getByRole("button", { name: "More actions" }));
	await user.click(
		await screen.findByRole("menuitem", { name: "Copy as API request" }),
	);

	const dialog = await screen.findByRole("dialog", { name: "API request" });
	expect(dialog.querySelector("pre")?.textContent).toBe(renamePlanCurl);
});

test("lists record actions after the shared items", async () => {
	const user = userEvent.setup();
	const onArchive = vi.fn();
	render(
		<PageMenu record={plan}>
			<DropdownMenuItem onSelect={onArchive}>Archive</DropdownMenuItem>
		</PageMenu>,
	);

	await user.click(screen.getByRole("button", { name: "More actions" }));
	const items = await screen.findAllByRole("menuitem");
	expect(items.map((item) => item.textContent)).toEqual([
		"View JSON",
		"Archive",
	]);

	await user.click(screen.getByRole("menuitem", { name: "Archive" }));
	expect(onArchive).toHaveBeenCalledOnce();
});
