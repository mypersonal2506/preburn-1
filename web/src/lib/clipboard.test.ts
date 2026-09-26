import { afterEach, expect, test, vi } from "vitest";
import { copyText } from "@/lib/clipboard";

afterEach(() => {
	vi.unstubAllGlobals();
});

test("resolves true after the clipboard takes the text", async () => {
	const writeText = vi.fn(async () => {});
	vi.stubGlobal("navigator", { clipboard: { writeText } });

	expect(await copyText("pln_01jbvagescfn78y0938nkrkayd")).toBe(true);
	expect(writeText).toHaveBeenCalledWith("pln_01jbvagescfn78y0938nkrkayd");
});

test("resolves false when the clipboard refuses", async () => {
	const writeText = vi.fn(async () => {
		throw new DOMException("write refused", "NotAllowedError");
	});
	vi.stubGlobal("navigator", { clipboard: { writeText } });

	expect(await copyText("pln_01jbvagescfn78y0938nkrkayd")).toBe(false);
});

test("resolves false outside a secure context, where the clipboard is missing", async () => {
	vi.stubGlobal("navigator", {});

	expect(await copyText("pln_01jbvagescfn78y0938nkrkayd")).toBe(false);
});
