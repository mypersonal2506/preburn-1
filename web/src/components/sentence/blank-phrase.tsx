import { cn } from "cn";
import type { ComponentProps, ReactElement } from "react";

/**
 * Props of BlankPhrase. Every other button prop, such as those a popover
 * trigger adds, passes on to the button.
 */
export interface BlankPhraseProps extends ComponentProps<"button"> {
	placeholder: string;
	phrase: string | null;
}

/**
 * The button of a sentence blank, styled as a highlighted phrase: the
 * phrase, or while the blank is empty the placeholder phrase in muted text,
 * such as "which plan". `aria-invalid` underlines it in the destructive
 * color. It passes its other props and ref on to the button, so it works as
 * a Blank's popover trigger, as a picker's `trigger`, or alone as a toggle
 * such as "and" or "or".
 */
export function BlankPhrase({
	placeholder,
	phrase,
	...buttonProps
}: BlankPhraseProps): ReactElement {
	return (
		<button
			{...buttonProps}
			type="button"
			data-empty={phrase === null}
			className={cn(
				"rounded-sm bg-accent px-1 font-medium text-accent-foreground",
				"underline decoration-2 underline-offset-4",
				"decoration-transparent aria-invalid:decoration-destructive",
				"outline-none transition-colors hover:bg-muted",
				"aria-expanded:bg-muted",
				"focus-visible:ring-3 focus-visible:ring-ring/50",
				"data-[empty=true]:font-normal",
				"data-[empty=true]:text-muted-foreground",
			)}
		>
			{phrase ?? placeholder}
		</button>
	);
}
