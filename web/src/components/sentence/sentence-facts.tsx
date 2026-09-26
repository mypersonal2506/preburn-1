import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactElement } from "react";
import { Skeleton } from "@/components/ui/skeleton";

/**
 * The state of the query behind one fact, whose data is the fact's value
 * already formatted for display, such as a useQuery result with `select`.
 */
export type SentenceFactQuery = Pick<
	UseQueryResult<string>,
	"data" | "status" | "fetchStatus"
>;

/** One fact of a SentenceFacts row. */
export interface SentenceFact {
	label: string;
	query: SentenceFactQuery;
}

/** Props of SentenceFacts. */
export interface SentenceFactsProps {
	facts: readonly SentenceFact[];
}

/**
 * The facts row under a sentence, such as the customers a policy matches
 * now or the cost per request before and after. Each fact shows its query's
 * value, a skeleton while it first loads, "Unavailable" when it failed and
 * "-" while its query waits, such as for a complete draft. A fact keeps its
 * value while it refreshes and reports itself busy. Facts never hold up
 * saving: build their queries on useFactsDraft.
 */
export function SentenceFacts({ facts }: SentenceFactsProps): ReactElement {
	return (
		<dl className="flex flex-wrap gap-x-8 gap-y-2 text-sm">
			{facts.map((fact) => (
				<div key={fact.label} className="flex flex-col gap-0.5">
					<dt className="text-muted-foreground text-xs">{fact.label}</dt>
					<dd
						aria-busy={fact.query.fetchStatus === "fetching"}
						className="numeric"
					>
						<FactValue query={fact.query} />
					</dd>
				</div>
			))}
		</dl>
	);
}

function FactValue({ query }: { query: SentenceFactQuery }): ReactElement {
	if (query.status === "error") {
		return <>Unavailable</>;
	}
	if (query.data !== undefined) {
		return <>{query.data}</>;
	}
	if (query.fetchStatus === "fetching") {
		return <Skeleton className="h-5 w-16" />;
	}
	return <>-</>;
}
