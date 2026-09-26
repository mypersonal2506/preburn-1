import { PlusIcon } from "lucide-react";
import { type ReactElement, useRef } from "react";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/**
 * An optional clause of a sentence. `onAdd` adds the clause to the form
 * values, and the page renders its first blank with `defaultOpen`.
 */
export interface SentenceClause {
	label: string;
	onAdd: () => void;
}

/** Props of AddClause. */
export interface AddClauseProps {
	clauses: readonly SentenceClause[];
}

/**
 * The (+) button of a sentence, a menu of the optional clauses the
 * sentence does not have yet. Choosing one adds it, and focus stays in the
 * clause's first blank, which opens as it mounts. Escape closes the menu
 * with focus back on the button. Renders nothing when no clause is left.
 */
export function AddClause({ clauses }: AddClauseProps): ReactElement | null {
	const clauseAdded = useRef(false);

	if (clauses.length === 0) {
		return null;
	}

	return (
		<DropdownMenu
			onOpenChange={(open) => {
				if (open) {
					clauseAdded.current = false;
				}
			}}
		>
			<DropdownMenuTrigger asChild>
				<Button
					type="button"
					variant="ghost"
					size="icon-xs"
					aria-label="Add clause"
				>
					<PlusIcon />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent
				align="start"
				className="w-auto"
				onCloseAutoFocus={(event) => {
					// Focus returning to this button would leave the added clause's
					// open blank, which closes it.
					if (clauseAdded.current) {
						event.preventDefault();
					}
				}}
			>
				{clauses.map((clause) => (
					<DropdownMenuItem
						key={clause.label}
						onSelect={() => {
							clauseAdded.current = true;
							clause.onAdd();
						}}
					>
						{clause.label}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
