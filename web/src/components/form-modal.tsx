import type { ReactNode } from "react";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { FieldGroup } from "@/components/ui/field";

interface FormModalProps {
	open: boolean;
	onOpenChange: (open: boolean) => void;
	title: string;
	submitLabel: string;
	pending: boolean;
	rootError?: string;
	onSubmit: () => void;
	children: ReactNode;
}

/**
 * A modal form for a small record. The fields go in children. Submitting,
 * by button or Enter, calls onSubmit without the browser's own validation,
 * so the form's rules and the server's show their messages, and the submit
 * button stays disabled while pending. rootError shows above the buttons for problems that belong
 * to no field.
 */
export function FormModal({
	open,
	onOpenChange,
	title,
	submitLabel,
	pending,
	rootError,
	onSubmit,
	children,
}: FormModalProps) {
	return (
		<Dialog open={open} onOpenChange={onOpenChange}>
			<DialogContent aria-describedby={undefined}>
				<form
					noValidate
					className="flex flex-col gap-4"
					onSubmit={(event) => {
						event.preventDefault();
						onSubmit();
					}}
				>
					<DialogHeader>
						<DialogTitle>{title}</DialogTitle>
					</DialogHeader>
					<FieldGroup>{children}</FieldGroup>
					{rootError !== undefined && (
						<Alert variant="destructive">
							<AlertDescription>{rootError}</AlertDescription>
						</Alert>
					)}
					<DialogFooter>
						<DialogClose asChild>
							<Button type="button" variant="outline">
								Cancel
							</Button>
						</DialogClose>
						<Button type="submit" disabled={pending}>
							{submitLabel}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
