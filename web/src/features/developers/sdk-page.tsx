import { getRouteApi } from "@tanstack/react-router";
import { CodePanel } from "@/components/code-panel";
import { PageHeader } from "@/components/page-header";
import { SectionCard } from "@/components/section-card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
	type SdkLanguage,
	sdkLanguageSchema,
} from "@/features/developers/sdk-search";
import {
	curlCheckSnippet,
	curlReportSnippet,
	PYTHON_INSTALL_SNIPPET,
	PYTHON_OPENAI_INSTALL_SNIPPET,
	pythonCheckAndReportSnippet,
	pythonOpenAiSnippet,
} from "@/features/developers/sdk-snippets";

const SDK_LANGUAGE_LABELS: Record<SdkLanguage, string> = {
	python: "Python",
	curl: "curl",
};

const sdkRoute = getRouteApi("/_app/developers/sdk");

export function SdkPage() {
	const { language } = sdkRoute.useSearch();
	const navigate = sdkRoute.useNavigate();
	const origin = window.location.origin;

	return (
		<Tabs
			value={language}
			onValueChange={(tab) =>
				navigate({ search: { language: sdkLanguageSchema.parse(tab) } })
			}
			className="gap-6"
		>
			<PageHeader
				title="SDK"
				tabs={
					<TabsList>
						{sdkLanguageSchema.options.map((sdkLanguage) => (
							<TabsTrigger key={sdkLanguage} value={sdkLanguage}>
								{SDK_LANGUAGE_LABELS[sdkLanguage]}
							</TabsTrigger>
						))}
					</TabsList>
				}
			/>
			<TabsContent value="python" className="flex flex-col gap-6">
				<SectionCard title="Install">
					<CodePanel snippets={[PYTHON_INSTALL_SNIPPET]} />
				</SectionCard>
				<SectionCard title="Check and report">
					<CodePanel snippets={[pythonCheckAndReportSnippet(origin)]} />
				</SectionCard>
				<SectionCard title="OpenAI wrapper">
					<div className="flex flex-col gap-3">
						<CodePanel snippets={[PYTHON_OPENAI_INSTALL_SNIPPET]} />
						<CodePanel snippets={[pythonOpenAiSnippet(origin)]} />
					</div>
				</SectionCard>
			</TabsContent>
			<TabsContent value="curl" className="flex flex-col gap-6">
				<SectionCard title="Check">
					<CodePanel snippets={[curlCheckSnippet(origin)]} />
				</SectionCard>
				<SectionCard title="Report">
					<CodePanel snippets={[curlReportSnippet(origin)]} />
				</SectionCard>
			</TabsContent>
		</Tabs>
	);
}
