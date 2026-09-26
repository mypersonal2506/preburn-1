import { readFileSync } from "node:fs";
import { describe, expect, test } from "vitest";

const themeStylesheet = readFileSync(
	`${import.meta.dirname}/theme.css`,
	"utf8",
);

const inkTokens: [token: string, light: string, dark: string][] = [
	["background", "oklch(0.992 0.001 255)", "oklch(0.155 0.006 255)"],
	["foreground", "oklch(0.18 0.01 255)", "oklch(0.95 0.003 255)"],
	["card", "oklch(1 0 0)", "oklch(0.19 0.007 255)"],
	["popover", "oklch(1 0 0)", "oklch(0.21 0.008 255)"],
	["primary", "oklch(0.22 0.012 255)", "oklch(0.94 0.004 255)"],
	["primary-foreground", "oklch(0.985 0 0)", "oklch(0.18 0.01 255)"],
	["secondary", "oklch(0.955 0.004 255)", "oklch(0.25 0.008 255)"],
	["accent", "oklch(0.955 0.004 255)", "oklch(0.25 0.008 255)"],
	["secondary-foreground", "oklch(0.22 0.012 255)", "oklch(0.95 0.003 255)"],
	["accent-foreground", "oklch(0.22 0.012 255)", "oklch(0.95 0.003 255)"],
	["muted", "oklch(0.96 0.003 255)", "oklch(0.235 0.008 255)"],
	["muted-foreground", "oklch(0.5 0.012 255)", "oklch(0.68 0.012 255)"],
	["destructive", "oklch(0.56 0.2 25)", "oklch(0.68 0.18 25)"],
	["border", "oklch(0.905 0.004 255)", "oklch(0.28 0.008 255)"],
	["input", "oklch(0.88 0.005 255)", "oklch(0.3 0.008 255)"],
	["ring", "oklch(0.55 0.19 262)", "oklch(0.7 0.15 262)"],
	["sidebar", "oklch(0.985 0.002 255)", "oklch(0.17 0.006 255)"],
	["sidebar-foreground", "oklch(0.35 0.012 255)", "oklch(0.78 0.01 255)"],
	["sidebar-accent", "oklch(0.94 0.004 255)", "oklch(0.24 0.008 255)"],
	[
		"sidebar-accent-foreground",
		"oklch(0.18 0.01 255)",
		"oklch(0.96 0.003 255)",
	],
	["sidebar-border", "oklch(0.905 0.004 255)", "oklch(0.26 0.008 255)"],
	["success", "oklch(0.5 0.12 155)", "oklch(0.78 0.13 155)"],
	["warning", "oklch(0.55 0.13 70)", "oklch(0.83 0.12 80)"],
	["revenue", "oklch(0.55 0.19 262)", "oklch(0.7 0.15 262)"],
	["cost", "oklch(0.72 0.01 255)", "oklch(0.5 0.01 255)"],
];

function declarations(selector: string): Map<string, string> {
	const start = themeStylesheet.indexOf(`\n${selector} {\n`);
	if (start === -1) {
		throw new Error(`theme block missing selector=${selector}`);
	}
	const end = themeStylesheet.indexOf("\n}", start + 1);
	const block = themeStylesheet.slice(start, end);
	const found = new Map<string, string>();
	for (const match of block.matchAll(/^\t--([a-z-]+): (.+);$/gm)) {
		const [, name, value] = match;
		if (name === undefined || value === undefined) {
			throw new Error(`theme declaration unreadable line=${match[0]}`);
		}
		found.set(name, value);
	}
	return found;
}

describe.each([":root", ".dark"])("%s", (selector) => {
	const tokens = declarations(selector);

	test.each(inkTokens)("sets %s from the Ink table", (token, light, dark) => {
		expect(tokens.get(token)).toBe(selector === ":root" ? light : dark);
	});

	test("defines the foregrounds shadcn components read", () => {
		expect(tokens.get("card-foreground")).toBe("var(--foreground)");
		expect(tokens.get("popover-foreground")).toBe("var(--foreground)");
		expect(tokens.get("sidebar-ring")).toBe("var(--ring)");
	});
});

test("sets the radius", () => {
	expect(declarations(":root").get("radius")).toBe("0.375rem");
});

test.each(["success", "warning", "revenue", "cost"])(
	"maps %s into Tailwind colors",
	(token) => {
		expect(themeStylesheet).toContain(`\t--color-${token}: var(--${token});`);
	},
);

test("sets compact spacing and self-hosted Geist fonts", () => {
	expect(themeStylesheet).toContain("\t--spacing: 0.235rem;");
	expect(themeStylesheet).toContain('@import "@fontsource-variable/geist";');
	expect(themeStylesheet).toContain(
		'@import "@fontsource-variable/geist-mono";',
	);
});

test("keeps the shadcn and animation imports", () => {
	expect(themeStylesheet).toContain('@import "shadcn/tailwind.css";');
	expect(themeStylesheet).toContain('@import "tw-animate-css";');
});
