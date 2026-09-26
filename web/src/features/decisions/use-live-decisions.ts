import { type QueryKey, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import {
	type DecisionFilters,
	matchesDecisionFilters,
} from "@/features/decisions/decision-filters";
import { type EventStream, useEventStream } from "@/lib/use-event-stream";

type RefreshState = {
	timer: ReturnType<typeof setTimeout> | undefined;
	running: boolean;
	requested: boolean;
};

const LIVE_REFRESH_DELAY_MILLISECONDS = 1_000;

/**
 * Streams the decisions of the environment that the list's filters keep,
 * for the decisions list, and keeps its first page current. A streamed
 * decision, while the first page shows, refetches the list query of
 * listQueryKey 1 second later, so a burst of decisions costs one request.
 * Decisions that arrive during that refetch schedule one more. Decisions
 * that arrive while the stream is paused wait in its buffer and count as
 * new, and resuming delivers them, so the list refetches once after resume.
 */
export function useLiveDecisions(
	listQueryKey: QueryKey,
	filters: DecisionFilters,
	firstPage: boolean,
): EventStream {
	const queryClient = useQueryClient();
	const listQueryKeyRef = useRef(listQueryKey);
	const refreshRef = useRef<RefreshState>({
		timer: undefined,
		running: false,
		requested: false,
	});

	useEffect(() => {
		listQueryKeyRef.current = listQueryKey;
	});

	useEffect(() => {
		const refresh = refreshRef.current;
		return () => clearTimeout(refresh.timer);
	}, []);

	function requestRefresh(): void {
		const refresh = refreshRef.current;
		refresh.requested = true;
		if (refresh.running || refresh.timer !== undefined) {
			return;
		}
		refresh.timer = setTimeout(
			() => void runRefresh(),
			LIVE_REFRESH_DELAY_MILLISECONDS,
		);
	}

	async function runRefresh(): Promise<void> {
		const refresh = refreshRef.current;
		refresh.timer = undefined;
		refresh.requested = false;
		refresh.running = true;
		await queryClient.refetchQueries({
			queryKey: listQueryKeyRef.current,
			exact: true,
			type: "active",
		});
		refresh.running = false;
		if (refresh.requested) {
			requestRefresh();
		}
	}

	return useEventStream(
		() => {
			if (firstPage) {
				requestRefresh();
			}
		},
		(decision) => matchesDecisionFilters(decision, filters),
	);
}
