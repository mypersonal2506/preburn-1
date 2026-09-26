import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { toApiProblem } from "@/lib/api-problem";
import type { CodeMessages } from "@/lib/form-problem";

interface ConfirmModalProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	title: string;
	description: string;
	confirmLabel: string;
	pending: boolean;
	error?: unknown;
	codeMessages?: CodeMessages;
	onConfirm: () => void;
}

/**
 * Asks the member to confirm a destructive action. The description states
 * the consequence. The confirm button uses the destructive variant and
 * stays disabled while pending. error takes the mutation's error, null or
 * undefined while there is none, and shows the codeMessages message for
 * its problem code, or else its problem detail.
 */
export function ConfirmModal({
	open,
	onOpenChange,
	title,
	description,
	confirmLabel,
	pending,
	error,
	codeMessages = {},
	onConfirm,
}: ConfirmModalProps) {
	const message = problemMessage(error, codeMessages);
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent>
				<DialogHeader>
					<DialogTitle>{title}</DialogTitle>
					<DialogDescription>{description}</DialogDescription>
				</DialogHeader>
				{message !== null && (
					<Alert variant="destructive">
						<AlertDescription className="first-letter:uppercase">
							{message}
						</AlertDescription>
					</Alert>
				)}
				<DialogFooter>
					<DialogClose asChild>
						<Button variant="outline">Cancel</Button>
					</DialogClose>
					<Button variant="destructive" disabled={pending} onClick={onConfirm}>
						{confirmLabel}
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}

function problemMessage(
	error: unknown,
	codeMessages: CodeMessages,
): string | null {
	if (error === null || error === undefined) {
		return null;
	}
	const problem = toApiProblem(error);
	const codeMessage = codeMessages[problem.code];
	return codeMessage === undefined ? problem.detail : codeMessage(problem);
}
