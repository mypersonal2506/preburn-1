import { expect, test } from "vitest";
import type { Problem } from "@/client";
import { ApiProblem, readApiProblem, toApiProblem } from "@/lib/api-problem";

const validationProblem: Problem = {
	type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#validation_failed",
	title: "Unprocessable Entity",
	status: 422,
	detail: "validation failed",
	code: "validation_failed",
	errors: [
		{ location: "body.email", message: "expected an email address" },
		{ location: "body.password", message: "expected 12 to 256 characters" },
	],
};

function problemResponse(
	problem: Problem,
	headers: Record<string, string> = {},
): Response {
	return new Response(JSON.stringify(problem), {
		status: problem.status,
		headers: { "Content-Type": "application/problem+json", ...headers },
	});
}

test("a problem response becomes an ApiProblem with its field errors", async () => {
	const problem = await readApiProblem(problemResponse(validationProblem));

	expect(problem).toBeInstanceOf(ApiProblem);
	expect(problem.type).toBe(validationProblem.type);
	expect(problem.title).toBe("Unprocessable Entity");
	expect(problem.status).toBe(422);
	expect(problem.code).toBe("validation_failed");
	expect(problem.detail).toBe("validation failed");
	expect(problem.errors).toEqual(validationProblem.errors);
	expect(problem.retryAfterSeconds).toBeNull();
	expect(problem.message).toBe("api problem status=422 code=validation_failed");
});

test("a problem without field errors has an empty error list", async () => {
	const problem = await readApiProblem(
		problemResponse({
			type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#login_failed",
			title: "Unauthorized",
			status: 401,
			detail: "email or password is wrong",
			code: "login_failed",
		}),
	);

	expect(problem.code).toBe("login_failed");
	expect(problem.errors).toEqual([]);
});

test("a rate limited problem carries the Retry-After seconds", async () => {
	const problem = await readApiProblem(
		problemResponse(
			{
				type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#rate_limited",
				title: "Too Many Requests",
				status: 429,
				detail: "rate limit exceeded",
				code: "rate_limited",
			},
			{ "Retry-After": "540" },
		),
	);

	expect(problem.code).toBe("rate_limited");
	expect(problem.retryAfterSeconds).toBe(540);
});

test("a response that is not a problem becomes unexpected_response", async () => {
	const problem = await readApiProblem(
		new Response("<html>bad gateway</html>", {
			status: 502,
			headers: { "Content-Type": "text/html" },
		}),
	);

	expect(problem.type).toBe("about:blank");
	expect(problem.title).toBe("Unexpected response");
	expect(problem.status).toBe(502);
	expect(problem.code).toBe("unexpected_response");
	expect(problem.errors).toEqual([]);
});

test.each(["code", "type", "title"])(
	"a problem body missing its %s becomes unexpected_response",
	async (missingField) => {
		const body = Object.fromEntries(
			Object.entries({
				type: "https://github.com/preburn/preburn/blob/main/docs/errors.md#internal_error",
				title: "Internal Server Error",
				status: 500,
				detail: "internal error",
				code: "internal_error",
			}).filter(([field]) => field !== missingField),
		);

		const problem = await readApiProblem(
			new Response(JSON.stringify(body), {
				status: 500,
				headers: { "Content-Type": "application/problem+json" },
			}),
		);

		expect(problem.status).toBe(500);
		expect(problem.code).toBe("unexpected_response");
	},
);

test("toApiProblem returns an ApiProblem unchanged", async () => {
	const problem = await readApiProblem(problemResponse(validationProblem));

	expect(toApiProblem(problem)).toBe(problem);
});

test.each([
	["a network failure", new TypeError("Failed to fetch")],
	["a thrown string", "bad gateway"],
	["undefined", undefined],
])("toApiProblem turns %s into unexpected_response", (_name, thrown) => {
	const problem = toApiProblem(thrown);

	expect(problem).toBeInstanceOf(ApiProblem);
	expect(problem.type).toBe("about:blank");
	expect(problem.title).toBe("Unexpected response");
	expect(problem.status).toBe(0);
	expect(problem.code).toBe("unexpected_response");
	expect(problem.errors).toEqual([]);
});
