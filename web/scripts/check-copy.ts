/**
 * The copy check of spec section 22.4. Parses the dashboard source in
 * `src/features` and `src/components` and fails when a string literal, the
 * static text of a template literal or JSX text is longer than 60
 * characters and `copy-allowlist.json` does not list it with a reason, when
 * any of them holds an em dash, an ellipsis character or an arrow, or when
 * an allowlist entry matches no string. Tests, test support files and the
 * generated `src/components/ui` are not checked, and neither are class
 * names: strings inside a `className` attribute or a `cn()` call.
 *
 * Run it with `pnpm check-copy`.
 */
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import {
	type CallExpression,
	type JSXAttribute,
	parseSync,
	Visitor,
} from "oxc-parser";
import * as z from "zod";

/** A string of dashboard source, as the copy check measures it. */
export interface CopyString {
	file: string;
	line: number;
	text: string;
}

/** A long string the copy check accepts in one file, and why. */
export type CopyAllowlistEntry = z.infer<typeof allowlistEntrySchema>;

const COPY_MAXIMUM_CHARACTERS = 60;
const CHECKED_DIRECTORIES = ["src/features", "src/components"];
const GENERATED_DIRECTORY = "src/components/ui/";
const SOURCE_FILE_PATTERN = /\.tsx?$/;
const EXCLUDED_FILE_PATTERN = /(\.test|-test-support)\.tsx?$/;
const WHITESPACE_PATTERN = /\s+/g;
const FORBIDDEN_CHARACTER_PATTERN =
	/[\u2014\u2026\u2190-\u21ff\u27f0-\u27ff\u2900-\u297f]/u;
const CODE_POINT_DIGITS = 4;
const HEXADECIMAL = 16;
const ALLOWLIST_FILE = "copy-allowlist.json";
const CLASS_NAME_ATTRIBUTE = "className";
const CLASS_NAME_FUNCTION = "cn";

const allowlistEntrySchema = z.object({
	file: z.string().min(1),
	text: z.string().min(1),
	reason: z.string().min(1),
});
const allowlistSchema = z.array(allowlistEntrySchema);

/**
 * Tells whether the copy check reads the file at path, relative to the web
 * directory: TypeScript files that are not tests, test support files or
 * generated shadcn components.
 */
export function isCopyCheckedFile(path: string): boolean {
	return (
		SOURCE_FILE_PATTERN.test(path) &&
		!EXCLUDED_FILE_PATTERN.test(path) &&
		!path.startsWith(GENERATED_DIRECTORY)
	);
}

/**
 * Lists the string literals, template literals and JSX text of source,
 * except class names inside a `className` attribute or a `cn()` call. A
 * template literal's text is its static text, the parts outside `${}`, so
 * a code template of long expressions stays short. JSX text has its
 * whitespace collapsed, and whitespace-only JSX text is skipped. Throws
 * `copy parse failed file=...` when source does not parse.
 */
export function collectCopyStrings(file: string, source: string): CopyString[] {
	const parsed = parseSync(file, source);
	const [parseError] = parsed.errors;
	if (parseError !== undefined) {
		throw new Error(
			`copy parse failed file=${file} message=${parseError.message}`,
		);
	}
	const lineStarts = findLineStarts(source);
	const strings: CopyString[] = [];
	let classNameDepth = 0;
	const addString = (offset: number, text: string) => {
		if (classNameDepth === 0) {
			strings.push({ file, line: lineAt(lineStarts, offset), text });
		}
	};
	new Visitor({
		JSXAttribute(node) {
			if (isClassNameAttribute(node)) {
				classNameDepth += 1;
			}
		},
		"JSXAttribute:exit"(node) {
			if (isClassNameAttribute(node)) {
				classNameDepth -= 1;
			}
		},
		CallExpression(node) {
			if (isClassNameCall(node)) {
				classNameDepth += 1;
			}
		},
		"CallExpression:exit"(node) {
			if (isClassNameCall(node)) {
				classNameDepth -= 1;
			}
		},
		Literal(node) {
			const value = node.value;
			if (typeof value === "string") {
				addString(node.start, value);
			}
		},
		TemplateLiteral(node) {
			addString(
				node.start,
				node.quasis
					.map((quasi) => quasi.value.cooked ?? quasi.value.raw)
					.join(""),
			);
		},
		JSXText(node) {
			const raw = source.slice(node.start, node.end);
			const text = node.value.replace(WHITESPACE_PATTERN, " ").trim();
			if (text !== "") {
				addString(node.start + raw.length - raw.trimStart().length, text);
			}
		},
	}).visit(parsed.program);
	return strings;
}

/**
 * Lists the copy problems of strings as `copy ...` lines: strings over 60
 * characters that allowlist does not list for their file, strings holding an
 * em dash, an ellipsis character or an arrow (allowlisted or not), and
 * allowlist entries that match no string.
 */
export function findCopyProblems(
	strings: readonly CopyString[],
	allowlist: readonly CopyAllowlistEntry[],
): string[] {
	const problems: string[] = [];
	const usedEntries = new Set<CopyAllowlistEntry>();
	for (const copy of strings) {
		const quoted = JSON.stringify(copy.text);
		const forbidden = FORBIDDEN_CHARACTER_PATTERN.exec(copy.text);
		if (forbidden !== null) {
			problems.push(
				`copy character forbidden file=${copy.file} line=${copy.line} character=${codePointLabel(forbidden[0])} text=${quoted}`,
			);
		}
		if (copy.text.length <= COPY_MAXIMUM_CHARACTERS) {
			continue;
		}
		const entry = allowlist.find(
			(candidate) =>
				candidate.file === copy.file && candidate.text === copy.text,
		);
		if (entry === undefined) {
			problems.push(
				`copy too long file=${copy.file} line=${copy.line} length=${copy.text.length} text=${quoted}`,
			);
		} else {
			usedEntries.add(entry);
		}
	}
	for (const entry of allowlist) {
		if (!usedEntries.has(entry)) {
			problems.push(
				`copy allowlist entry unused file=${entry.file} text=${JSON.stringify(entry.text)}`,
			);
		}
	}
	return problems;
}

function isClassNameAttribute(node: JSXAttribute): boolean {
	return (
		node.name.type === "JSXIdentifier" &&
		node.name.name === CLASS_NAME_ATTRIBUTE
	);
}

function isClassNameCall(node: CallExpression): boolean {
	return (
		node.callee.type === "Identifier" &&
		node.callee.name === CLASS_NAME_FUNCTION
	);
}

function findLineStarts(source: string): number[] {
	const lineStarts = [0];
	for (let offset = 0; offset < source.length; offset++) {
		if (source[offset] === "\n") {
			lineStarts.push(offset + 1);
		}
	}
	return lineStarts;
}

function lineAt(lineStarts: readonly number[], offset: number): number {
	return lineStarts.findLastIndex((lineStart) => lineStart <= offset) + 1;
}

function codePointLabel(character: string): string {
	return `U+${character.charCodeAt(0).toString(HEXADECIMAL).toUpperCase().padStart(CODE_POINT_DIGITS, "0")}`;
}

function checkedFiles(webDirectory: string): string[] {
	return CHECKED_DIRECTORIES.flatMap((directory) =>
		readdirSync(join(webDirectory, directory), {
			recursive: true,
			encoding: "utf8",
		})
			.map((path) => join(directory, path))
			.filter(isCopyCheckedFile),
	).sort();
}

function checkCopy(webDirectory: string): void {
	const allowlist = allowlistSchema.parse(
		JSON.parse(readFileSync(join(webDirectory, ALLOWLIST_FILE), "utf8")),
	);
	const files = checkedFiles(webDirectory);
	const strings = files.flatMap((file) =>
		collectCopyStrings(file, readFileSync(join(webDirectory, file), "utf8")),
	);
	const problems = findCopyProblems(strings, allowlist);
	for (const problem of problems) {
		console.error(problem);
	}
	if (problems.length > 0) {
		process.exitCode = 1;
		return;
	}
	console.log(
		`copy checked files=${files.length} strings=${strings.length} allowlisted=${allowlist.length}`,
	);
}

if (import.meta.main) {
	checkCopy(join(import.meta.dirname, ".."));
}
