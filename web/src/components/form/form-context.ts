import { createFormHookContexts } from "@tanstack/react-form";

/**
 * The React contexts that connect useAppForm forms to their field
 * components. Field components read their field with useFieldContext.
 */
export const { fieldContext, formContext, useFieldContext } =
	createFormHookContexts();
