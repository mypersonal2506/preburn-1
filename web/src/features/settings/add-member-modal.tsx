import { useMutation, useQueryClient } from "@tanstack/react-query";
import * as z from "zod";
import type { AddedMemberResponse } from "@/client";
import {
	addMemberMutation,
	listMembersQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { zAddMemberBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormModal } from "@/components/form-modal";
import {
	DISPLAY_NAME_RULE_MESSAGE,
	displayNameSchema,
	EMAIL_RULE_MESSAGE,
} from "@/features/auth/member-form-rules";
import {
	type CodeMessages,
	type FieldProblems,
	mapProblemToForm,
} from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

interface AddMemberModalProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	onAdded: (invitation: AddedMemberResponse) => void;
}

type AddMemberField = "email" | "displayName";

const addMemberFormSchema = z.object({
	email: z.string().trim().min(1, "Enter an email"),
	displayName: displayNameSchema,
});

const ADD_MEMBER_FIELD_PROBLEMS: FieldProblems<AddMemberField> = {
	"body.email": { field: "email", message: EMAIL_RULE_MESSAGE },
	"body.display_name": {
		field: "displayName",
		message: DISPLAY_NAME_RULE_MESSAGE,
	},
};

const ADD_MEMBER_CODE_MESSAGES: CodeMessages = {
	member_email_taken: () => "A member with this email exists.",
};

/**
 * The Add member modal: the new member's email and name. Adding creates
 * the member without a password, refreshes the member list and hands the
 * answer, which holds the invite link, to onAdded. The form then empties
 * and the mutation forgets the answer, so only onAdded's receiver holds the
 * link.
 */
export function AddMemberModal({
	open,
	onOpenChange,
	onAdded,
}: AddMemberModalProps) {
	const queryClient = useQueryClient();
	const addMember = useMutation({
		...addMemberMutation(),
		gcTime: 0,
		onSuccess: (invitation) => {
			void queryClient.invalidateQueries({ queryKey: listMembersQueryKey() });
			onAdded(invitation);
		},
	});
	const form = useAppForm({
		defaultValues: { email: "", displayName: "" },
		validators: { onSubmit: addMemberFormSchema },
		onSubmit: ({ value, formApi }) => {
			addMember.mutate(
				{
					body: parseRequestBody(zAddMemberBody, {
						email: value.email.trim(),
						display_name: value.displayName.trim(),
					}),
				},
				{
					onSuccess: () => {
						formApi.reset();
						addMember.reset();
					},
				},
			);
		},
	});
	const problem = mapProblemToForm(
		addMember.error,
		ADD_MEMBER_FIELD_PROBLEMS,
		ADD_MEMBER_CODE_MESSAGES,
	);

	return (
		<FormModal
			open={open}
			onOpenChange={(nextOpen) => {
				if (!nextOpen) {
					form.reset();
					addMember.reset();
				}
				onOpenChange(nextOpen);
			}}
			title="Add member"
			submitLabel="Add"
			pending={addMember.isPending}
			rootError={problem.form?.join(" ")}
			onSubmit={() => {
				void form.handleSubmit();
			}}
		>
			<form.AppField name="email">
				{(field) => (
					<field.TextField
						label="Email"
						type="email"
						autoComplete="off"
						problemMessages={problem.fields.email}
					/>
				)}
			</form.AppField>
			<form.AppField name="displayName">
				{(field) => (
					<field.TextField
						label="Name"
						autoComplete="off"
						problemMessages={problem.fields.displayName}
					/>
				)}
			</form.AppField>
		</FormModal>
	);
}
