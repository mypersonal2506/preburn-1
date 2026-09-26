import { cn } from "cn";
import { ChevronsUpDownIcon } from "lucide-react";
import { type ReactElement, useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import {
	Command,
	CommandEmpty,
	CommandGroup,
	CommandInput,
	CommandItem,
	CommandList,
} from "@/components/ui/command";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { toApiProblem } from "@/lib/api-problem";

/**
 * One option of a picker. `value` identifies the option among the picker's
 * options, and the search matches it together with `label`. `detail` is a
 * muted second line, `choice` is what selecting the option hands back.
 */
export interface PickerOption<Choice> {
	value: string;
	label: string;
	detail: string | null;
	choice: Choice;
}

/** Options listed together, under a heading when it is set. */
export interface PickerGroup<Choice> {
	heading?: string;
	options: readonly PickerOption<Choice>[];
}

/** The next page of a paginated option list. */
export interface PickerNextPage {
	loading: boolean;
	load: () => void;
}

/**
 * Renders a picker's trigger in place of its outline button, such as a
 * sentence blank. It gets the selected option's label, "Loading" while the
 * selected option loads, or null when nothing is selected. The popover
 * attaches its handler, ref, id and ARIA attributes to the returned element,
 * so it must be a button that passes its props on.
 */
export type PickerTrigger = (label: string | null) => ReactElement;

/**
 * What a picker offers for the typed search text beyond its options: an
 * option made from the text, such as a new key, or a hint on why the text
 * cannot be one. An offered option's value must differ from every option's.
 */
export type PickerSearchOffer<Choice> =
	| { kind: "option"; option: PickerOption<Choice> }
	| { kind: "hint"; hint: string };

/**
 * Props every picker hands to PickerShell unchanged. `invalid` marks the
 * trigger invalid for a problem shown elsewhere, `invalidMessage` also
 * shows the problem at the top of the list. `defaultOpen` opens the list as
 * the picker mounts, and `trigger` replaces the outline button.
 */
export interface PickerFieldProps {
	id?: string;
	invalid?: boolean;
	invalidMessage?: string;
	defaultOpen?: boolean;
	trigger?: PickerTrigger;
}

/** Props of PickerShell. */
export interface PickerShellProps<Choice> extends PickerFieldProps {
	placeholder: string;
	searchPlaceholder: string;
	emptyText: string;
	selectedValue: string | null;
	selectedLabel: string | null;
	groups: readonly PickerGroup<Choice>[];
	loading: boolean;
	error: unknown;
	onSelect: (choice: Choice) => void;
	onSearchChange?: (search: string) => void;
	nextPage?: PickerNextPage;
	searchOffer?: (search: string) => PickerSearchOffer<Choice> | null;
}

const SEARCH_DEBOUNCE_MILLISECONDS = 250;
const LOADING_TEXT = "Loading";
const LOAD_MORE_VALUE = "load more";

/**
 * The combobox every picker is built on: a trigger button showing the
 * selected option's label, or the placeholder, opening a popover with a cmdk
 * search and the grouped options. Arrow keys move through the options, Enter
 * selects one and closes the popover, Escape closes it, and focus returns to
 * the trigger. While `loading` the list shows a loading state, and a non-null
 * `error` shows its ApiProblem detail. Without `onSearchChange` the search
 * filters the options in the browser. With it the options are left as given
 * and the search text is reported 250 ms after typing stops, for a server
 * search. `nextPage` adds a Load more option. `searchOffer` is asked about
 * non-empty search text, and its option comes after every matching option.
 * Closing clears the search.
 */
export function PickerShell<Choice>({
	id,
	invalid = false,
	invalidMessage,
	defaultOpen = false,
	trigger,
	placeholder,
	searchPlaceholder,
	emptyText,
	selectedValue,
	selectedLabel,
	groups,
	loading,
	error,
	onSelect,
	onSearchChange,
	nextPage,
	searchOffer,
}: PickerShellProps<Choice>): ReactElement {
	const [open, setOpen] = useState(defaultOpen);
	const [search, setSearch] = useState("");
	const invalidMessageId = useId();

	useEffect(() => {
		if (onSearchChange === undefined) {
			return;
		}
		const timer = window.setTimeout(
			() => onSearchChange(search),
			SEARCH_DEBOUNCE_MILLISECONDS,
		);
		return () => window.clearTimeout(timer);
	}, [search, onSearchChange]);

	function changeOpen(nextOpen: boolean): void {
		setOpen(nextOpen);
		if (!nextOpen) {
			setSearch("");
		}
	}

	function select(choice: Choice): void {
		onSelect(choice);
		changeOpen(false);
	}

	const selectedText =
		selectedLabel ?? (selectedValue !== null && loading ? LOADING_TEXT : null);
	const offer =
		searchOffer === undefined || search === "" ? null : searchOffer(search);
	const offeredOption = offer?.kind === "option" ? offer.option : null;

	return (
		<Popover open={open} onOpenChange={changeOpen}>
			<PopoverTrigger
				asChild
				id={id}
				aria-invalid={invalid || invalidMessage !== undefined}
			>
				{trigger === undefined ? (
					<Button
						variant="outline"
						className="w-full justify-between font-normal"
					>
						<span
							className={cn(
								"truncate",
								selectedLabel === null && "text-muted-foreground",
							)}
						>
							{selectedText ?? placeholder}
						</span>
						<ChevronsUpDownIcon className="opacity-50" />
					</Button>
				) : (
					trigger(selectedText)
				)}
			</PopoverTrigger>
			<PopoverContent
				align="start"
				aria-describedby={
					invalidMessage === undefined ? undefined : invalidMessageId
				}
				className="w-(--radix-popover-trigger-width) min-w-72 p-0"
			>
				{invalidMessage !== undefined && (
					<p
						id={invalidMessageId}
						className="px-3 pt-3 text-destructive text-sm"
					>
						{invalidMessage}
					</p>
				)}
				<Command shouldFilter={onSearchChange === undefined}>
					<CommandInput
						placeholder={searchPlaceholder}
						value={search}
						onValueChange={setSearch}
					/>
					{loading && (
						<div
							role="status"
							className="py-6 text-center text-muted-foreground text-sm"
						>
							{LOADING_TEXT}
						</div>
					)}
					{error !== null && (
						<div
							role="alert"
							className="px-2 py-6 text-center text-destructive text-sm"
						>
							{toApiProblem(error).detail}
						</div>
					)}
					<CommandList>
						{!loading && error === null && (
							<CommandEmpty>{emptyText}</CommandEmpty>
						)}
						{groups.map((group) => (
							<CommandGroup key={group.heading ?? ""} heading={group.heading}>
								{group.options.map((option) => (
									<CommandItem
										key={option.value}
										value={option.value}
										keywords={[option.label]}
										data-checked={option.value === selectedValue}
										onSelect={() => select(option.choice)}
									>
										<PickerOptionText option={option} />
									</CommandItem>
								))}
							</CommandGroup>
						))}
						{offeredOption !== null && (
							<CommandGroup>
								<CommandItem
									value={offeredOption.value}
									keywords={[offeredOption.label]}
									onSelect={() => select(offeredOption.choice)}
								>
									<PickerOptionText option={offeredOption} />
								</CommandItem>
							</CommandGroup>
						)}
						{nextPage !== undefined && (
							<CommandGroup forceMount>
								<CommandItem
									value={LOAD_MORE_VALUE}
									forceMount
									disabled={nextPage.loading}
									onSelect={nextPage.load}
								>
									{nextPage.loading ? LOADING_TEXT : "Load more"}
								</CommandItem>
							</CommandGroup>
						)}
					</CommandList>
					{offer?.kind === "hint" && (
						<p className="px-3 pb-3 text-muted-foreground text-xs">
							{offer.hint}
						</p>
					)}
				</Command>
			</PopoverContent>
		</Popover>
	);
}

function PickerOptionText<Choice>({
	option,
}: {
	option: PickerOption<Choice>;
}): ReactElement {
	return (
		<div className="flex min-w-0 flex-col">
			<span className="truncate">{option.label}</span>
			{option.detail !== null && (
				<span className="truncate text-muted-foreground text-xs">
					{option.detail}
				</span>
			)}
		</div>
	);
}
