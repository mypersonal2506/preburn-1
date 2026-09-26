import { type ReactElement, type ReactNode, useId } from "react";
import { BlankPhrase } from "@/components/sentence/blank-phrase";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";

/** Props of Blank. */
export interface BlankProps {
	placeholder: string;
	phrase: string | null;
	invalidMessage?: string;
	defaultOpen?: boolean;
	wide?: boolean;
	children: ReactNode;
}

/**
 * A blank of a sentence: a BlankPhrase that opens a popover holding the
 * blank's input, such as an AmountInput, or a ConditionPanel with `wide`.
 * The popover is named after the placeholder phrase. Enter or Space opens
 * it and moves focus inside, and Escape closes it with focus back on the
 * blank. `invalidMessage` underlines the blank and shows at the top of the
 * popover. `defaultOpen` opens the blank as it mounts, for the first blank
 * of a clause just added. A picker blank passes a BlankPhrase as the
 * picker's `trigger` instead, so only the picker's popover opens.
 */
export function Blank({
	placeholder,
	phrase,
	invalidMessage,
	defaultOpen = false,
	wide = false,
	children,
}: BlankProps): ReactElement {
	const invalidMessageId = useId();
	return (
		<Popover defaultOpen={defaultOpen}>
			<PopoverTrigger asChild aria-invalid={invalidMessage !== undefined}>
				<BlankPhrase placeholder={placeholder} phrase={phrase} />
			</PopoverTrigger>
			<PopoverContent
				align="start"
				aria-label={placeholder}
				aria-describedby={
					invalidMessage === undefined ? undefined : invalidMessageId
				}
				className={wide ? "w-160 max-w-[calc(100vw-2rem)]" : undefined}
			>
				{invalidMessage !== undefined && (
					<p id={invalidMessageId} className="text-destructive text-sm">
						{invalidMessage}
					</p>
				)}
				{children}
			</PopoverContent>
		</Popover>
	);
}
