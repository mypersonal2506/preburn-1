import { expect, test } from "vitest";
import {
	type EnforcementSettings,
	enforcementSummary,
} from "@/features/policies/policy-phrases";

test.each<[EnforcementSettings, string]>([
	[
		{ enforcement: "soft", on_unreachable: "allow", on_uncosted: "allow" },
		"Soft, allow if unreachable or unpriced",
	],
	[
		{ enforcement: "hard", on_unreachable: "deny", on_uncosted: "deny" },
		"Hard, deny if unreachable or unpriced",
	],
	[
		{ enforcement: "hard", on_unreachable: "allow", on_uncosted: "deny" },
		"Hard, allow if unreachable, deny if unpriced",
	],
])("enforcementSummary reads %o as %s", (settings, summary) => {
	expect(enforcementSummary(settings)).toBe(summary);
});
