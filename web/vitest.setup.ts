import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, vi } from "vitest";

class PageRelativeRequest extends Request {
	constructor(input: RequestInfo | URL, init?: RequestInit) {
		super(
			typeof input === "string" ? new URL(input, document.baseURI) : input,
			init,
		);
	}
}

class SilentEventSource extends EventTarget {
	close(): void {}
}

class InertResizeObserver {
	observe(): void {}
	unobserve(): void {}
	disconnect(): void {}
}

function matchNoMedia(query: string): MediaQueryList {
	return Object.assign(new EventTarget(), {
		matches: false,
		media: query,
		onchange: null,
		addListener: vi.fn(),
		removeListener: vi.fn(),
	});
}

globalThis.Request = PageRelativeRequest;
globalThis.ResizeObserver = InertResizeObserver;
Object.assign(globalThis, { EventSource: SilentEventSource });
window.matchMedia = matchNoMedia;
window.scrollTo = vi.fn();
Element.prototype.scrollIntoView = vi.fn();

afterEach(cleanup);
