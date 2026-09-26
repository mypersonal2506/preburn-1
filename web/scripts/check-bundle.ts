/**
 * The bundle check of spec section 22.10. After `pnpm build`, reads the Vite
 * manifest in `dist`, sums the gzip size of the entry chunk and every chunk
 * it imports statically, and fails when the sum is above
 * INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES. Lazy route chunks and CSS do not count.
 *
 * Run it with `pnpm check-bundle`.
 */
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import * as z from "zod";

/** The JavaScript a browser loads before the first route renders. */
export interface InitialBundle {
	files: string[];
	gzipBytes: number;
}

/** The largest initial JavaScript the dashboard may ship, gzipped. */
export const INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES = 300000;

const MANIFEST_PATH = ".vite/manifest.json";
const DIST_DIRECTORY = "dist";

const manifestSchema = z.record(
	z.string(),
	z.object({
		file: z.string(),
		isEntry: z.boolean().optional(),
		imports: z.array(z.string()).optional(),
	}),
);

/**
 * Measures the initial bundle of the build in distDirectory: the manifest's
 * one entry chunk and its static imports, each counted once. Throws when the
 * manifest does not have exactly one entry or names an import it lacks.
 */
export function measureInitialBundle(distDirectory: string): InitialBundle {
	const manifest = manifestSchema.parse(
		JSON.parse(readFileSync(join(distDirectory, MANIFEST_PATH), "utf8")),
	);
	const entryKeys = Object.entries(manifest)
		.filter(([, chunk]) => chunk.isEntry === true)
		.map(([key]) => key);
	const [entryKey] = entryKeys;
	if (entryKey === undefined || entryKeys.length > 1) {
		throw new Error(`bundle manifest entries=${entryKeys.length} expected=1`);
	}
	const files: string[] = [];
	const visitedKeys = new Set<string>();
	const visit = (key: string) => {
		if (visitedKeys.has(key)) {
			return;
		}
		visitedKeys.add(key);
		const chunk = manifest[key];
		if (chunk === undefined) {
			throw new Error(`bundle manifest import missing key=${key}`);
		}
		files.push(chunk.file);
		for (const importKey of chunk.imports ?? []) {
			visit(importKey);
		}
	};
	visit(entryKey);
	const gzipBytes = files.reduce(
		(total, file) =>
			total + gzipSync(readFileSync(join(distDirectory, file))).length,
		0,
	);
	return { files, gzipBytes };
}

/**
 * Returns the problem line for a bundle above
 * INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES, undefined for one at or below it.
 */
export function initialBundleProblem(
	bundle: InitialBundle,
): string | undefined {
	if (bundle.gzipBytes <= INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES) {
		return undefined;
	}
	return `bundle initial too large gzip_bytes=${bundle.gzipBytes} maximum_bytes=${INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES}`;
}

function checkBundle(distDirectory: string): void {
	const bundle = measureInitialBundle(distDirectory);
	const problem = initialBundleProblem(bundle);
	if (problem !== undefined) {
		console.error(problem);
		process.exitCode = 1;
		return;
	}
	console.log(
		`bundle initial gzip_bytes=${bundle.gzipBytes} maximum_bytes=${INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES} chunks=${bundle.files.length}`,
	);
}

if (import.meta.main) {
	checkBundle(join(import.meta.dirname, "..", DIST_DIRECTORY));
}
