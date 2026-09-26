import { createFormHook } from "@tanstack/react-form";
import { fieldContext, formContext } from "@/components/form/form-context";
import { TextField } from "@/components/form/text-field";

/**
 * TanStack Form's useForm with the dashboard's shared field components.
 * Inside `form.AppField` the render function receives a field whose
 * `TextField` renders a labelled input bound to it.
 */
export const { useAppForm } = createFormHook({
	fieldContext,
	formContext,
	fieldComponents: { TextField },
	formComponents: {},
});
