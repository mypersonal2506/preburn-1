import { type ReactElement, useEffect, useId, useRef, useState } from "react";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
	InputGroupText,
} from "@/components/ui/input-group";
import { DecimalInputError } from "@/lib/decimal";

/**
 * How a typed input shows a value as text and reads text back. `format`
 * throws DecimalInputError for a value it cannot show, and `parse` for text
 * it rejects.
 */
export interface TextConversion<Value> {
	format: (value: Value) => string;
	parse: (text: string) => Value;
}

/**
 * Props every typed input shares. `value` is in API units, null when the
 * field is empty or its text is invalid. `invalid` marks the field invalid
 * for a problem found elsewhere, such as a server field error.
 */
export interface TypedInputProps<Value> {
	id?: string;
	"aria-label"?: string;
	invalid?: boolean;
	value: Value | null;
	onChange: (value: Value | null) => void;
}

/** Props of ConvertedInput. */
export interface ConvertedInputProps<Value> extends TypedInputProps<Value> {
	conversion: TextConversion<Value>;
	inputMode: "decimal" | "numeric";
	prefix?: string;
	suffix?: string;
}

interface TextReading<Value> {
	value: Value | null;
	problem: DecimalInputError | null;
}

interface FieldState<Value> {
	text: string;
	value: Value | null;
	unshownValue: Value | null;
}

const UNSHOWN_VALUE_MESSAGE = "Cannot show this value. Enter it again.";

/**
 * A text field that shows its value through `conversion` and reports the
 * value of every edit: the parsed value, or null for empty or rejected text.
 * Rejected text stays in the field, marked invalid with the reason below it.
 * Text that means the value already shown is kept as typed, and a value from
 * outside that means something else replaces the text. A value from outside
 * that `conversion` cannot show, such as a price with more decimals than the
 * new unit allows, leaves the field empty and marked invalid until the
 * member types, and is reported as null.
 */
export function ConvertedInput<Value>({
	id,
	"aria-label": ariaLabel,
	invalid,
	value,
	onChange,
	conversion,
	inputMode,
	prefix,
	suffix,
}: ConvertedInputProps<Value>): ReactElement {
	const [field, setField] = useState(() => showValue(value, conversion));
	const onChangeRef = useRef(onChange);
	const problemId = useId();

	const unshownValueKept =
		field.unshownValue !== null && value === field.unshownValue;
	if (!unshownValueKept && !sameValue(value, field.value, conversion)) {
		setField(showValue(value, conversion));
	}

	useEffect(() => {
		onChangeRef.current = onChange;
	});

	useEffect(() => {
		if (field.unshownValue !== null) {
			onChangeRef.current(null);
		}
	}, [field.unshownValue]);

	function changeText(nextText: string): void {
		const nextValue = readText(nextText, conversion).value;
		setField({ text: nextText, value: nextValue, unshownValue: null });
		onChange(nextValue);
	}

	const problem =
		field.unshownValue === null
			? readProblem(field.text, conversion)
			: UNSHOWN_VALUE_MESSAGE;

	return (
		<div className="flex flex-col gap-1">
			<InputGroup>
				{prefix !== undefined && (
					<InputGroupAddon>
						<InputGroupText>{prefix}</InputGroupText>
					</InputGroupAddon>
				)}
				<InputGroupInput
					id={id}
					aria-label={ariaLabel}
					aria-invalid={invalid === true || problem !== null}
					aria-describedby={problem === null ? undefined : problemId}
					autoComplete="off"
					inputMode={inputMode}
					className="numeric"
					value={field.text}
					onChange={(event) => changeText(event.target.value)}
				/>
				{suffix !== undefined && (
					<InputGroupAddon align="inline-end">
						<InputGroupText>{suffix}</InputGroupText>
					</InputGroupAddon>
				)}
			</InputGroup>
			{problem !== null && (
				<p id={problemId} className="text-destructive text-xs">
					{problem}
				</p>
			)}
		</div>
	);
}

function readText<Value>(
	text: string,
	conversion: TextConversion<Value>,
): TextReading<Value> {
	if (text.trim() === "") {
		return { value: null, problem: null };
	}
	try {
		return { value: conversion.parse(text), problem: null };
	} catch (thrown) {
		if (thrown instanceof DecimalInputError) {
			return { value: null, problem: thrown };
		}
		throw thrown;
	}
}

function readProblem<Value>(
	text: string,
	conversion: TextConversion<Value>,
): string | null {
	const { problem } = readText(text, conversion);
	return problem === null ? null : problemMessage(problem);
}

function showValue<Value>(
	value: Value | null,
	conversion: TextConversion<Value>,
): FieldState<Value> {
	if (value === null) {
		return { text: "", value: null, unshownValue: null };
	}
	try {
		return { text: conversion.format(value), value, unshownValue: null };
	} catch (thrown) {
		if (thrown instanceof DecimalInputError) {
			return { text: "", value: null, unshownValue: value };
		}
		throw thrown;
	}
}

function sameValue<Value>(
	left: Value | null,
	right: Value | null,
	conversion: TextConversion<Value>,
): boolean {
	if (left === right) {
		return true;
	}
	const leftField = showValue(left, conversion);
	const rightField = showValue(right, conversion);
	return (
		leftField.unshownValue === null &&
		rightField.unshownValue === null &&
		leftField.text === rightField.text
	);
}

function problemMessage(problem: DecimalInputError): string {
	switch (problem.problem) {
		case "invalid_format":
			return "Enter a number";
		case "too_many_decimals":
			return decimalsMessage(problem.limit);
		case "too_many_integer_digits":
			return "Number is too large";
		case "negative":
			return "Enter 0 or more";
	}
}

function decimalsMessage(limit: number | null): string {
	if (limit === 0) {
		return "Enter a whole number";
	}
	return `Use at most ${limit} ${limit === 1 ? "decimal" : "decimals"}`;
}
