import { useMutation, useQueryClient } from "@tanstack/react-query";
import { getRouteApi, useRouter } from "@tanstack/react-router";
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
	DISPLAY_NAME_RULE_MESSAGE,
	displayNameSchema,
} from "@/features/auth/member-form-rules";
import { type FieldProblems, mapProblemToForm } from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

type NameField = "displayName";

const nameFormSchema = z.object({ displayName: displayNameSchema });

const NAME_FIELD_PROBLEMS: FieldProblems<NameField> = {
	"body.display_name": {
		field: "displayName",
		message: DISPLAY_NAME_RULE_MESSAGE,
	},
};

const accountRoute = getRouteApi("/_app/account");

/**
 * The Profile card of the account page: the signed-in member's display name
 * with Save. A saved name replaces the cached member and reloads the route
 * context, so the sidebar shows it.
 */
export function AccountNameForm() {
	const member = accountRoute.useRouteContext({
		select: (context) => context.member,
	});
	const queryClient = useQueryClient();
	const router = useRouter();
	const updateName = useMutation({
		...updateCurrentMemberMutation(),
		onSuccess: async (updatedMember) => {
			queryClient.setQueryData(
				getCurrentMemberOptions().queryKey,
				updatedMember,
			);
			toast.success("Name saved");
			await router.invalidate();
		},
	});
	const form = useAppForm({
		defaultValues: { displayName: member.display_name },
		validators: { onSubmit: nameFormSchema },
		onSubmit: ({ value }) => {
			updateName.mutate({
				body: parseRequestBody(zUpdateCurrentMemberBody, {
					display_name: value.displayName.trim(),
				}),
			});
		},
	});
	const problem = mapProblemToForm(updateName.error, NAME_FIELD_PROBLEMS, {});

	return (
		<SectionCard title="Profile">
			<form
				noValidate
				className="flex flex-col gap-6"
				onSubmit={(event) => {
					event.preventDefault();
					void form.handleSubmit();
				}}
			>
				<FieldGroup>
					<form.AppField name="displayName">
						{(field) => (
							<field.TextField
								label="Name"
								autoComplete="name"
								problemMessages={problem.fields.displayName}
							/>
						)}
					</form.AppField>
				</FieldGroup>
				<FormProblemAlert messages={problem.form} />
				<div>
					<Button type="submit" disabled={updateName.isPending}>
						Save
					</Button>
				</div>
			</form>
		</SectionCard>
	);
}
