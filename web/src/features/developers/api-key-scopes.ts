import type { ApiKeyResponse } from "@/client";
import { zCreateApiKeyRequest } from "@/client/zod.gen";

/** The scope of an API key, runtime or admin. */
export type ApiKeyScope = ApiKeyResponse["scope"];

interface ApiKeyScopeDisplay {
	label: string;
	description: string;
}

/** Parses a scope from a form control. Its options list the API's scopes. */
export const apiKeyScopeSchema = zCreateApiKeyRequest.shape.scope;

/** The label of each scope and what a key of that scope can call. */
export const API_KEY_SCOPE_DISPLAYS: Record<ApiKeyScope, ApiKeyScopeDisplay> = {
	runtime: {
		label: "Runtime",
		description: "Check, report, release, customers, revenue",
	},
	admin: { label: "Admin", description: "Everything, for scripts and CI" },
};
