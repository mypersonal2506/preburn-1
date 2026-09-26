import type * as z from "zod";

/**
 * Checks a request body built from form values against the generated Zod
 * schema of its operation, so a mapper that drifts from the API fails loud
 * instead of sending, and returns the body unchanged. The parsed output is
 * never sent, because the generated schemas turn int64 values into bigints.
 * Throws `request schema mismatch path=...` naming the invalid paths.
 */
export function parseRequestBody<
	Schema extends z.ZodType,
	Body extends z.input<Schema>,
>(schema: Schema, body: Body): Body {
	const result = schema.safeParse(body);
	if (!result.success) {
		const paths = result.error.issues
			.map((issue) => issue.path.map(String).join("."))
			.join(",");
		throw new Error(`request schema mismatch path=${paths}`);
	}
	return body;
}
