/** The name input under the sentence. */
export const NAME_BLANK = "name";

/** Who the policy applies to: all customers, a plan or one customer. */
export const WHO_BLANK = "who";

/** The feature the policy applies to, or any feature. */
export const FEATURE_BLANK = "feature";

/** "always", the match of inline conditions, or the condition panel. */
export const WHEN_BLANK = "when";

/** The outcome: allow, route, cap or deny. */
export const OUTCOME_BLANK = "outcome";

/** Every setting of a cap, such as "4s, no audio". */
export const CAP_SETTINGS_BLANK = "cap_settings";

/** The per period limit of a cap. */
export const LIMIT_BLANK = "limit";

/** The signal of the inline condition at index, such as "pace". */
export function conditionSignalBlank(index: number): string {
	return `when.${index}.signal`;
}

/** The comparison of the inline condition at index, such as "above 2.0x". */
export function conditionComparisonBlank(index: number): string {
	return `when.${index}.comparison`;
}

/** The model of the route chain entry at index. */
export function routeTargetBlank(index: number): string {
	return `route_chain.${index}`;
}

/** The route override of the parameter key, such as "5s". */
export function routeOverrideBlank(key: string): string {
	return `overrides.${key}`;
}
