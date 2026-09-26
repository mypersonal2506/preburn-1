import { cn } from "cn";
import { PlusIcon, XIcon } from "lucide-react";
import { type ComponentType, type ReactElement, useRef } from "react";
import type { PolicyCondition } from "@/client";
import { zPolicyCondition } from "@/client/zod.gen";
import type { TypedInputProps } from "@/components/inputs/converted-input";
import { Button } from "@/components/ui/button";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { conditionOperatorLabel } from "@/lib/labels";

/** How a condition compares its signal with its value, as the API writes it. */
export type ConditionOperator = PolicyCondition["operator"];

/** One condition: a signal compared with a value, as the API writes it. */
export interface Condition<Signal extends string> {
	signal: Signal;
	operator: ConditionOperator;
	value: string;
}

/**
 * A group that holds when all or any of its members hold, as the API
 * writes it: exactly one of `all` and `any` is set.
 */
export interface ConditionGroup<Signal extends string> {
	all?: ConditionMember<Signal>[];
	any?: ConditionMember<Signal>[];
}

/** A member of a condition group: a condition or a nested group. */
export type ConditionMember<Signal extends string> =
	| Condition<Signal>
	| ConditionGroup<Signal>;

/**
 * One signal a condition can compare: its key, its phrase, such as
 * "allowance left", and the typed input for its API value, such as
 * AmountInput for a money signal.
 */
export interface ConditionSignal<Signal extends string> {
	signal: Signal;
	phrase: string;
	input: ComponentType<TypedInputProps<string>>;
}

/**
 * The signals a ConditionPanel offers, at least one. A new condition
 * compares the first.
 */
export type ConditionCatalog<Signal extends string> = readonly [
	ConditionSignal<Signal>,
	...ConditionSignal<Signal>[],
];

/** Props of ConditionPanel. */
export interface ConditionPanelProps<Signal extends string> {
	catalog: ConditionCatalog<Signal>;
	value: ConditionGroup<Signal>;
	onChange: (group: ConditionGroup<Signal>) => void;
}

type GroupMatch = "all" | "any";

interface ReadGroup<Signal extends string> {
	match: GroupMatch;
	members: ConditionMember<Signal>[];
}

interface ConditionGroupEditorProps<Signal extends string> {
	catalog: ConditionCatalog<Signal>;
	group: ConditionGroup<Signal>;
	depth: number;
	onChange: (group: ConditionGroup<Signal>) => void;
	onRemove?: () => void;
}

interface MemberKeys<Signal extends string> {
	keyOf: (member: ConditionMember<Signal>) => string;
	carry: (
		member: ConditionMember<Signal>,
		nextMember: ConditionMember<Signal>,
	) => void;
}

interface ConditionRowProps<Signal extends string> {
	catalog: ConditionCatalog<Signal>;
	condition: Condition<Signal>;
	onChange: (condition: Condition<Signal>) => void;
	onRemove: () => void;
}

const CONDITION_GROUP_DEPTH_MAXIMUM = 3;
const CONDITION_OPERATORS: readonly ConditionOperator[] = [
	"gt",
	"gte",
	"lt",
	"lte",
	"eq",
	"ne",
];
const NEW_CONDITION_OPERATOR: ConditionOperator = "gt";

/**
 * Edits a condition group in a wide Blank popover, generic over a catalog
 * of signals: "Match [all or any] of", then a row per condition with its
 * signal, comparison and value input, and the nested groups. Groups nest at
 * most 3 deep counting the outer one, the API limit, so the deepest groups
 * offer no Add group. Every condition and nested group has a remove button.
 * A new group matches all of no members, and a new condition compares the
 * catalog's first signal with an empty value. Changing a signal empties the
 * value, whose unit follows the signal.
 */
export function ConditionPanel<Signal extends string>({
	catalog,
	value,
	onChange,
}: ConditionPanelProps<Signal>): ReactElement {
	return (
		<ConditionGroupEditor
			catalog={catalog}
			group={value}
			depth={1}
			onChange={onChange}
		/>
	);
}

function ConditionGroupEditor<Signal extends string>({
	catalog,
	group,
	depth,
	onChange,
	onRemove,
}: ConditionGroupEditorProps<Signal>): ReactElement {
	const memberKeys = useMemberKeys<Signal>();
	const { match, members } = readGroup(group);

	function changeMembers(nextMembers: ConditionMember<Signal>[]): void {
		onChange(writeGroup(match, nextMembers));
	}

	function replaceMember(
		index: number,
		member: ConditionMember<Signal>,
		nextMember: ConditionMember<Signal>,
	): void {
		memberKeys.carry(member, nextMember);
		changeMembers(members.with(index, nextMember));
	}

	return (
		<fieldset
			aria-label={depth === 1 ? "Conditions" : "Condition group"}
			className={cn(
				"flex min-w-0 flex-col gap-2",
				depth > 1 && "rounded-md border border-border p-2",
			)}
		>
			<div className="flex items-center gap-1.5 text-sm">
				Match
				<Button
					type="button"
					variant="outline"
					size="xs"
					onClick={() =>
						onChange(writeGroup(match === "all" ? "any" : "all", members))
					}
				>
					{match}
				</Button>
				of
				{onRemove !== undefined && (
					<Button
						type="button"
						variant="ghost"
						size="icon-xs"
						aria-label="Remove group"
						className="ml-auto"
						onClick={onRemove}
					>
						<XIcon />
					</Button>
				)}
			</div>
			{members.map((member, index) =>
				isCondition(member) ? (
					<ConditionRow
						key={memberKeys.keyOf(member)}
						catalog={catalog}
						condition={member}
						onChange={(nextCondition) =>
							replaceMember(index, member, nextCondition)
						}
						onRemove={() => changeMembers(members.toSpliced(index, 1))}
					/>
				) : (
					<ConditionGroupEditor
						key={memberKeys.keyOf(member)}
						catalog={catalog}
						group={member}
						depth={depth + 1}
						onChange={(nextGroup) => replaceMember(index, member, nextGroup)}
						onRemove={() => changeMembers(members.toSpliced(index, 1))}
					/>
				),
			)}
			<div className="flex gap-1">
				<Button
					type="button"
					variant="ghost"
					size="xs"
					onClick={() =>
						changeMembers([
							...members,
							{
								signal: catalog[0].signal,
								operator: NEW_CONDITION_OPERATOR,
								value: "",
							},
						])
					}
				>
					<PlusIcon />
					Add condition
				</Button>
				{depth < CONDITION_GROUP_DEPTH_MAXIMUM && (
					<Button
						type="button"
						variant="ghost"
						size="xs"
						onClick={() => changeMembers([...members, { all: [] }])}
					>
						<PlusIcon />
						Add group
					</Button>
				)}
			</div>
		</fieldset>
	);
}

function ConditionRow<Signal extends string>({
	catalog,
	condition,
	onChange,
	onRemove,
}: ConditionRowProps<Signal>): ReactElement {
	const conditionSignal = signalOf(catalog, condition.signal);
	const ValueInput = conditionSignal.input;

	return (
		<div className="flex flex-wrap items-center gap-1.5 text-sm">
			<Select
				value={condition.signal}
				onValueChange={(signal) =>
					onChange({
						signal: signalOf(catalog, signal).signal,
						operator: condition.operator,
						value: "",
					})
				}
			>
				<SelectTrigger size="sm" aria-label="Signal">
					<SelectValue>{conditionSignal.phrase}</SelectValue>
				</SelectTrigger>
				<SelectContent>
					{catalog.map((entry) => (
						<SelectItem key={entry.signal} value={entry.signal}>
							{entry.phrase}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			is
			<Select
				value={condition.operator}
				onValueChange={(operator) =>
					onChange({
						...condition,
						operator: zPolicyCondition.shape.operator.parse(operator),
					})
				}
			>
				<SelectTrigger size="sm" aria-label="Comparison">
					<SelectValue>
						{conditionOperatorLabel(condition.operator)}
					</SelectValue>
				</SelectTrigger>
				<SelectContent>
					{CONDITION_OPERATORS.map((operator) => (
						<SelectItem key={operator} value={operator}>
							{conditionOperatorLabel(operator)}
						</SelectItem>
					))}
				</SelectContent>
			</Select>
			<div className="w-40">
				<ValueInput
					aria-label="Value"
					value={condition.value === "" ? null : condition.value}
					onChange={(nextValue) =>
						onChange({ ...condition, value: nextValue ?? "" })
					}
				/>
			</div>
			<Button
				type="button"
				variant="ghost"
				size="icon-xs"
				aria-label="Remove condition"
				onClick={onRemove}
			>
				<XIcon />
			</Button>
		</div>
	);
}

function useMemberKeys<Signal extends string>(): MemberKeys<Signal> {
	const keys = useRef(new WeakMap<ConditionMember<Signal>, string>());
	const keyCount = useRef(0);

	function keyOf(member: ConditionMember<Signal>): string {
		const key = keys.current.get(member);
		if (key !== undefined) {
			return key;
		}
		keyCount.current += 1;
		const newKey = String(keyCount.current);
		keys.current.set(member, newKey);
		return newKey;
	}

	function carry(
		member: ConditionMember<Signal>,
		nextMember: ConditionMember<Signal>,
	): void {
		keys.current.set(nextMember, keyOf(member));
	}

	return { keyOf, carry };
}

function readGroup<Signal extends string>(
	group: ConditionGroup<Signal>,
): ReadGroup<Signal> {
	if (group.any !== undefined) {
		return { match: "any", members: group.any };
	}
	if (group.all !== undefined) {
		return { match: "all", members: group.all };
	}
	throw new Error("condition group invalid members=none");
}

function writeGroup<Signal extends string>(
	match: GroupMatch,
	members: ConditionMember<Signal>[],
): ConditionGroup<Signal> {
	return match === "all" ? { all: members } : { any: members };
}

function isCondition<Signal extends string>(
	member: ConditionMember<Signal>,
): member is Condition<Signal> {
	return "signal" in member;
}

function signalOf<Signal extends string>(
	catalog: ConditionCatalog<Signal>,
	signal: string,
): ConditionSignal<Signal> {
	const conditionSignal = catalog.find((entry) => entry.signal === signal);
	if (conditionSignal === undefined) {
		throw new Error(`condition signal unknown signal=${signal}`);
	}
	return conditionSignal;
}
