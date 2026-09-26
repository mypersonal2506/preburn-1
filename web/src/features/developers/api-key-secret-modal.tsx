import { useId, useState } from "react";
import { CodePanel } from "@/components/code-panel";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";

interface ApiKeySecretModalProps {
	secret: string;
	onDone: () => void;
}

/**
 * Shows the secret of a new API key, the only time the API returns it, with
 * a copy button. Escape and outside clicks keep it open until the member
 * checks "I stored the key". Done then calls onDone, which stops rendering
 * the modal, so every new secret starts unchecked.
 */
export function ApiKeySecretModal({ secret, onDone }: ApiKeySecretModalProps) {
	const [stored, setStored] = useState(false);
	const storedCheckboxId = useId();

	return (
		<Dialog
			open
			onOpenChange={(open) => {
				if (!open && stored) {
					onDone();
				}
			}}
		>
			<DialogContent showCloseButton={false}>
				<DialogHeader>
					<DialogTitle>Store your API key</DialogTitle>
					<DialogDescription>
						This is the only time it is shown.
					</DialogDescription>
				</DialogHeader>
				<CodePanel snippets={[{ label: "API key", code: secret }]} />
				<Field orientation="horizontal">
					<Checkbox
						id={storedCheckboxId}
						checked={stored}
						onCheckedChange={(checked) => setStored(checked === true)}
					/>
					<FieldLabel htmlFor={storedCheckboxId}>I stored the key</FieldLabel>
				</Field>
				<DialogFooter>
					<Button disabled={!stored} onClick={onDone}>
						Done
					</Button>
				</DialogFooter>
			</DialogContent>
		</Dialog>
	);
}
