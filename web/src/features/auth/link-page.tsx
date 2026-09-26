import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useId } from "react";
import * as z from "zod";
import type { LinkDetailsResponse } from "@/client";
import {
	consumeLinkMutation,
	getCurrentMemberOptions,
} from "@/client/@tanstack/react-query.gen";
import { inspectLink } from "@/client/sdk.gen";
import { zConsumeLinkBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { Button } from "@/components/ui/button";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
	newPasswordSchema,
	PASSWORD_CONFIRMATION_RULE,
	PASSWORD_RULE_MESSAGE,
	passwordsMatch,
} from "@/features/auth/member-form-rules";
import { useFragmentToken } from "@/features/auth/use-fragment-token";
import { toApiProblem } from "@/lib/api-problem";
import { type FieldProblems, mapProblemToForm } from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

type LinkField = "password";

const LINK_EXPIRED_CODE = "link_expired";

const linkFormSchema = z
	.object({
		password: newPasswordSchema,
		passwordConfirmation: z.string(),
	})
	.refine(passwordsMatch, PASSWORD_CONFIRMATION_RULE);

const LINK_FIELD_PROBLEMS: FieldProblems<LinkField> = {
	"body.password": { field: "password", message: PASSWORD_RULE_MESSAGE },
};

const LINK_TITLES: Record<LinkDetailsResponse["purpose"], string> = {
	invite: "Accept invite",
	password_reset: "Reset password",
};

export function LinkPage() {
	const token = useFragmentToken();
	const emailId = useId();
	const queryClient = useQueryClient();
	const navigate = useNavigate();
	const linkDetails = useQuery({
		queryKey: ["inspectLink", { token }],
		queryFn: async () =>
			(await inspectLink({ body: { token }, throwOnError: true })).data,
		enabled: token !== "",
		throwOnError: (error) => !isLinkExpired(error),
	});
	const consumeLink = useMutation({
		...consumeLinkMutation(),
		onSuccess: async (member) => {
			queryClient.setQueryData(getCurrentMemberOptions().queryKey, member);
			await navigate({ to: "/" });
		},
	});
	const form = useAppForm({
		defaultValues: { password: "", passwordConfirmation: "" },
		validators: { onSubmit: linkFormSchema },
		onSubmit: ({ value }) => {
			consumeLink.mutate({
				body: parseRequestBody(zConsumeLinkBody, {
					token,
					password: value.password,
				}),
			});
		},
	});
	const problem = mapProblemToForm(consumeLink.error, LINK_FIELD_PROBLEMS, {});

	if (
		token === "" ||
		isLinkExpired(linkDetails.error) ||
		isLinkExpired(consumeLink.error)
	) {
		return (
			<div className="flex flex-col gap-2">
				<h1 className="text-lg font-semibold">Link expired</h1>
				<p className="text-sm text-muted-foreground">
					Ask another member for a new link.
				</p>
			</div>
		);
	}

	if (linkDetails.data === undefined) {
		return (
			<div aria-busy="true" className="flex flex-col gap-6">
				<Skeleton className="h-7 w-40" />
				<Skeleton className="h-9 w-full" />
				<Skeleton className="h-9 w-full" />
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
			<h1 className="text-lg font-semibold">
				{LINK_TITLES[linkDetails.data.purpose]}
			</h1>
			<FieldGroup>
				<Field>
					<FieldLabel htmlFor={emailId}>Email</FieldLabel>
					<Input
						id={emailId}
						type="email"
						autoComplete="username"
						value={linkDetails.data.email}
						readOnly
					/>
				</Field>
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
			<Button type="submit" disabled={consumeLink.isPending}>
				Set password
			</Button>
		</form>
	);
}

function isLinkExpired(error: unknown): boolean {
	return error !== null && toApiProblem(error).code === LINK_EXPIRED_CODE;
}
