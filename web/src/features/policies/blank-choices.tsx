import type { ReactElement } from "react";
import { Button } from "@/components/ui/button";

/** One choice of a BlankChoices list, with an optional muted description. */
export interface BlankChoice<Value extends string> {
	value: Value;
	label: string;
	description?: string;
}

interface BlankChoicesProps<Value extends string> {
	label: string;
	choices: readonly BlankChoice<Value>[];
	value: Value | null;
	onChoose: (value: Value) => void;
}

/**
 * The choices inside a sentence blank's popover, such as the outcomes, as a
 * list of buttons named label. The chosen one is pressed, and none while
 * value is null. Choosing runs on click or Enter only, so moving through
 * the list never changes the sentence.
 */
export function BlankChoices<Value extends string>({
	label,
	choices,
	value,
	onChoose,
}: BlankChoicesProps<Value>): ReactElement {
	return (
		<fieldset aria-label={label} className="flex flex-col gap-0.5">
			{choices.map((choice) => (
				<Button
					key={choice.value}
					type="button"
					variant="ghost"
					size="sm"
					aria-pressed={choice.value === value}
					className="justify-between gap-4 aria-pressed:bg-muted"
					onClick={() => onChoose(choice.value)}
				>
					{choice.label}
					{choice.description !== undefined && (
						<span className="font-normal text-muted-foreground">
							{choice.description}
						</span>
					)}
				</Button>
			))}
		</fieldset>
	);
}
