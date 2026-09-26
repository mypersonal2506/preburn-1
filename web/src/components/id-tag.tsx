import { CopyIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { copyText } from "@/lib/clipboard";

interface IdTagProps {
	id: string;
}

const ID_START_CHARACTERS = 12;
const ID_END_CHARACTERS = 6;
const TRUNCATION_MARK = "...";

/**
 * An id in mono type with a copy button, for detail pages. Ids longer than
 * 21 characters show their first 12 and last 6 characters with "..."
 * between. Screen readers and the copy button get the whole id.
 */
export function IdTag({ id }: IdTagProps) {
	async function copyId(): Promise<void> {
		if (await copyText(id)) {
			toast.success("Id copied");
		} else {
			toast.error("Copy failed");
		}
	}

	return (
		<span className="inline-flex items-center gap-1 font-mono text-xs text-muted-foreground">
			<span aria-hidden title={id}>
				{middleTruncated(id)}
			</span>
			<span className="sr-only">{id}</span>
			<Button
				variant="ghost"
				size="icon-xs"
				aria-label="Copy id"
				onClick={copyId}
			>
				<CopyIcon />
			</Button>
		</span>
	);
}

function middleTruncated(id: string): string {
	if (
		id.length <=
		ID_START_CHARACTERS + TRUNCATION_MARK.length + ID_END_CHARACTERS
	) {
		return id;
	}
	return `${id.slice(0, ID_START_CHARACTERS)}${TRUNCATION_MARK}${id.slice(-ID_END_CHARACTERS)}`;
}
