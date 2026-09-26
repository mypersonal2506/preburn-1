import * as z from "zod";
import { zGetPolicyPath } from "@/client/zod.gen";

/**
 * The policy list's status filter as a URL search param: active, disabled,
 * archived, or all, opening on active.
 */
export const policyListSearchSchema = z.object({
	status: z.enum(["active", "disabled", "archived", "all"]).default("active"),
});

/** The policy list's status filter as its route reads it. */
export type PolicyListSearch = z.output<typeof policyListSearchSchema>;

/** The search params of the list opening on active, left out of its URL. */
export const policyListSearchDefaults = policyListSearchSchema.parse({});

/**
 * The new policy page's search params: `duplicate`, the id of a policy whose
 * document the page starts from.
 */
export const newPolicySearchSchema = z.object({
	duplicate: zGetPolicyPath.shape.policy_id.optional(),
});
