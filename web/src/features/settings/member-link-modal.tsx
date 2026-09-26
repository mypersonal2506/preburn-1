import type { ReactNode } from "react";
import type { MemberResponse } from "@/client";
import { CodePanel } from "@/components/code-panel";
import { FormProblemAlert } from "@/components/form/form-problem-alert";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogClose,
	DialogContent,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { type CodeMessages, mapProblemToForm } from "@/lib/form-problem";

interface MemberLinkModalProps {
	title: string;
	member: MemberResponse | null;
	linkUrl: string | undefined;
	error: unknown;
	onClose: () => void;
}

const LINK_CODE_MESSAGES: CodeMessages = {
	member_disabled: () => "This member was removed.",
};

/**
 * Shows the one-time invite or reset link of member while member is set:
 * who to send it to, and the link in a CodePanel with its copy button. A
 * skeleton stands in while linkUrl is undefined, and a non-null error shows
 * its problem instead of the link. The API returns a link only once, and
 * the caller drops it in onClose, so a closed modal never shows it again.
 */
export function MemberLinkModal({
	title,
	member,
	linkUrl,
	error,
	onClose,
}: MemberLinkModalProps) {
	function linkContent(displayName: string): ReactNode {
		if (error !== null) {
			return (
				<FormProblemAlert
					messages={mapProblemToForm(error, {}, LINK_CODE_MESSAGES).form}
				/>
			);
		}
		if (linkUrl === undefined) {
			return <Skeleton className="h-10 w-full" />;
		}
		return (
			<>
				<p className="text-sm text-muted-foreground">
					{`Send this link to ${displayName}. It works once and expires in 24 hours.`}
				</p>
				<CodePanel snippets={[{ label: "link", code: linkUrl }]} />
			</>
		);
	}

	return (
		<Dialog
			open={member !== null}
			onOpenChange={(open) => {
				if (!open) {
					onClose();
				}
			}}
		>
			{member !== null && (
				<DialogContent aria-describedby={undefined}>
					<DialogHeader>
						<DialogTitle>{title}</DialogTitle>
					</DialogHeader>
					{linkContent(member.display_name)}
					<DialogFooter>
						<DialogClose asChild>
							<Button>Done</Button>
						</DialogClose>
					</DialogFooter>
				</DialogContent>
			)}
		</Dialog>
	);
}
