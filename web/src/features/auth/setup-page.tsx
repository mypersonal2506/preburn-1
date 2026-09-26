import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import * as z from "zod";
import {
	completeSetupMutation,
	getCurrentMemberOptions,
	getSetupStatusOptions,
} from "@/client/@tanstack/react-query.gen";
import { zCompleteSetupBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { Button } from "@/components/ui/button";
import { FieldGroup } from "@/components/ui/field";
import {
	DISPLAY_NAME_RULE_MESSAGE,
	displayNameSchema,
	EMAIL_RULE_MESSAGE,
	newPasswordSchema,
	PASSWORD_CONFIRMATION_RULE,
	PASSWORD_RULE_MESSAGE,
	passwordsMatch,
} from "@/features/auth/member-form-rules";
import { useFragmentToken } from "@/features/auth/use-fragment-token";
import {
	type CodeMessages,
	type FieldProblems,
	mapProblemToForm,
} from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

type SetupField = "email" | "displayName" | "password";

const setupFormSchema = z
	.object({
		email: z.string().trim().min(1, "Enter your email"),
		displayName: displayNameSchema,
		password: newPasswordSchema,
		passwordConfirmation: z.string(),
	})
	.refine(passwordsMatch, PASSWORD_CONFIRMATION_RULE);

const SETUP_FIELD_PROBLEMS: FieldProblems<SetupField> = {
	"body.email": { field: "email", message: EMAIL_RULE_MESSAGE },
	"body.display_name": {
		field: "displayName",
		message: DISPLAY_NAME_RULE_MESSAGE,
	},
	"body.password": { field: "password", message: PASSWORD_RULE_MESSAGE },
};

const SETUP_CODE_MESSAGES: CodeMessages = {
	setup_token_invalid: () =>
		"This setup link is out of date. Use the newest one.",
	setup_not_available: () => "Setup is already complete. Log in instead.",
};

export function SetupPage() {
	const token = useFragmentToken();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const completeSetup = useMutation({
		...completeSetupMutation(),
		onSuccess: async (member) => {
			queryClient.setQueryData(getCurrentMemberOptions().queryKey, member);
			queryClient.setQueryData(getSetupStatusOptions().queryKey, {
				setup_required: false,
			});
			await navigate({ to: "/" });
		},
	});
	const form = useAppForm({
		defaultValues: {
			email: "",
			displayName: "",
			password: "",
			passwordConfirmation: "",
		},
		validators: { onSubmit: setupFormSchema },
		onSubmit: ({ value }) => {
			completeSetup.mutate({
				body: parseRequestBody(zCompleteSetupBody, {
					token,
					email: value.email.trim(),
					display_name: value.displayName.trim(),
					password: value.password,
				}),
			});
		},
	});
	const problem = mapProblemToForm(
		completeSetup.error,
		SETUP_FIELD_PROBLEMS,
		SETUP_CODE_MESSAGES,
	);

	if (token === "") {
		return (
			<div className="flex flex-col gap-2">
				<h1 className="text-lg font-semibold">Set up</h1>
				<p className="text-sm text-muted-foreground">
					Open the setup link from the server log.
				</p>
			</div>
		);
	}

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(event) => {
				event.preventDefault();
				void form.handleSubmit();
			}}
		>
			<h1 className="text-lg font-semibold">Set up</h1>
			<FieldGroup>
				<form.AppField name="email">
					{(field) => (
						<field.TextField
							label="Email"
							type="email"
							autoComplete="username"
							problemMessages={problem.fields.email}
						/>
					)}
				</form.AppField>
				<form.AppField name="displayName">
					{(field) => (
						<field.TextField
							label="Name"
							autoComplete="name"
							problemMessages={problem.fields.displayName}
						/>
					)}
				</form.AppField>
				<form.AppField name="password">
					{(field) => (
						<field.TextField
							label="Password"
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
			<Button type="submit" disabled={completeSetup.isPending}>
				Complete setup
			</Button>
		</form>
	);
}
