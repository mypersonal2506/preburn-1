import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useId, useState } from "react";
import { toast } from "sonner";
import * as z from "zod";
import {
	createApiKeyMutation,
	getDashboardOnboardingQueryKey,
	listApiKeysQueryKey,
} from "@/client/@tanstack/react-query.gen";
import { zCreateApiKeyBody } from "@/client/zod.gen";
import { useAppForm } from "@/components/form/app-form";
import { FormModal } from "@/components/form-modal";
import {
	Field,
	FieldContent,
	FieldDescription,
	FieldLabel,
	FieldLegend,
	FieldSet,
	FieldTitle,
} from "@/components/ui/field";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
	API_KEY_SCOPE_DISPLAYS,
	type ApiKeyScope,
	apiKeyScopeSchema,
} from "@/features/developers/api-key-scopes";
import { ApiKeySecretModal } from "@/features/developers/api-key-secret-modal";
import { type FieldProblems, mapProblemToForm } from "@/lib/form-problem";
import { parseRequestBody } from "@/lib/request-body";

interface CreateApiKeyModalProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
}

interface CreateApiKeyFormValues {
	name: string;
	scope: ApiKeyScope;
}

type CreateApiKeyField = "name";

const NAME_MAXIMUM_CHARACTERS = 80;
const NAME_RULE_MESSAGE = "Enter a name of 1 to 80 characters.";

const createApiKeyFormSchema = z.object({
	name: z
		.string()
		.trim()
		.min(1, NAME_RULE_MESSAGE)
		.max(NAME_MAXIMUM_CHARACTERS, NAME_RULE_MESSAGE),
	scope: apiKeyScopeSchema,
});

const CREATE_API_KEY_DEFAULT_VALUES: CreateApiKeyFormValues = {
	name: "",
	scope: "runtime",
};

const CREATE_API_KEY_FIELD_PROBLEMS: FieldProblems<CreateApiKeyField> = {
	"body.name": { field: "name", message: NAME_RULE_MESSAGE },
};

/**
 * The Create API key modal: a name and the scope as option cards, Runtime
 * first. A created key closes the form, refreshes the key list and the
 * onboarding status, and opens ApiKeySecretModal with the secret, also when
 * the form was closed while the key was being created. The secret lives
 * only in its state, never in the query or mutation cache, so keep it
 * mounted while the secret shows.
 */
export function CreateApiKeyModal({
	open,
	onOpenChange,
}: CreateApiKeyModalProps) {
	const queryClient = useQueryClient();
	const scopeIdPrefix = useId();
	const [secret, setSecret] = useState<string | null>(null);
	const createKey = useMutation({
		...createApiKeyMutation(),
		gcTime: 0,
		onSuccess: (createdKey) => {
			setSecret(createdKey.secret);
			closeForm();
			toast.success("API key created");
			void queryClient.invalidateQueries({ queryKey: listApiKeysQueryKey() });
			void queryClient.invalidateQueries({
				queryKey: getDashboardOnboardingQueryKey(),
			});
		},
	});
	const form = useAppForm({
		defaultValues: CREATE_API_KEY_DEFAULT_VALUES,
		validators: { onSubmit: createApiKeyFormSchema },
		onSubmit: ({ value }) => {
			createKey.mutate({
				body: parseRequestBody(zCreateApiKeyBody, {
					name: value.name.trim(),
					scope: value.scope,
				}),
			});
		},
	});
	const problem = mapProblemToForm(
		createKey.error,
		CREATE_API_KEY_FIELD_PROBLEMS,
	);

	function closeForm(): void {
		form.reset();
		createKey.reset();
		onOpenChange(false);
	}

	return (
		<>
			<FormModal
				open={open}
				onOpenChange={(nextOpen) => {
					if (!nextOpen) {
						closeForm();
					}
				}}
				title="Create API key"
				submitLabel="Create"
				pending={createKey.isPending}
				rootError={problem.form?.join(" ")}
				onSubmit={() => void form.handleSubmit()}
			>
				<form.AppField name="name">
					{(field) => (
						<field.TextField
							label="Name"
							autoComplete="off"
							problemMessages={problem.fields.name}
						/>
					)}
				</form.AppField>
				<form.Field name="scope">
					{(field) => (
						<FieldSet>
							<FieldLegend variant="label">Scope</FieldLegend>
							<RadioGroup
								value={field.state.value}
								onValueChange={(scope) =>
									field.handleChange(apiKeyScopeSchema.parse(scope))
								}
							>
								{apiKeyScopeSchema.options.map((scope) => (
									<FieldLabel key={scope} htmlFor={`${scopeIdPrefix}-${scope}`}>
										<Field orientation="horizontal">
											<FieldContent>
												<FieldTitle>
													{API_KEY_SCOPE_DISPLAYS[scope].label}
												</FieldTitle>
												<FieldDescription>
													{API_KEY_SCOPE_DISPLAYS[scope].description}
												</FieldDescription>
											</FieldContent>
											<RadioGroupItem
												id={`${scopeIdPrefix}-${scope}`}
												value={scope}
											/>
										</Field>
									</FieldLabel>
								))}
							</RadioGroup>
						</FieldSet>
					)}
				</form.Field>
			</FormModal>
			{secret !== null && (
				<ApiKeySecretModal secret={secret} onDone={() => setSecret(null)} />
			)}
		</>
	);
}
