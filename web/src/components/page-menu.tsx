import { EllipsisIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import { CodePanel, type CodeSnippet } from "@/components/code-panel";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuSeparator,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { copyText } from "@/lib/clipboard";

interface ApiRequest {
	method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
	path: string;
	body?: unknown;
}

interface PageMenuProps {
	record?: unknown;
	apiRequest?: ApiRequest;
	children?: ReactNode;
}

type ShownCode = {
	title: string;
	snippet: CodeSnippet;
};

const API_KEY_PLACEHOLDER = "$PREBURN_API_KEY";
const JSON_INDENT = 2;
const SHELL_SINGLE_QUOTE_ESCAPE = "'\\''";

/**
 * The "More actions" menu of a page header. View JSON shows record in a
 * CodePanel modal. Copy as API request copies apiRequest, an admin API
 * request whose path starts with /api/v1 and whose body is sent as JSON, as
 * a curl command that reads the key from $PREBURN_API_KEY, and shows the
 * command in a modal when the clipboard refuses. Record actions go in
 * children as DropdownMenuItems after a separator.
 */
export function PageMenu({ record, apiRequest, children }: PageMenuProps) {
	const [shownCode, setShownCode] = useState<ShownCode | null>(null);

	async function copyApiRequest(request: ApiRequest): Promise<void> {
		const command = curlCommand(request);
		if (await copyText(command)) {
			toast.success("Request copied");
			return;
		}
		setShownCode({
			title: "API request",
			snippet: { label: "curl", code: command },
		});
	}

	return (
		<>
			<DropdownMenu>
				<DropdownMenuTrigger asChild>
					<Button variant="outline" size="icon" aria-label="More actions">
						<EllipsisIcon />
					</Button>
				</DropdownMenuTrigger>
				<DropdownMenuContent align="end" className="w-auto">
					{record !== undefined && (
						<DropdownMenuItem
							onSelect={() =>
								setShownCode({
									title: "JSON",
									snippet: {
										label: "JSON",
										code: JSON.stringify(record, null, JSON_INDENT),
									},
								})
							}
						>
							View JSON
						</DropdownMenuItem>
					)}
					{apiRequest !== undefined && (
						<DropdownMenuItem onSelect={() => copyApiRequest(apiRequest)}>
							Copy as API request
						</DropdownMenuItem>
					)}
					{children !== undefined && (
						<>
							<DropdownMenuSeparator />
							{children}
						</>
					)}
				</DropdownMenuContent>
			</DropdownMenu>
			<Dialog
				open={shownCode !== null}
				onOpenChange={(open) => {
					if (!open) {
						setShownCode(null);
					}
				}}
			>
				{shownCode !== null && (
					<DialogContent aria-describedby={undefined} className="sm:max-w-2xl">
						<DialogHeader>
							<DialogTitle>{shownCode.title}</DialogTitle>
						</DialogHeader>
						<CodePanel snippets={[shownCode.snippet]} />
					</DialogContent>
				)}
			</Dialog>
		</>
	);
}

function curlCommand(request: ApiRequest): string {
	const lines = [
		`curl -X ${request.method} '${window.location.origin}${request.path}'`,
		`  -H "Authorization: Bearer ${API_KEY_PLACEHOLDER}"`,
	];
	if (request.body !== undefined) {
		const json = JSON.stringify(request.body, null, JSON_INDENT);
		lines.push(
			"  -H 'Content-Type: application/json'",
			`  -d '${json.replaceAll("'", SHELL_SINGLE_QUOTE_ESCAPE)}'`,
		);
	}
	return lines.join(" \\\n");
}
