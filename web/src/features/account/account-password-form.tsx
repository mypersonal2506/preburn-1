import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { toast } from "sonner";
import * as z from "zod";
import {
	getCurrentMemberOptions,
	updateCurrentMemberMutation,
} from "@/client/@tanstack/react-query.gen";
import { zUpdateCurrentMemberBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { SectionCard } from "@/components/section-card";
import { Button } from "@/components/ui/button";
import { FieldGroup } from "@/components/ui/field";
import {
	newPasswordSchema,
	PASSWORD_CONFIRMATION_RULE,
	PASSWORD_RULE_MESSAGE,
	passwordsMatch,
} from "@/features/auth/member-form-rules";
import {
	type CodeMessages,
	type FieldProblems,
	mapProblemToForm,
	rateLimitMessage,
} from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

type PasswordField = "currentPassword" | "password";

const passwordFormSchema = z
	.object({
		currentPassword: z.string().min(1, "Enter your current password"),
		password: newPasswordSchema,
		passwordConfirmation: z.string(),
	})
	.refine(passwordsMatch, PASSWORD_CONFIRMATION_RULE);

const PASSWORD_FIELD_PROBLEMS: FieldProblems<PasswordField> = {
	"body.password.current": {
		field: "currentPassword",
		message: "Current password is wrong",
	},
	"body.password.new": { field: "password", message: PASSWORD_RULE_MESSAGE },
};

const PASSWORD_CODE_MESSAGES: CodeMessages = {
	rate_limited: rateLimitMessage,
};

/**
 * The Password card of the account page: the current password, a new one
 * and its confirmation. The server answers a change with a new session and
 * ends the member's other sessions, and the form empties.
 */
export function AccountPasswordForm() {
	const queryClient = useQueryClient();
	const router = useRouter();
	const changePassword = useMutation({
		...updateCurrentMemberMutation(),
		onSuccess: async (member) => {
			queryClient.setQueryData(getCurrentMemberOptions().queryKey, member);
			toast.success("Password changed");
			await router.invalidate();
		},
	});
	const form = useAppForm({
		defaultValues: {
			currentPassword: "",
			password: "",
			passwordConfirmation: "",
		},
		validators: { onSubmit: passwordFormSchema },
		onSubmit: ({ value, formApi }) => {
			changePassword.mutate(
				{
					body: parseRequestBody(zUpdateCurrentMemberBody, {
						password: { current: value.currentPassword, new: value.password },
					}),
				},
				{ onSuccess: () => formApi.reset() },
			);
		},
	});
	const problem = mapProblemToForm(
		changePassword.error,
		PASSWORD_FIELD_PROBLEMS,
		PASSWORD_CODE_MESSAGES,
	);

	return (
		<SectionCard title="Password">
			<form
				noValidate
				className="flex flex-col gap-6"
				onSubmit={(event) => {
					event.preventDefault();
					void form.handleSubmit();
				}}
			>
				<FieldGroup>
					<form.AppField name="currentPassword">
						{(field) => (
							<field.TextField
								label="Current password"
								type="password"
								autoComplete="current-password"
								problemMessages={problem.fields.currentPassword}
							/>
						)}
					</form.AppField>
					<form.AppField name="password">
						{(field) => (
							<field.TextField
								label="New password"
								type="password"
								autoComplete="new-password"
								problemMessages={problem.fields.password}
							/>
						)}
					</form.AppField>
					<form.AppField name="passwordConfirmation">
						{(field) => (
							<field.TextField
								label="Confirm password"
								type="password"
								autoComplete="new-password"
							/>
						)}
					</form.AppField>
				</FieldGroup>
				<FormProblemAlert messages={problem.form} />
				<div>
					<Button type="submit" disabled={changePassword.isPending}>
						Change password
					</Button>
				</div>
			</form>
		</SectionCard>
	);
}
