import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { setEnvironment } from "@/lib/environment-store";

afterEach(() => {
	setEnvironment("test");
	window.localStorage.clear();
});

test("an environment switch returns to the first page", () => {
	const { result } = renderHook(() => useCursorPagination("all"));
	act(() => result.current.goToNext("test-page-2"));
	act(() => result.current.goToNext("test-page-3"));
	expect(result.current.cursor).toBe("test-page-3");

	act(() => setEnvironment("live"));

	expect(result.current.cursor).toBeUndefined();
	expect(result.current.hasPrevious).toBe(false);
});

test("pages moved to after a switch stay in the new environment", () => {
	const { result } = renderHook(() => useCursorPagination("all"));
	act(() => result.current.goToNext("test-page-2"));
	act(() => setEnvironment("live"));

	act(() => result.current.goToNext("live-page-2"));
	act(() => result.current.goToNext("live-page-3"));
	act(() => result.current.goToPrevious());

	expect(result.current.cursor).toBe("live-page-2");
	expect(result.current.hasPrevious).toBe(true);
});
