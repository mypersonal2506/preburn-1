import { useForm } from "@tanstack/react-form";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { describe, expect, test } from "vitest";
import type { ProblemError } from "@/client";
import { ApiProblem } from "@/lib/api-problem";
import {
	bodyFieldProblems,
	mapProblemToForm,
	rateLimitMessage,
} from "@/lib/form-problem";

const policyFieldProblems = bodyFieldProblems([
	"name",
	"feature",
	"when.all[0].signal",
	"when.all[0].value",
]);

const problemForServer = policyInvalid([
	{ location: "body.name", message: "expected 1 to 120 characters" },
	{
		location: "body.when.all[0].value",
		message: "expected a ratio string with at most 4 decimals",
	},
	{
		location: "body.action.route_chain",
		message: "expected 1 to 5 route targets for outcome route",
	},
]);

function policyInvalid(errors: ProblemError[]): ApiProblem {
	return problem(422, "policy_invalid", errors);
}

function problem(
	status: number,
	code: string,
	errors: ProblemError[],
	retryAfterSeconds: number | null = null,
): ApiProblem {
	return new ApiProblem({
		type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
		title: "Error",
		status,
		code,
		detail: code.replaceAll("_", " "),
		errors,
		retryAfterSeconds,
	});
}

function PolicyForm(): ReactElement {
	const form = useForm({
		defaultValues: {
			name: "",
			when: {
				all: [{ signal: "pace", operator: "gt", value: "2.12345" }],
			},
		},
	});
	return (
		<>
			<form.Field name="name">
				{(field) => (
					<>
						<input
							aria-label="Name"
							value={field.state.value}
							onChange={(event) => field.handleChange(event.target.value)}
						/>
						<span>{field.state.meta.errors.join(" ")}</span>
					</>
				)}
			</form.Field>
			<form.Field name="when.all[0].value">
				{(field) => (
					<p data-testid="condition value">
						{field.state.meta.errors.join(" ")}
					</p>
				)}
			</form.Field>
			<form.Subscribe selector={(state) => state.errors}>
				{(formErrors) => (
					<ul>
						{formErrors.flat().map((message) => (
							<li key={String(message)}>{String(message)}</li>
						))}
					</ul>
				)}
			</form.Subscribe>
			<button
				type="button"
				onClick={() =>
					form.setErrorMap({
						onSubmit: mapProblemToForm(
							problemForServer,
							bodyFieldProblems(Object.keys(form.fieldInfo)),
						),
					})
				}
			>
				Save
			</button>
		</>
	);
}

describe("mapProblemToForm", () => {
	test("no error maps to no messages", () => {
		expect(mapProblemToForm(null, policyFieldProblems)).toEqual({
			fields: {},
		});
	});

	test("body.when.all[0].value maps to the when.all[0].value field", () => {
		const errors = mapProblemToForm(
			policyInvalid([
				{
					location: "body.when.all[0].value",
					message: "expected a ratio string with at most 4 decimals",
				},
				{
					location: "body.name",
					message: "expected 1 to 120 characters",
				},
			]),
			policyFieldProblems,
		);

		expect(errors).toEqual({
			fields: {
				"when.all[0].value": [
					"expected a ratio string with at most 4 decimals",
				],
				name: ["expected 1 to 120 characters"],
			},
		});
	});

	test("a field problem with a message shows it in place of the server message", () => {
		const errors = mapProblemToForm(
			problem(422, "validation_failed", [
				{
					location: "body.display_name",
					message: "expected 1 to 80 characters",
				},
			]),
			{
				"body.display_name": {
					field: "displayName",
					message: "Use 1 to 80 characters",
				},
			},
		);

		expect(errors).toEqual({
			fields: { displayName: ["Use 1 to 80 characters"] },
		});
	});

	test("unknown locations go to the form list with their location", () => {
		const errors = mapProblemToForm(
			policyInvalid([
				{
					location: "body.action.route_chain",
					message: "expected 1 to 5 route targets for outcome route",
				},
				{ location: "body.when.all[1]", message: "expected a condition" },
				{ location: "query.limit", message: "expected at most 100" },
				{ location: "body", message: "request body is not valid JSON" },
				{ location: "body.feature", message: "expected a feature name" },
			]),
			policyFieldProblems,
		);

		expect(errors).toEqual({
			fields: { feature: ["expected a feature name"] },
			form: [
				"action.route_chain: expected 1 to 5 route targets for outcome route",
				"when.all[1]: expected a condition",
				"query.limit: expected at most 100",
				"body: request body is not valid JSON",
			],
		});
	});

	test("a field keeps every message it gets", () => {
		const errors = mapProblemToForm(
			policyInvalid([
				{ location: "body.name", message: "expected 1 to 120 characters" },
				{ location: "body.name", message: "expected no control characters" },
			]),
			policyFieldProblems,
		);

		expect(errors.fields).toEqual({
			name: ["expected 1 to 120 characters", "expected no control characters"],
		});
	});

	test("a code message goes to the form list next to the field errors", () => {
		const errors = mapProblemToForm(
			policyInvalid([
				{ location: "body.name", message: "expected 1 to 120 characters" },
			]),
			policyFieldProblems,
			{ policy_invalid: () => "Fix the marked fields" },
		);

		expect(errors).toEqual({
			fields: { name: ["expected 1 to 120 characters"] },
			form: ["Fix the marked fields"],
		});
	});

	test("a problem without field errors or a code message shows the generic message", () => {
		const errors = mapProblemToForm(
			problem(422, "plan_not_found", []),
			policyFieldProblems,
		);

		expect(errors).toEqual({
			fields: {},
			form: ["Something went wrong. Try again."],
		});
	});

	test("an error that is not an API problem shows the generic message", () => {
		const errors = mapProblemToForm(
			new TypeError("Failed to fetch"),
			policyFieldProblems,
		);

		expect(errors).toEqual({
			fields: {},
			form: ["Something went wrong. Try again."],
		});
	});
});

describe("rateLimitMessage", () => {
	test("says the wait in seconds under a minute", () => {
		expect(rateLimitMessage(problem(429, "rate_limited", [], 30))).toBe(
			"Too many attempts. Try again in 30 seconds.",
		);
	});

	test("says the wait in minutes rounded up from a minute", () => {
		expect(rateLimitMessage(problem(429, "rate_limited", [], 841))).toBe(
			"Too many attempts. Try again in 15 minutes.",
		);
	});

	test("throws without a Retry-After wait", () => {
		expect(() => rateLimitMessage(problem(429, "rate_limited", []))).toThrow(
			"retry after missing code=rate_limited",
		);
	});
});

test("the errors land on TanStack Form fields and the form error list", async () => {
	const user = userEvent.setup();
	render(<PolicyForm />);

	await user.click(screen.getByRole("button", { name: "Save" }));

	expect(screen.getByText("expected 1 to 120 characters")).toBeInTheDocument();
	expect(screen.getByTestId("condition value")).toHaveTextContent(
		"expected a ratio string with at most 4 decimals",
	);
	expect(screen.getByRole("listitem")).toHaveTextContent(
		"action.route_chain: expected 1 to 5 route targets for outcome route",
	);

	await user.type(screen.getByRole("textbox", { name: "Name" }), "Stop losses");
	expect(
		screen.queryByText("expected 1 to 120 characters"),
	).not.toBeInTheDocument();
	expect(screen.queryByRole("listitem")).not.toBeInTheDocument();
});
