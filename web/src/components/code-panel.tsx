import { CopyIcon } from "lucide-react";
import { useRef } from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { copyText } from "@/lib/clipboard";

/** A piece of code and the tab label that names its language or format. */
export interface CodeSnippet {
	label: string;
	code: string;
}

interface CodePanelProps {
	snippets: readonly [CodeSnippet, ...CodeSnippet[]];
}

interface CodeBlockProps {
	snippet: CodeSnippet;
}

/**
 * The only code and JSON display of the dashboard. One snippet shows alone,
 * several show as tabs named by their labels. Each snippet has a copy
 * button labelled "Copy {label}". When the clipboard refuses, the code is
 * selected so the member can copy it by hand.
 */
export function CodePanel({ snippets }: CodePanelProps) {
	const [firstSnippet] = snippets;
	if (snippets.length === 1) {
		return <CodeBlock snippet={firstSnippet} />;
	}
	return (
		<Tabs defaultValue={firstSnippet.label}>
			<TabsList>
				{snippets.map((snippet) => (
					<TabsTrigger key={snippet.label} value={snippet.label}>
						{snippet.label}
					</TabsTrigger>
				))}
			</TabsList>
			{snippets.map((snippet) => (
				<TabsContent key={snippet.label} value={snippet.label}>
					<CodeBlock snippet={snippet} />
				</TabsContent>
			))}
		</Tabs>
	);
}

function CodeBlock({ snippet }: CodeBlockProps) {
	const codeRef = useRef<HTMLPreElement>(null);

	async function copyCode(): Promise<void> {
		if (await copyText(snippet.code)) {
			toast.success("Copied");
			return;
		}
		if (codeRef.current !== null) {
			window.getSelection()?.selectAllChildren(codeRef.current);
		}
		toast.error("Copy failed, text selected");
	}

	return (
		<div className="relative rounded-lg bg-muted">
			<pre
				ref={codeRef}
				className="overflow-x-auto p-3 pr-10 font-mono text-xs leading-relaxed"
			>
				{snippet.code}
			</pre>
			<Button
				variant="ghost"
				size="icon-xs"
				aria-label={`Copy ${snippet.label}`}
				className="absolute top-2 right-2"
				onClick={copyCode}
			>
				<CopyIcon />
			</Button>
		</div>
	);
}
