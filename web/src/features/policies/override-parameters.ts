import type { ModelParametersResponse, ParameterResponse } from "@/client";
import type {
	OverrideValue,
	RouteTarget,
} from "@/features/policies/policy-phrases";

/**
 * A parameter a policy override can set, by the name overrides use, such as
 * duration. `values` lists the values it takes, both for a boolean. An
 * integer with only a range has no values and takes whole numbers from
 * `minimum` to `maximum`.
 */
export interface OverrideParameter {
	key: string;
	values: OverrideValue[];
	minimum: number | null;
	maximum: number | null;
}

const BOOLEAN_VALUES: readonly OverrideValue[] = [true, false];

/**
 * The parameters a route override can set: those every picked target model
 * has, with the values every one of them takes. Targets without a picked
 * model are skipped, and no picked model means no parameters.
 */
export function routeOverrideParameters(
	models: readonly ModelParametersResponse[],
	targets: readonly RouteTarget[],
): OverrideParameter[] {
	const [firstParameters, ...otherParameters] = targets
		.filter((target) => target.model !== "")
		.map((target) => modelParameters(models, target));
	if (firstParameters === undefined) {
		return [];
	}
	return Object.entries(firstParameters).flatMap(([key, firstParameter]) => {
		const sameParameters = otherParameters.flatMap((parameters) => {
			const parameter = parameters[key];
			return parameter === undefined ? [] : [parameter];
		});
		if (sameParameters.length < otherParameters.length) {
			return [];
		}
		const parameter = commonParameter(key, firstParameter, sameParameters);
		return parameter === null ? [] : [parameter];
	});
}

/**
 * The parameters a cap override can set: every parameter of any model, with
 * every value some model takes, since a cap override needs to suit only one
 * model.
 */
export function capOverrideParameters(
	models: readonly ModelParametersResponse[],
): OverrideParameter[] {
	const parametersByKey = Map.groupBy(
		models.flatMap((model) => Object.entries(model.parameters)),
		([key]) => key,
	);
	return Array.from(parametersByKey, ([key, entries]) =>
		anyParameter(
			key,
			entries.map(([, parameter]) => parameter),
		),
	);
}

function modelParameters(
	models: readonly ModelParametersResponse[],
	target: RouteTarget,
): ModelParametersResponse["parameters"] {
	return (
		models.find(
			(model) =>
				model.provider === target.provider && model.model === target.model,
		)?.parameters ?? {}
	);
}

function commonParameter(
	key: string,
	firstParameter: ParameterResponse,
	otherParameters: readonly ParameterResponse[],
): OverrideParameter | null {
	const parameters = [firstParameter, ...otherParameters];
	if (parameters.every((parameter) => parameter.value_type === "boolean")) {
		return { key, values: [...BOOLEAN_VALUES], minimum: null, maximum: null };
	}
	if (parameters.every((parameter) => parameter.allowed_values.length === 0)) {
		const minimum = Math.max(...parameters.map(rangeMinimum));
		const maximum = Math.min(...parameters.map(rangeMaximum));
		return minimum > maximum ? null : { key, values: [], minimum, maximum };
	}
	const values = firstParameter.allowed_values.filter((value) =>
		otherParameters.every((parameter) =>
			includesValue(parameter.allowed_values, value),
		),
	);
	return values.length === 0
		? null
		: { key, values, minimum: null, maximum: null };
}

function anyParameter(
	key: string,
	parameters: readonly ParameterResponse[],
): OverrideParameter {
	if (parameters.every((parameter) => parameter.value_type === "boolean")) {
		return { key, values: [...BOOLEAN_VALUES], minimum: null, maximum: null };
	}
	if (parameters.every((parameter) => parameter.allowed_values.length === 0)) {
		return {
			key,
			values: [],
			minimum: Math.min(...parameters.map(rangeMinimum)),
			maximum: Math.max(...parameters.map(rangeMaximum)),
		};
	}
	const values: OverrideValue[] = [];
	for (const value of parameters.flatMap(
		(parameter) => parameter.allowed_values,
	)) {
		if (!includesValue(values, value)) {
			values.push(value);
		}
	}
	return { key, values, minimum: null, maximum: null };
}

function includesValue(
	values: readonly OverrideValue[],
	value: OverrideValue,
): boolean {
	return values.some((other) => String(other) === String(value));
}

function rangeMinimum(parameter: ParameterResponse): number {
	if (parameter.minimum === null) {
		throw new Error("parameter range missing bound=minimum");
	}
	return parameter.minimum;
}

function rangeMaximum(parameter: ParameterResponse): number {
	if (parameter.maximum === null) {
		throw new Error("parameter range missing bound=maximum");
	}
	return parameter.maximum;
}
