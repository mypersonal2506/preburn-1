import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId } from "react";
import { toast } from "sonner";
import * as z from "zod";
import type { SettingsResponse } from "@/client";
import {
	getSettingsQueryKey,
	updateSettingsMutation,
} from "@/client/@tanstack/react-query.gen";
import { zUpdateSettingsBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { PlanPicker } from "@/components/pickers/plan-picker";
import { Button } from "@/components/ui/button";
import {
	Field,
	FieldDescription,
	FieldGroup,
	FieldLabel,
} from "@/components/ui/field";
import {
	DISPLAY_NAME_RULE_MESSAGE,
	displayNameSchema,
} from "@/features/auth/member-form-rules";
import { useEnvironment } from "@/lib/environment-store";
import {
	type CodeMessages,
	type FieldProblems,
	mapProblemToForm,
} from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

interface InstallationSettingsFormProps {
	settings: SettingsResponse;
}

type InstallationField = "installationName";

const installationFormSchema = z.object({
	installationName: displayNameSchema,
	defaultPlanId: z.string().nullable(),
});

const INSTALLATION_FIELD_PROBLEMS: FieldProblems<InstallationField> = {
	"body.installation_name": {
		field: "installationName",
		message: DISPLAY_NAME_RULE_MESSAGE,
	},
};

const INSTALLATION_CODE_MESSAGES: CodeMessages = {
	plan_not_found: () => "The plan is archived or missing. Pick another.",
};

/**
 * The installation form: the installation name, which both environments
 * share, and the default plan of the current environment, which a note
 * under the picker names. Save sends both with the current environment, so
 * the other environment's default plan stays as it is. A saved form
 * refetches the settings, which also renames the installation in the
 * sidebar.
 */
export function InstallationSettingsForm({
	settings,
}: InstallationSettingsFormProps) {
	const environment = useEnvironment();
	const queryClient = useQueryClient();
	const defaultPlanPickerId = useId();
	const updateSettings = useMutation({
		...updateSettingsMutation(),
		onSuccess: async () => {
			toast.success("Settings saved");
			await queryClient.invalidateQueries({ queryKey: getSettingsQueryKey() });
		},
	});
	const form = useAppForm({
		defaultValues: {
			installationName: settings.installation_name,
			defaultPlanId: settings.default_plan_id,
		},
		validators: { onSubmit: installationFormSchema },
		onSubmit: ({ value }) => {
			updateSettings.mutate({
				body: parseRequestBody(zUpdateSettingsBody, {
					installation_name: value.installationName.trim(),
					default_plan_id: value.defaultPlanId,
				}),
			});
		},
	});
	const problem = mapProblemToForm(
		updateSettings.error,
		INSTALLATION_FIELD_PROBLEMS,
		INSTALLATION_CODE_MESSAGES,
	);

	return (
		<form
			noValidate
			className="flex flex-col gap-6"
			onSubmit={(event) => {
				event.preventDefault();
				void form.handleSubmit();
			}}
		>
			<FieldGroup>
				<form.AppField name="installationName">
					{(field) => (
						<field.TextField
							label="Name"
							autoComplete="off"
							problemMessages={problem.fields.installationName}
						/>
					)}
				</form.AppField>
				<form.Field name="defaultPlanId">
					{(field) => (
						<Field>
							<FieldLabel htmlFor={defaultPlanPickerId}>
								Default plan
							</FieldLabel>
							<div className="flex gap-2">
								<div className="min-w-0 flex-1">
									<PlanPicker
										id={defaultPlanPickerId}
										value={field.state.value}
										onChange={(plan) => field.handleChange(plan.id)}
									/>
								</div>
								{field.state.value !== null && (
									<Button
										type="button"
										variant="ghost"
										aria-label="Clear default plan"
										onClick={() => field.handleChange(null)}
									>
										Clear
									</Button>
								)}
							</div>
							<FieldDescription>
								{`Applies to the ${environment} environment only.`}
							</FieldDescription>
						</Field>
					)}
				</form.Field>
			</FieldGroup>
			<FormProblemAlert messages={problem.form} />
			<div>
				<Button type="submit" disabled={updateSettings.isPending}>
					Save
				</Button>
			</div>
		</form>
	);
}
