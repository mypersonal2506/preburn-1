import type { ComponentProps } from "react";
import type { DecisionResponse } from "@/client";
import type { Badge } from "@/components/ui/badge";

/** A decision outcome: allow, route, cap or deny. */
export type DecisionOutcome = DecisionResponse["outcome"];

/**
 * How the dashboard shows one outcome: the Badge variant, the CSS color of
 * its chart series, the past-tense label of a decision that had it, and the
 * action label of a policy that decides it.
 */
export type OutcomeStyle = {
	badgeVariant: NonNullable<ComponentProps<typeof Badge>["variant"]>;
	seriesColor: string;
	pastTenseLabel: string;
	actionLabel: string;
};

/** Every decision outcome in glossary order. */
export const decisionOutcomes: readonly DecisionOutcome[] = [
	"allow",
	"route",
	"cap",
	"deny",
];

/**
 * The one outcome mapping of the dashboard. Allow is secondary and muted,
 * route is outlined in the revenue hue, cap is ink, deny is destructive.
 */
export const outcomeStyles: Record<DecisionOutcome, OutcomeStyle> = {
	allow: {
		badgeVariant: "secondary",
		seriesColor: "var(--muted-foreground)",
		pastTenseLabel: "Allowed",
		actionLabel: "Allow",
	},
	route: {
		badgeVariant: "outline",
		seriesColor: "var(--revenue)",
		pastTenseLabel: "Routed",
		actionLabel: "Route",
	},
	cap: {
		badgeVariant: "default",
		seriesColor: "var(--foreground)",
		pastTenseLabel: "Capped",
		actionLabel: "Cap",
	},
	deny: {
		badgeVariant: "destructive",
		seriesColor: "var(--destructive)",
		pastTenseLabel: "Denied",
		actionLabel: "Deny",
	},
};
