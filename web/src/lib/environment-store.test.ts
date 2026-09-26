import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

type EnvironmentStoreModule = typeof import("@/lib/environment-store");

const storageKey = "preburn.environment";

async function loadEnvironmentStore(): Promise<EnvironmentStoreModule> {
	vi.resetModules();
	return import("@/lib/environment-store");
}

function throwStorageDenied(): never {
	throw new DOMException("storage denied", "SecurityError");
}

beforeEach(() => {
	window.localStorage.clear();
});

afterEach(() => {
	vi.restoreAllMocks();
});

test("defaults to test when nothing is stored", async () => {
	const store = await loadEnvironmentStore();

	expect(store.getEnvironment()).toBe("test");
});

test("ignores a stored value that is not an environment", async () => {
	window.localStorage.setItem(storageKey, "staging");

	const store = await loadEnvironmentStore();

	expect(store.getEnvironment()).toBe("test");
});

test("survives a reload", async () => {
	const firstLoad = await loadEnvironmentStore();
	firstLoad.setEnvironment("live");

	const secondLoad = await loadEnvironmentStore();

	expect(secondLoad.getEnvironment()).toBe("live");
});

test("falls back to test when reading storage throws", async () => {
	window.localStorage.setItem(storageKey, "live");
	vi.spyOn(Storage.prototype, "getItem").mockImplementation(throwStorageDenied);

	const store = await loadEnvironmentStore();

	expect(store.getEnvironment()).toBe("test");
});

test("switches in memory when writing storage throws", async () => {
	vi.spyOn(Storage.prototype, "setItem").mockImplementation(throwStorageDenied);
	const store = await loadEnvironmentStore();

	store.setEnvironment("live");

	expect(store.getEnvironment()).toBe("live");
});

test("notifies subscribers of a switch and not of the same environment", async () => {
	const store = await loadEnvironmentStore();
	const listener = vi.fn();
	const unsubscribe = store.subscribeEnvironment(listener);

	store.setEnvironment("test");
	store.setEnvironment("live");
	unsubscribe();
	store.setEnvironment("test");

	expect(listener).toHaveBeenCalledTimes(1);
});

test("the hook renders the current environment and follows switches", async () => {
	const store = await loadEnvironmentStore();
	const { result } = renderHook(() => store.useEnvironment());

	expect(result.current).toBe("test");

	act(() => {
		store.setEnvironment("live");
	});

	expect(result.current).toBe("live");
});
