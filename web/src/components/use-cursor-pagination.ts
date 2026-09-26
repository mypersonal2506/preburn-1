import { useState } from "react";
import { type Environment, useEnvironment } from "@/lib/environment-store";

/** The page a cursor-paginated list shows and the moves between pages. */
export interface CursorPagination {
	cursor: string | undefined;
	hasPrevious: boolean;
	goToNext: (nextCursor: string) => void;
	goToPrevious: () => void;
}

type CursorStack = {
	environment: Environment;
	filterKey: string;
	cursors: readonly string[];
};

/**
 * Keeps the stack of cursors that led to the shown page of an API list.
 * cursor is the query parameter of the shown page, undefined on the first
 * page. goToNext takes the next_cursor of the shown page, goToPrevious
 * returns to the page before. A cursor only fits the environment, filters
 * and sort it came from, so an environment switch returns to the first page,
 * and so does a new filterKey: pass the page's filters and sort as
 * filterKey.
 */
export function useCursorPagination(filterKey: string): CursorPagination {
	const environment = useEnvironment();
	const [stack, setStack] = useState<CursorStack>({
		environment,
		filterKey,
		cursors: [],
	});
	let cursors = stack.cursors;
	if (stack.environment !== environment || stack.filterKey !== filterKey) {
		cursors = [];
		setStack({ environment, filterKey, cursors });
	}

	return {
		cursor: cursors.at(-1),
		hasPrevious: cursors.length > 0,
		goToNext: (nextCursor) => {
			setStack({ environment, filterKey, cursors: [...cursors, nextCursor] });
		},
		goToPrevious: () => {
			setStack({ environment, filterKey, cursors: cursors.slice(0, -1) });
		},
	};
}
