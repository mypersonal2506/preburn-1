import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, test, vi } from "vitest";
import { LiveIndicator } from "@/components/live-indicator";
import type { EventStream, EventStreamStatus } from "@/lib/use-event-stream";

function eventStream(status: EventStreamStatus, newCount: number): EventStream {
	return { status, newCount, pause: vi.fn(), resume: vi.fn() };
}

test.each([
	["connecting", 0, "Connecting", "Pause"],
	["live", 0, "Live", "Pause"],
	["reconnecting", 0, "Reconnecting", "Pause"],
	["paused", 0, "Paused", "Resume"],
	["paused", 1234, "Paused, 1,234 new", "Resume"],
] as const)(
	"%s with %i buffered reads %s and offers %s",
	(status, newCount, label, action) => {
		render(<LiveIndicator stream={eventStream(status, newCount)} />);

		expect(screen.getByRole("status")).toHaveTextContent(label);
		expect(screen.getByRole("button", { name: action })).toBeInTheDocument();
	},
);

test("Pause pauses a live stream", async () => {
	const user = userEvent.setup();
	const stream = eventStream("live", 0);
	render(<LiveIndicator stream={stream} />);

	await user.click(screen.getByRole("button", { name: "Pause" }));

	expect(stream.pause).toHaveBeenCalledOnce();
	expect(stream.resume).not.toHaveBeenCalled();
});

test("Resume resumes a paused stream", async () => {
	const user = userEvent.setup();
	const stream = eventStream("paused", 3);
	render(<LiveIndicator stream={stream} />);

	await user.click(screen.getByRole("button", { name: "Resume" }));

	expect(stream.resume).toHaveBeenCalledOnce();
	expect(stream.pause).not.toHaveBeenCalled();
});
