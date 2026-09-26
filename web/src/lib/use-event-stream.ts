import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import type {
	StreamDashboardDecisionsData,
	StreamDashboardDecisionsResponse,
} from "@/client";
import { getCurrentMemberOptions } from "@/client/@tanstack/react-query.gen";
import { client } from "@/client/client.gen";
import { zStreamDashboardDecisionsResponse } from "@/client/zod.gen";
import { isAuthenticationRequired } from "@/lib/api-problem";
import { type Environment, useEnvironment } from "@/lib/environment-store";

/** A decision summary the decision stream sends as the check decides. */
export type DecisionEvent = StreamDashboardDecisionsResponse["data"];

/**
 * Where a decision stream is: connecting for the first time, live, reconnecting
 * after the connection dropped, or paused by the member.
 */
export type EventStreamStatus =
	| "connecting"
	| "live"
	| "reconnecting"
	| "paused";

/**
 * The state and controls of a decision stream. newCount counts every kept
 * decision that arrived while paused, including those the buffer dropped.
 */
export interface EventStream {
	status: EventStreamStatus;
	newCount: number;
	pause: () => void;
	resume: () => void;
}

type ConnectionStatus = Exclude<EventStreamStatus, "paused">;

const DECISION_STREAM_PATH = "/api/v1/dashboard/decisions/stream";
const DECISION_EVENT_NAME = "decision";
const RECONNECT_DELAY_INITIAL_MILLISECONDS = 1_000;
const RECONNECT_DELAY_MAXIMUM_MILLISECONDS = 30_000;

/**
 * The most decisions a paused stream keeps for resume. Past it the oldest
 * kept decision is dropped, so resume delivers the newest ones.
 */
export const PAUSED_DECISIONS_MAXIMUM = 1_000;

/**
 * Streams the decisions of the current environment through `EventSource` and
 * passes each one keepsDecision accepts to onDecision in arrival order. The
 * others are dropped, so they never reach the paused buffer or its count.
 * `EventSource` cannot send headers, so the environment and the last
 * received event id travel as the query parameters `environment` and
 * `last_event_id`. After a dropped
 * connection it reconnects from the last event id, waiting 1 second and
 * doubling the wait after each failure up to 30 seconds. `EventSource` hides
 * the status of a failed request, so each failure also loads the current
 * member through the query client: an ended session answers
 * `authentication_required`, whose handler opens the login page, and the
 * stream stops reconnecting. While paused, the connection stays open and new
 * decisions wait in a buffer that resume delivers, holding at most the
 * newest PAUSED_DECISIONS_MAXIMUM. An environment switch empties the buffer
 * and starts over on the other environment's stream.
 */
export function useEventStream(
	onDecision: (decision: DecisionEvent) => void,
	keepsDecision: (decision: DecisionEvent) => boolean = keepsEveryDecision,
): EventStream {
	const environment = useEnvironment();
	const queryClient = useQueryClient();
	const [connectionStatus, setConnectionStatus] =
		useState<ConnectionStatus>("connecting");
	const [paused, setPaused] = useState(false);
	const [newCount, setNewCount] = useState(0);
	const onDecisionRef = useRef(onDecision);
	const keepsDecisionRef = useRef(keepsDecision);
	const pausedRef = useRef(false);
	const bufferRef = useRef<DecisionEvent[]>([]);

	useEffect(() => {
		onDecisionRef.current = onDecision;
		keepsDecisionRef.current = keepsDecision;
	});

	useEffect(() => {
		let lastEventId: string | undefined;
		let failedAttempts = 0;
		let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
		bufferRef.current = [];
		setNewCount(0);
		setConnectionStatus("connecting");
		let source = connect();

		function connect(): EventSource {
			const opened = new EventSource(
				decisionStreamUrl(environment, lastEventId),
			);
			opened.addEventListener("open", () => {
				setConnectionStatus("live");
			});
			opened.addEventListener("error", () => {
				// Closing stops the browser's own reconnect, which has no backoff.
				opened.close();
				setConnectionStatus("reconnecting");
				reconnectTimer = setTimeout(() => {
					source = connect();
				}, reconnectDelay(failedAttempts));
				failedAttempts += 1;
				void checkSession();
			});
			opened.addEventListener(
				DECISION_EVENT_NAME,
				(event: MessageEvent<string>) => {
					lastEventId = event.lastEventId;
					// Reset here, not on open: a stream that opens and fails at once, as
					// when Redis is down, would otherwise reconnect every second.
					failedAttempts = 0;
					const decision = zStreamDashboardDecisionsResponse.shape.data.parse(
						JSON.parse(event.data),
					);
					if (!keepsDecisionRef.current(decision)) {
						return;
					}
					if (pausedRef.current) {
						bufferRef.current.push(decision);
						if (bufferRef.current.length > PAUSED_DECISIONS_MAXIMUM) {
							bufferRef.current.shift();
						}
						setNewCount((count) => count + 1);
						return;
					}
					onDecisionRef.current(decision);
				},
			);
			return opened;
		}

		async function checkSession(): Promise<void> {
			try {
				await queryClient.fetchQuery({
					...getCurrentMemberOptions(),
					staleTime: 0,
				});
			} catch (error) {
				if (isAuthenticationRequired(error)) {
					clearTimeout(reconnectTimer);
					source.close();
				}
			}
		}

		return () => {
			clearTimeout(reconnectTimer);
			source.close();
		};
	}, [environment, queryClient]);

	function pause(): void {
		pausedRef.current = true;
		setPaused(true);
	}

	function resume(): void {
		pausedRef.current = false;
		setPaused(false);
		const buffered = bufferRef.current;
		bufferRef.current = [];
		setNewCount(0);
		for (const decision of buffered) {
			onDecisionRef.current(decision);
		}
	}

	return {
		status: paused ? "paused" : connectionStatus,
		newCount,
		pause,
		resume,
	};
}

function keepsEveryDecision(): boolean {
	return true;
}

function decisionStreamUrl(
	environment: Environment,
	lastEventId: string | undefined,
): string {
	return client.buildUrl<StreamDashboardDecisionsData>({
		url: DECISION_STREAM_PATH,
		query: { environment, last_event_id: lastEventId },
	});
}

function reconnectDelay(failedAttempts: number): number {
	return Math.min(
		RECONNECT_DELAY_INITIAL_MILLISECONDS * 2 ** failedAttempts,
		RECONNECT_DELAY_MAXIMUM_MILLISECONDS,
	);
}
