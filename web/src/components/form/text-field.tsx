import { useId } from "react";
import { useFieldContext } from "@/components/form/form-context";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

interface TextFieldProps {
	label: string;
	type?: "text" | "email" | "password";
	autoComplete: string;
	problemMessages?: readonly string[];
}

interface FieldMessage {
	message: string;
}

/**
 * A labelled text input for the enclosing `form.AppField`. Below the input
 * it lists the field's validation messages and problemMessages, the
 * messages for server errors at the field, and marks the input invalid
 * while any shows.
 */
export function TextField({
	label,
	type = "text",
	autoComplete,
	problemMessages = [],
}: TextFieldProps) {
	const field = useFieldContext<string>();
	const inputId = useId();
	const messagesId = useId();
	const messages = [
		...field.state.meta.errors.map((error: unknown) =>
			fieldMessage(field.name, error),
		),
		...problemMessages.map((message) => ({ message })),
	];
	const invalid = messages.length > 0;

	return (
		<Field data-invalid={invalid}>
			<FieldLabel htmlFor={inputId}>{label}</FieldLabel>
			<Input
				id={inputId}
				name={field.name}
				type={type}
				autoComplete={autoComplete}
				value={field.state.value}
				onBlur={field.handleBlur}
				onChange={(event) => field.handleChange(event.target.value)}
				aria-invalid={invalid}
				aria-describedby={invalid ? messagesId : undefined}
			/>
			<FieldError id={messagesId} errors={messages} />
		</Field>
	);
}

function fieldMessage(fieldName: string, error: unknown): FieldMessage {
	if (
		typeof error !== "object" ||
		error === null ||
		!("message" in error) ||
		typeof error.message !== "string"
	) {
		throw new Error(`field error without message field=${fieldName}`);
	}
	return { message: error.message };
}
