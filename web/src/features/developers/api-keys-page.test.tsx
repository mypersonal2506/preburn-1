import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";
import type { ApiKeyResponse, CreatedApiKeyResponse } from "@/client";
import { sentBody, sentRoutes } from "@/features/auth/auth-test-support";
import {
	API_KEY_LIST_ROUTE,
	adminKey,
	apiKeyPage,
	CREATE_API_KEY_ROUTE,
	ONBOARDING_ROUTE,
	onboardingAllDone,
	runtimeKey,
} from "@/features/developers/developers-test-support";
import {
	type ApiAnswers,
	holdAnswer,
	jsonAnswer,
	problemAnswer,
	renderApp,
	signedInAnswers,
	stubApi,
} from "@/routes/-render-app";

const REVOKE_RUNTIME_KEY_ROUTE = `DELETE /api/v1/api-keys/${runtimeKey.id}`;

afterEach(() => {
	vi.unstubAllGlobals();
});

function keyListAnswers(listAnswer: () => Response): ApiAnswers {
	return {
		...signedInAnswers,
		[ONBOARDING_ROUTE]: jsonAnswer(onboardingAllDone),
		[API_KEY_LIST_ROUTE]: listAnswer,
	};
}

function listedApiKey(createdKey: CreatedApiKeyResponse): ApiKeyResponse {
	return {
		id: createdKey.id,
		name: createdKey.name,
		scope: createdKey.scope,
		secret_last_four: createdKey.secret_last_four,
		status: createdKey.status,
		last_used_at: createdKey.last_used_at,
		created_at: createdKey.created_at,
	};
}

async function findKeyRow(name: string): Promise<HTMLElement> {
	const table = await screen.findByRole("table", { name: "API keys" });
	const cell = await within(table).findByText(name);
	const row = cell.closest("tr");
	if (row === null) {
		throw new Error(`key row missing name=${name}`);
	}
	return row;
}

test("lists each key with its masked key, scope, last use, creation and status", async () => {
	stubApi(keyListAnswers(jsonAnswer(apiKeyPage([runtimeKey, adminKey]))));
	renderApp("/developers/api-keys");

	const runtimeRow = await findKeyRow("Production app");

	const table = screen.getByRole("table", { name: "API keys" });
	expect(
		within(table)
			.getAllByRole("columnheader")
			.map((header) => header.textContent),
	).toEqual([
		"Name",
		"Key",
		"Scope",
		"Last used",
		"Created",
		"Status",
		"Actions",
	]);
	expect(within(runtimeRow).getByText("••••7Qx2")).toBeInTheDocument();
	expect(within(runtimeRow).getByText("Runtime")).toBeInTheDocument();
	expect(within(runtimeRow).getByText("Never")).toBeInTheDocument();
	expect(within(runtimeRow).getByText("2 days ago")).toBeInTheDocument();
	expect(within(runtimeRow).getByText("Active")).toBeInTheDocument();
	const adminRow = await findKeyRow("CI deploys");
	expect(within(adminRow).getByText("Admin")).toBeInTheDocument();
	expect(within(adminRow).getByText("1 hour ago")).toBeInTheDocument();
});

test("the secret modal cannot close before I stored the key and never shows the secret again", async () => {
	const user = userEvent.setup();
	const secret = `secret-${crypto.randomUUID()}`;
	const createdKey: CreatedApiKeyResponse = {
		...adminKey,
		id: "key_01jbvagescfn78y0938nkrka03",
		name: "Nightly export",
		secret,
		secret_last_four: secret.slice(-4),
		last_used_at: null,
		created_at: new Date().toISOString(),
	};
	let listedKeys: ApiKeyResponse[] = [runtimeKey];
	const requests = stubApi({
		...keyListAnswers(() => jsonAnswer(apiKeyPage(listedKeys))()),
		[CREATE_API_KEY_ROUTE]: () => {
			listedKeys = [listedApiKey(createdKey), runtimeKey];
			return new Response(JSON.stringify(createdKey), {
				status: 201,
				headers: { "Content-Type": "application/json" },
			});
		},
	});
	const { queryClient } = renderApp("/developers/api-keys");
	await findKeyRow("Production app");

	await user.click(screen.getByRole("button", { name: "Create API key" }));
	const createDialog = await screen.findByRole("dialog", {
		name: "Create API key",
	});
	await user.type(
		within(createDialog).getByRole("textbox", { name: "Name" }),
		"Nightly export",
	);
	await user.click(within(createDialog).getByRole("radio", { name: /Admin/ }));
	await user.click(
		within(createDialog).getByRole("button", { name: "Create" }),
	);

	const secretDialog = await screen.findByRole("dialog", {
		name: "Store your API key",
	});
	expect(await sentBody(requests, CREATE_API_KEY_ROUTE)).toEqual({
		name: "Nightly export",
		scope: "admin",
	});
	expect(within(secretDialog).getByText(secret)).toBeInTheDocument();
	expect(
		within(secretDialog).getByRole("button", { name: "Copy API key" }),
	).toBeInTheDocument();
	const doneButton = within(secretDialog).getByRole("button", { name: "Done" });
	expect(doneButton).toBeDisabled();
	expect(
		within(secretDialog).queryByRole("button", { name: "Close" }),
	).not.toBeInTheDocument();

	await user.keyboard("{Escape}");

	expect(
		screen.getByRole("dialog", { name: "Store your API key" }),
	).toBeVisible();

	await user.click(
		within(secretDialog).getByRole("checkbox", { name: "I stored the key" }),
	);
	await user.click(doneButton);

	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(screen.queryByText(secret)).not.toBeInTheDocument();
	await waitFor(() => {
		expect(queryClient.getMutationCache().getAll()).toEqual([]);
	});
	const createdRow = await findKeyRow("Nightly export");
	expect(
		within(createdRow).getByText(`••••${secret.slice(-4)}`),
	).toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "Create API key" }));
	const reopenedDialog = await screen.findByRole("dialog", {
		name: "Create API key",
	});
	expect(
		within(reopenedDialog).getByRole("textbox", { name: "Name" }),
	).toHaveValue("");
	expect(screen.queryByText(secret)).not.toBeInTheDocument();
});

test("a key created after the modal closed still shows its secret and joins the list", async () => {
	const user = userEvent.setup();
	const secret = `secret-${crypto.randomUUID()}`;
	const createdKey: CreatedApiKeyResponse = {
		...runtimeKey,
		id: "key_01jbvagescfn78y0938nkrka04",
		name: "Worker",
		secret,
		secret_last_four: secret.slice(-4),
		created_at: new Date().toISOString(),
	};
	let listedKeys: ApiKeyResponse[] = [runtimeKey];
	const heldCreate = holdAnswer();
	stubApi({
		...keyListAnswers(() => jsonAnswer(apiKeyPage(listedKeys))()),
		[CREATE_API_KEY_ROUTE]: heldCreate.answer,
	});
	renderApp("/developers/api-keys");
	await findKeyRow("Production app");

	await user.click(screen.getByRole("button", { name: "Create API key" }));
	const createDialog = await screen.findByRole("dialog", {
		name: "Create API key",
	});
	await user.type(
		within(createDialog).getByRole("textbox", { name: "Name" }),
		"Worker",
	);
	await user.click(
		within(createDialog).getByRole("button", { name: "Create" }),
	);
	await user.click(
		within(createDialog).getByRole("button", { name: "Cancel" }),
	);
	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});

	listedKeys = [listedApiKey(createdKey), runtimeKey];
	heldCreate.release(
		() =>
			new Response(JSON.stringify(createdKey), {
				status: 201,
				headers: { "Content-Type": "application/json" },
			}),
	);

	const secretDialog = await screen.findByRole("dialog", {
		name: "Store your API key",
	});
	expect(within(secretDialog).getByText(secret)).toBeInTheDocument();
	await user.click(
		within(secretDialog).getByRole("checkbox", { name: "I stored the key" }),
	);
	await user.click(within(secretDialog).getByRole("button", { name: "Done" }));
	expect(await findKeyRow("Worker")).toBeInTheDocument();
});

test("an empty name shows its rule and sends nothing", async () => {
	const user = userEvent.setup();
	const requests = stubApi(
		keyListAnswers(jsonAnswer(apiKeyPage([runtimeKey]))),
	);
	renderApp("/developers/api-keys");
	await findKeyRow("Production app");

	await user.click(screen.getByRole("button", { name: "Create API key" }));
	const createDialog = await screen.findByRole("dialog", {
		name: "Create API key",
	});
	await user.click(
		within(createDialog).getByRole("button", { name: "Create" }),
	);

	expect(
		await within(createDialog).findByText(
			"Enter a name of 1 to 80 characters.",
		),
	).toBeInTheDocument();
	expect(sentRoutes(requests)).not.toContain(CREATE_API_KEY_ROUTE);
});

test("revoke asks for confirmation and updates the row", async () => {
	const user = userEvent.setup();
	let revoked = false;
	const requests = stubApi({
		...keyListAnswers(() =>
			jsonAnswer(
				apiKeyPage([
					revoked ? { ...runtimeKey, status: "disabled" } : runtimeKey,
					adminKey,
				]),
			)(),
		),
		[REVOKE_RUNTIME_KEY_ROUTE]: () => {
			revoked = true;
			return new Response(null, { status: 204 });
		},
	});
	renderApp("/developers/api-keys");
	const runtimeRow = await findKeyRow("Production app");

	await user.click(
		within(runtimeRow).getByRole("button", { name: "Row actions" }),
	);
	await user.click(await screen.findByRole("menuitem", { name: "Revoke" }));

	const confirmDialog = await screen.findByRole("dialog", {
		name: "Revoke Production app?",
	});
	expect(sentRoutes(requests)).not.toContain(REVOKE_RUNTIME_KEY_ROUTE);

	await user.click(
		within(confirmDialog).getByRole("button", { name: "Revoke key" }),
	);

	await waitFor(() => {
		expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
	});
	expect(sentRoutes(requests)).toContain(REVOKE_RUNTIME_KEY_ROUTE);
	const revokedRow = await findKeyRow("Production app");
	expect(await within(revokedRow).findByText("Disabled")).toBeInTheDocument();
	const adminRow = await findKeyRow("CI deploys");
	expect(within(adminRow).getByText("Active")).toBeInTheDocument();
});

test("a disabled key cannot be revoked again", async () => {
	const user = userEvent.setup();
	stubApi(
		keyListAnswers(
			jsonAnswer(apiKeyPage([{ ...runtimeKey, status: "disabled" }])),
		),
	);
	renderApp("/developers/api-keys");
	const disabledRow = await findKeyRow("Production app");

	await user.click(
		within(disabledRow).getByRole("button", { name: "Row actions" }),
	);

	expect(
		await screen.findByRole("menuitem", { name: "Revoke" }),
	).toHaveAttribute("aria-disabled", "true");
});

test("without keys the list offers to create one", async () => {
	stubApi(keyListAnswers(jsonAnswer(apiKeyPage([]))));
	renderApp("/developers/api-keys");

	const table = await screen.findByRole("table", { name: "API keys" });

	expect(await within(table).findByText("No API keys yet")).toBeInTheDocument();
	expect(
		within(table).getByRole("button", { name: "Create API key" }),
	).toBeInTheDocument();
});

test("a failed list shows the error with Try again", async () => {
	stubApi(keyListAnswers(problemAnswer(403, "scope_forbidden")));
	renderApp("/developers/api-keys");

	const table = await screen.findByRole("table", { name: "API keys" });

	expect(
		await within(table).findByText("Something went wrong"),
	).toBeInTheDocument();
	expect(
		within(table).getByRole("button", { name: "Try again" }),
	).toBeInTheDocument();
});
