import { type ReactElement, type ReactNode, useId } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Popover,
	PopoverContent,
	PopoverTrigger,
} from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";

/**
 * The primary action of a SentenceCard: creating the record under `label`,
 * or saving edits, which shows Save and Discard only while `dirty`.
 */
export type SentenceCardAction =
	| { kind: "create"; label: string }
	| { kind: "edit"; dirty: boolean; onDiscard: () => void };

/**
 * The name input in the foot of a SentenceCard: its value, a callback for
 * each change, and whether a problem marks it invalid.
 */
export interface SentenceCardName {
	value: string;
	onChange: (name: string) => void;
	invalid: boolean;
}

/** The status switch of a SentenceCard, on while the record is active. */
export interface SentenceCardStatus {
	active: boolean;
	onActiveChange: (active: boolean) => void;
}

/**
 * The Advanced settings of a SentenceCard: a one-line summary, such as
 * "Soft, allow if unreachable or unpriced", and the settings it opens.
 */
export interface SentenceCardAdvanced {
	summary: string;
	content: ReactNode;
}

/** Props of SentenceCard. */
export interface SentenceCardProps {
	sentence: ReactNode;
	facts: ReactNode;
	errors: readonly string[];
	name?: SentenceCardName;
	status?: SentenceCardStatus;
	advanced?: SentenceCardAdvanced;
	action: SentenceCardAction;
	pending: boolean;
	onSubmit: () => void;
}

/**
 * The card a sentence builder page edits a record in: the sentence, text
 * with inline Blank buttons, then the facts row and the form's error list,
 * each message once. The foot holds the optional name input, for a
 * sentence without its own name blank, the optional status switch, the
 * optional Advanced trigger with its summary, and the primary action. A create card always shows its action, an edit card shows Save
 * and Discard only while dirty. The primary action is disabled only while
 * `pending`, so loading facts never hold up saving. Submitting, by button
 * or Enter in the name input, calls onSubmit unless an edit is clean or a
 * save is pending.
 */
export function SentenceCard({
	sentence,
	facts,
	errors,
	name,
	status,
	advanced,
	action,
	pending,
	onSubmit,
}: SentenceCardProps): ReactElement {
	const statusId = useId();
	const canSubmit = action.kind === "create" || action.dirty;
	const errorMessages = [...new Set(errors)];

	return (
		<Card>
			<form
				className="flex flex-col gap-4"
				onSubmit={(event) => {
					event.preventDefault();
					if (canSubmit && !pending) {
						onSubmit();
					}
				}}
			>
				<CardContent className="flex flex-col gap-4">
					<p className="text-lg leading-10">{sentence}</p>
					{facts}
					{errorMessages.length > 0 && (
						<Alert variant="destructive">
							<AlertDescription>
								<ul className="list-disc pl-4">
									{errorMessages.map((message) => (
										<li key={message}>{message}</li>
									))}
								</ul>
							</AlertDescription>
						</Alert>
					)}
				</CardContent>
				<CardFooter className="flex flex-wrap gap-3">
					{name !== undefined && (
						<Input
							aria-label="Name"
							placeholder="Name"
							aria-invalid={name.invalid}
							className="w-64"
							value={name.value}
							onChange={(event) => name.onChange(event.target.value)}
						/>
					)}
					{status !== undefined && (
						<div className="flex items-center gap-2">
							<Switch
								id={statusId}
								checked={status.active}
								onCheckedChange={status.onActiveChange}
							/>
							<Label htmlFor={statusId}>Active</Label>
						</div>
					)}
					{advanced !== undefined && (
						<Popover>
							<PopoverTrigger asChild>
								<Button type="button" variant="ghost" className="font-normal">
									<span className="font-medium">Advanced</span>
									<span className="text-muted-foreground">
										{advanced.summary}
									</span>
								</Button>
							</PopoverTrigger>
							<PopoverContent
								align="start"
								aria-label="Advanced"
								className="w-80"
							>
								{advanced.content}
							</PopoverContent>
						</Popover>
					)}
					<div className="ml-auto flex gap-2">
						{action.kind === "create" && (
							<Button type="submit" disabled={pending}>
								{action.label}
							</Button>
						)}
						{action.kind === "edit" && action.dirty && (
							<>
								<Button
									type="button"
									variant="outline"
									onClick={action.onDiscard}
								>
									Discard
								</Button>
								<Button type="submit" disabled={pending}>
									Save
								</Button>
							</>
						)}
					</div>
				</CardFooter>
			</form>
		</Card>
	);
}
