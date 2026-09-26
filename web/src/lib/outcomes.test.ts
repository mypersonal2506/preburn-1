import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { decisionOutcomes, outcomeStyles } from "@/lib/outcomes";

const themeStylesheet = readFileSync(
	`${import.meta.dirname}/../styles/theme.css`,
	"utf8",
);

test("lists the outcomes in glossary order", () => {
	expect(decisionOutcomes).toEqual(["allow", "route", "cap", "deny"]);
});

test.each([
	["allow", "secondary", "var(--muted-foreground)", "Allowed", "Allow"],
	["route", "outline", "var(--revenue)", "Routed", "Route"],
	["cap", "default", "var(--foreground)", "Capped", "Cap"],
	["deny", "destructive", "var(--destructive)", "Denied", "Deny"],
] as const)(
	"%s uses the %s badge, the %s series and the labels %s and %s",
	(outcome, badgeVariant, seriesColor, pastTenseLabel, actionLabel) => {
		expect(outcomeStyles[outcome]).toEqual({
			badgeVariant,
			seriesColor,
			pastTenseLabel,
			actionLabel,
		});
	},
);

test.each(decisionOutcomes)(
	"the %s series color is a theme token",
	(outcome) => {
		const token = outcomeStyles[outcome].seriesColor.slice(
			"var(".length,
			-")".length,
		);

		expect(themeStylesheet).toContain(`\t${token}: `);
	},
);
