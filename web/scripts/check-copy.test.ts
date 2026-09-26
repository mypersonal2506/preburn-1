import { expect, test } from "vitest";
import {
	type CopyAllowlistEntry,
	collectCopyStrings,
	findCopyProblems,
	isCopyCheckedFile,
} from "./check-copy.ts";

const FILE = "src/features/plans/plan-page.tsx";
const LONG_HINT =
	"Plans set the margin target that every policy measures against";

function problemsIn(
	source: string,
	allowlist: readonly CopyAllowlistEntry[] = [],
): string[] {
	return findCopyProblems(collectCopyStrings(FILE, source), allowlist);
}

test("a string literal over 60 characters fails", () => {
	expect(problemsIn(`const hint = "${LONG_HINT}";\n`)).toEqual([
		`copy too long file=${FILE} line=1 length=62 text="${LONG_HINT}"`,
	]);
});

test("a string of exactly 60 characters passes", () => {
	const sixtyCharacters = "x".repeat(60);

	expect(problemsIn(`const hint = "${sixtyCharacters}";\n`)).toEqual([]);
});

test("JSX text counts with its whitespace collapsed", () => {
	const source = [
		"export function Hint() {",
		"\treturn (",
		"\t\t<p>",
		"\t\t\tPlans set the margin target that every",
		"\t\t\tpolicy measures against",
		"\t\t</p>",
		"\t);",
		"}",
		"",
	].join("\n");

	expect(problemsIn(source)).toEqual([
		`copy too long file=${FILE} line=4 length=62 text="${LONG_HINT}"`,
	]);
});

test("short JSX text and attribute strings pass", () => {
	expect(
		problemsIn('export const hint = <p title="Plans">Set a target</p>;\n'),
	).toEqual([]);
});

test("className values and cn() arguments are not copy", () => {
	const classList =
		"flex flex-col gap-0.5 px-2 pt-1 group-data-[collapsible=icon]:hidden text-sm";
	const source = [
		"export function Header({ active }: { active: boolean }) {",
		"\treturn (",
		`\t\t<div className="${classList}">`,
		`\t\t\t<span className={cn("${classList}", active && "${classList}")} />`,
		`\t\t\t<span className={\`${classList} \${active}\`} />`,
		"\t\t</div>",
		"\t);",
		"}",
		`const toneClasses = cn("${classList}");`,
		"",
	].join("\n");

	expect(problemsIn(source)).toEqual([]);
});

test("copy next to a className still counts", () => {
	const source = `export const hint = <p className="text-sm" title="${LONG_HINT}">${LONG_HINT}</p>;\n`;

	expect(problemsIn(source)).toEqual([
		`copy too long file=${FILE} line=1 length=62 text="${LONG_HINT}"`,
		`copy too long file=${FILE} line=1 length=62 text="${LONG_HINT}"`,
	]);
});

test("a template literal counts its static text only", () => {
	const source = `const hint = \`\${plan} sets the margin target that every one of its policies measures against\`;\n`;

	expect(problemsIn(source)).toEqual([
		`copy too long file=${FILE} line=1 length=71 text=" sets the margin target that every one of its policies measures against"`,
	]);
});

test("a code template of long expressions passes", () => {
	const source = `const command = \`curl -X \${request.method} '\${window.location.origin}\${request.path}'\`;\n`;

	expect(problemsIn(source)).toEqual([]);
});

test("an allowlisted string passes", () => {
	const allowlist = [
		{
			file: FILE,
			text: LONG_HINT,
			reason: "Tailwind class list, not UI text",
		},
	];

	expect(problemsIn(`const hint = "${LONG_HINT}";\n`, allowlist)).toEqual([]);
});

test("an allowlist entry for another file does not apply", () => {
	const allowlist = [
		{
			file: "src/features/plans/other-page.tsx",
			text: LONG_HINT,
			reason: "Tailwind class list, not UI text",
		},
	];

	expect(problemsIn(`const hint = "${LONG_HINT}";\n`, allowlist)).toEqual([
		`copy too long file=${FILE} line=1 length=62 text="${LONG_HINT}"`,
		`copy allowlist entry unused file=src/features/plans/other-page.tsx text="${LONG_HINT}"`,
	]);
});

test("an allowlist entry that matches no string fails", () => {
	const allowlist = [
		{ file: FILE, text: "Removed long text", reason: "Code template" },
	];

	expect(problemsIn('const hint = "Set a target";\n', allowlist)).toEqual([
		`copy allowlist entry unused file=${FILE} text="Removed long text"`,
	]);
});

test("em dashes, ellipses and arrows fail in short strings", () => {
	const source = [
		'const saving = "Saving\u2026";',
		"const range = `Test \u2014 live`;",
		"export const next = <span>Next \u2192</span>;",
		"export const back = <span>\u21d0 Back</span>;",
		"",
	].join("\n");

	expect(problemsIn(source)).toEqual([
		`copy character forbidden file=${FILE} line=1 character=U+2026 text="Saving\u2026"`,
		`copy character forbidden file=${FILE} line=2 character=U+2014 text="Test \u2014 live"`,
		`copy character forbidden file=${FILE} line=3 character=U+2192 text="Next \u2192"`,
		`copy character forbidden file=${FILE} line=4 character=U+21D0 text="\u21d0 Back"`,
	]);
});

test("a forbidden character fails even in an allowlisted string", () => {
	const text = `${LONG_HINT}\u2026`;
	const allowlist = [{ file: FILE, text, reason: "Code template" }];

	expect(problemsIn(`const hint = "${text}";\n`, allowlist)).toEqual([
		`copy character forbidden file=${FILE} line=1 character=U+2026 text="${text}"`,
	]);
});

test("lines count UTF-16 text before the string correctly", () => {
	const source = `const infinite = "\u221e";\nconst hint = "${LONG_HINT}";\n`;

	expect(problemsIn(source)).toEqual([
		`copy too long file=${FILE} line=2 length=62 text="${LONG_HINT}"`,
	]);
});

test("a file that does not parse fails loud", () => {
	expect(() => collectCopyStrings(FILE, "const hint = ;\n")).toThrow(
		`copy parse failed file=${FILE}`,
	);
});

test("tests, test support files and generated components are not checked", () => {
	expect(isCopyCheckedFile("src/features/plans/plan-page.tsx")).toBe(true);
	expect(isCopyCheckedFile("src/components/format-hint.ts")).toBe(true);
	expect(isCopyCheckedFile("src/features/plans/plan-page.test.tsx")).toBe(
		false,
	);
	expect(isCopyCheckedFile("src/lib/format.test.ts")).toBe(false);
	expect(
		isCopyCheckedFile("src/components/pickers/picker-test-support.tsx"),
	).toBe(false);
	expect(isCopyCheckedFile("src/features/auth/auth-test-support.ts")).toBe(
		false,
	);
	expect(isCopyCheckedFile("src/components/ui/button.tsx")).toBe(false);
	expect(isCopyCheckedFile("src/features/plans/plan-rules.css")).toBe(false);
});
