import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import * as z from "zod";
import {
	getCurrentMemberOptions,
	loginMutation,
} from "@/client/@tanstack/react-query.gen";
import { zLoginBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { Button } from "@/components/ui/button";
import { FieldGroup } from "@/components/ui/field";
import {
	type CodeMessages,
	mapProblemToForm,
	rateLimitMessage,
} from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

const loginFormSchema = z.object({
	email: z.string().trim().min(1, "Enter your email"),
	password: z.string().min(1, "Enter your password"),
});

const LOGIN_CODE_MESSAGES: CodeMessages = {
	login_failed: () => "Wrong email or password",
	rate_limited: rateLimitMessage,
};

export function LoginPage() {
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const login = useMutation({
		...loginMutation(),
		onSuccess: async (member) => {
			queryClient.setQueryData(getCurrentMemberOptions().queryKey, member);
			await navigate({ to: "/" });
		},
	});
	const form = useAppForm({
		defaultValues: { email: "", password: "" },
		validators: { onSubmit: loginFormSchema },
		onSubmit: ({ value }) => {
			login.mutate({
				body: parseRequestBody(zLoginBody, {
					email: value.email.trim(),
					password: value.password,
				}),
			});
		},
	});
	const problem = mapProblemToForm(login.error, {}, LOGIN_CODE_MESSAGES);

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(event) => {
				event.preventDefault();
				void form.handleSubmit();
			}}
		>
			<h1 className="text-lg font-semibold">Log in</h1>
			<FieldGroup>
				<form.AppField name="email">
					{(field) => (
						<field.TextField
							label="Email"
							type="email"
							autoComplete="username"
						/>
					)}
				</form.AppField>
				<form.AppField name="password">
					{(field) => (
						<field.TextField
							label="Password"
							type="password"
							autoComplete="current-password"
						/>
					)}
				</form.AppField>
			</FieldGroup>
			<FormProblemAlert messages={problem.form} />
			<Button type="submit" disabled={login.isPending}>
				Log in
			</Button>
		</form>
	);
}
