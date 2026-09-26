import * as z from "zod";

/** The values of a form that sets a new password and repeats it. */
export interface NewPasswordValues {
	password: string;
	passwordConfirmation: string;
}

const PASSWORD_MINIMUM_LENGTH = 12;
const PASSWORD_MAXIMUM_LENGTH = 256;
const DISPLAY_NAME_MAXIMUM_LENGTH = 80;

/** The password rule, shown under a new password field that breaks it. */
export const PASSWORD_RULE_MESSAGE = `Use ${PASSWORD_MINIMUM_LENGTH} to ${PASSWORD_MAXIMUM_LENGTH} characters`;

/** The display name rule, shown under a name field that breaks it. */
export const DISPLAY_NAME_RULE_MESSAGE = `Use 1 to ${DISPLAY_NAME_MAXIMUM_LENGTH} characters`;

/** Shown under an email field whose address the server rejects. */
export const EMAIL_RULE_MESSAGE = "Enter a valid email address";

/**
 * The form-level rule that the confirmation repeats the new password. Its
 * message shows under the `passwordConfirmation` field.
 */
export const PASSWORD_CONFIRMATION_RULE = {
	message: "Passwords do not match",
	path: ["passwordConfirmation"],
};

/**
 * A new password of 12 to 256 characters. Characters are Unicode code
 * points, as the server counts them.
 */
export const newPasswordSchema = z
	.string()
	.refine(
		(password) =>
			isLengthBetween(
				password,
				PASSWORD_MINIMUM_LENGTH,
				PASSWORD_MAXIMUM_LENGTH,
			),
		PASSWORD_RULE_MESSAGE,
	);

/**
 * A display name of 1 to 80 characters once surrounding spaces are removed.
 * Request bodies carry it trimmed.
 */
export const displayNameSchema = z
	.string()
	.refine(
		(displayName) =>
			isLengthBetween(displayName.trim(), 1, DISPLAY_NAME_MAXIMUM_LENGTH),
		DISPLAY_NAME_RULE_MESSAGE,
	);

/**
 * Tells whether the confirmation repeats the new password. Pair it with
 * PASSWORD_CONFIRMATION_RULE in a form schema's refine.
 */
export function passwordsMatch(values: NewPasswordValues): boolean {
	return values.password === values.passwordConfirmation;
}

function isLengthBetween(
	text: string,
	minimum: number,
	maximum: number,
): boolean {
	const length = Array.from(text).length;
	return length >= minimum && length <= maximum;
}
