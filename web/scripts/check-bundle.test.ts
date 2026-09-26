import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { gzipSync } from "node:zlib";
import { afterEach, beforeEach, expect, test } from "vitest";
import {
	INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES,
	initialBundleProblem,
	measureInitialBundle,
} from "./check-bundle.ts";

let distDirectory = "";

beforeEach(() => {
	distDirectory = mkdtempSync(join(tmpdir(), "check-bundle-"));
	mkdirSync(join(distDirectory, ".vite"));
	mkdirSync(join(distDirectory, "assets"));
});

afterEach(() => {
	rmSync(distDirectory, { recursive: true });
});

function writeChunk(file: string, contents: string): number {
	writeFileSync(join(distDirectory, file), contents);
	return gzipSync(Buffer.from(contents)).length;
}

function writeManifest(manifest: unknown): void {
	writeFileSync(
		join(distDirectory, ".vite", "manifest.json"),
		JSON.stringify(manifest),
	);
}

test("the initial bundle is the entry chunk and its static imports, each once", () => {
	const entryBytes = writeChunk("assets/index-a1.js", "entry ".repeat(400));
	const vendorBytes = writeChunk("assets/vendor-b2.js", "vendor ".repeat(900));
	const sharedBytes = writeChunk("assets/shared-c3.js", "shared ".repeat(300));
	writeChunk("assets/customers-d4.js", "lazy route ".repeat(5000));
	writeChunk("assets/index-e5.css", "css ".repeat(5000));
	writeManifest({
		"index.html": {
			file: "assets/index-a1.js",
			isEntry: true,
			imports: ["_vendor-b2.js", "_shared-c3.js"],
			dynamicImports: ["src/routes/_app/customers/index.tsx"],
			css: ["assets/index-e5.css"],
		},
		"_vendor-b2.js": {
			file: "assets/vendor-b2.js",
			imports: ["_shared-c3.js"],
		},
		"_shared-c3.js": { file: "assets/shared-c3.js" },
		"src/routes/_app/customers/index.tsx": {
			file: "assets/customers-d4.js",
			isDynamicEntry: true,
			imports: ["_vendor-b2.js"],
		},
	});

	expect(measureInitialBundle(distDirectory)).toEqual({
		files: ["assets/index-a1.js", "assets/vendor-b2.js", "assets/shared-c3.js"],
		gzipBytes: entryBytes + vendorBytes + sharedBytes,
	});
});

test("a bundle above the maximum is a problem", () => {
	expect(
		initialBundleProblem({
			files: ["assets/index-a1.js"],
			gzipBytes: INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES + 1,
		}),
	).toBe("bundle initial too large gzip_bytes=300001 maximum_bytes=300000");
});

test("a bundle at the maximum passes", () => {
	expect(INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES).toBe(300000);
	expect(
		initialBundleProblem({
			files: ["assets/index-a1.js"],
			gzipBytes: INITIAL_BUNDLE_GZIP_MAXIMUM_BYTES,
		}),
	).toBeUndefined();
});

test("a manifest without exactly one entry fails loud", () => {
	writeChunk("assets/index-a1.js", "entry");
	writeManifest({ "_shared-c3.js": { file: "assets/index-a1.js" } });

	expect(() => measureInitialBundle(distDirectory)).toThrow(
		"bundle manifest entries=0 expected=1",
	);
});

test("an import missing from the manifest fails loud", () => {
	writeChunk("assets/index-a1.js", "entry");
	writeManifest({
		"index.html": {
			file: "assets/index-a1.js",
			isEntry: true,
			imports: ["_missing.js"],
		},
	});

	expect(() => measureInitialBundle(distDirectory)).toThrow(
		"bundle manifest import missing key=_missing.js",
	);
});
