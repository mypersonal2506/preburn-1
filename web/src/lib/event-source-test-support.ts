import type { DecisionEvent } from "@/lib/use-event-stream";

/**
 * An EventSource stand-in for tests that records every stream the code
 * opens with its URL and whether it was closed, and dispatches the events a
 * test sends. Install it with `vi.stubGlobal("EventSource",
 * FakeEventSource)` after emptying `FakeEventSource.opened`.
 */
export class FakeEventSource extends EventTarget {
	/** Every stream opened since the test emptied the list, oldest first. */
	static opened: FakeEventSource[] = [];

	/** The URL the stream was opened with. */
	readonly url: string;

	/** True once the code closed the stream. */
	closed = false;

	constructor(url: string) {
		super();
		this.url = url;
		FakeEventSource.opened.push(this);
	}

	/** Records that the code closed the stream. */
	close(): void {
		this.closed = true;
	}

	/** Dispatches the open event of a connected stream. */
	open(): void {
		this.dispatchEvent(new Event("open"));
	}

	/** Dispatches the error event of a failed or lost connection. */
	fail(): void {
		this.dispatchEvent(new Event("error"));
	}

	/** Dispatches decision as a `decision` event with the stream id streamId. */
	sendDecision(streamId: string, decision: DecisionEvent): void {
		this.dispatchEvent(
			new MessageEvent("decision", {
				data: JSON.stringify(decision),
				lastEventId: streamId,
			}),
		);
	}
}

/** The stream the code opened last. Throws when it opened none. */
export function latestEventSource(): FakeEventSource {
	const source = FakeEventSource.opened.at(-1);
	if (source === undefined) {
		throw new Error("event source missing");
	}
	return source;
}
