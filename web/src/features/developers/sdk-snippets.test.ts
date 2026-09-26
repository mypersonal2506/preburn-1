import { expect, test } from "vitest";
import { curlRevenueSnippet } from "@/features/developers/sdk-snippets";

const ORIGIN = "https://preburn.example.com";

test("the revenue request covers the current UTC month", () => {
	const snippet = curlRevenueSnippet(
		ORIGIN,
		new Date("2026-09-30T22:00:00-05:00"),
	);

	expect(snippet.label).toBe("curl");
	expect(snippet.code).toContain(
		"curl -X POST https://preburn.example.com/api/v1/revenue \\",
	);
	expect(snippet.code).toContain('"period_start": "2026-10-01T00:00:00.000Z"');
	expect(snippet.code).toContain('"period_end": "2026-11-01T00:00:00.000Z"');
});

test("the revenue request of December ends in January", () => {
	const snippet = curlRevenueSnippet(ORIGIN, new Date("2026-12-31T12:00:00Z"));

	expect(snippet.code).toContain('"period_start": "2026-12-01T00:00:00.000Z"');
	expect(snippet.code).toContain('"period_end": "2027-01-01T00:00:00.000Z"');
});

test("the request body is valid JSON inside the single quotes", () => {
	const snippet = curlRevenueSnippet(ORIGIN, new Date("2026-09-26T12:00:00Z"));

	const body = snippet.code.slice(
		snippet.code.indexOf("-d '") + "-d '".length,
		snippet.code.lastIndexOf("'"),
	);
	expect(JSON.parse(body)).toEqual({
		customer_id: "customer_42",
		kind: "subscription",
		amount: "49.00",
		period_start: "2026-09-01T00:00:00.000Z",
		period_end: "2026-10-01T00:00:00.000Z",
		source_reference: "invoice_2026_09",
	});
});
