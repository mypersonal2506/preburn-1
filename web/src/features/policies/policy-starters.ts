import {
	CAP_SETTINGS_BLANK,
	routeTargetBlank,
} from "@/features/policies/policy-blanks";
import {
	type PolicyDraft,
	unpickedRouteTarget,
} from "@/features/policies/policy-draft";

/** A starter chip of the new policy page. */
export type PolicyStarter =
	| "stop_at_allowance"
	| "cheaper_model"
	| "cap_on_pace"
	| "stop_losses"
	| "blank";

/** A starter chip and its label. */
export interface PolicyStarterChip {
	starter: PolicyStarter;
	label: string;
}

/**
 * The draft a starter fills and the blank it opens for the member to finish,
 * null when it opens none.
 */
export interface StarterDraft {
	draft: PolicyDraft;
	openedBlank: string | null;
}

/** The starter chips in the order the new policy page shows them. */
export const POLICY_STARTERS: readonly PolicyStarterChip[] = [
	{ starter: "stop_at_allowance", label: "Stop at allowance" },
	{ starter: "cheaper_model", label: "Cheaper model" },
	{ starter: "cap_on_pace", label: "Cap on pace" },
	{ starter: "stop_losses", label: "Stop losses" },
	{ starter: "blank", label: "Blank" },
];

/**
 * The draft the Blank chip fills: always allow the requests of every
 * customer for any feature, soft, allowing when unreachable or unpriced.
 */
export const BLANK_POLICY_DRAFT: PolicyDraft = {
	name: null,
	level: "everyone",
	plan_id: null,
	customer: null,
	feature: null,
	when: { all: [] },
	action: { outcome: "allow", route_chain: [], overrides: {}, limit: null },
	enforcement: "soft",
	on_unreachable: "allow",
	on_uncosted: "allow",
	status: "active",
};

const NO_ALLOWANCE_LEFT = "0.000000000";
const CHEAPER_MODEL_PACE = "1.5000";
const CAP_PACE = "2.0000";
const LOSS_MARGIN = "0.0000";

/**
 * The draft a starter chip fills. Stop at allowance denies every request
 * once no allowance is left, enforced hard. Cheaper model routes the
 * customers of defaultPlanId above 1.5x pace and opens the model blank,
 * leaving the plan blank empty without a default plan. Cap on pace caps
 * above 2.0x pace and opens the settings blank. Stop losses denies while
 * the projected margin is below 0%. Blank always allows.
 */
export function starterDraft(
	starter: PolicyStarter,
	defaultPlanId: string | null,
): StarterDraft {
	switch (starter) {
		case "stop_at_allowance":
			return {
				draft: {
					...BLANK_POLICY_DRAFT,
					when: {
						all: [
							{
								signal: "allowance_remaining",
								operator: "lte",
								value: NO_ALLOWANCE_LEFT,
							},
						],
					},
					action: { ...BLANK_POLICY_DRAFT.action, outcome: "deny" },
					enforcement: "hard",
				},
				openedBlank: null,
			};
		case "cheaper_model":
			return {
				draft: {
					...BLANK_POLICY_DRAFT,
					level: "plan",
					plan_id: defaultPlanId,
					when: {
						all: [
							{ signal: "pace", operator: "gt", value: CHEAPER_MODEL_PACE },
						],
					},
					action: {
						...BLANK_POLICY_DRAFT.action,
						outcome: "route",
						route_chain: [unpickedRouteTarget()],
					},
				},
				openedBlank: routeTargetBlank(0),
			};
		case "cap_on_pace":
			return {
				draft: {
					...BLANK_POLICY_DRAFT,
					when: {
						all: [{ signal: "pace", operator: "gt", value: CAP_PACE }],
					},
					action: { ...BLANK_POLICY_DRAFT.action, outcome: "cap" },
				},
				openedBlank: CAP_SETTINGS_BLANK,
			};
		case "stop_losses":
			return {
				draft: {
					...BLANK_POLICY_DRAFT,
					when: {
						all: [
							{
								signal: "projected_margin",
								operator: "lt",
								value: LOSS_MARGIN,
							},
						],
					},
					action: { ...BLANK_POLICY_DRAFT.action, outcome: "deny" },
				},
				openedBlank: null,
			};
		case "blank":
			return { draft: BLANK_POLICY_DRAFT, openedBlank: null };
	}
}
