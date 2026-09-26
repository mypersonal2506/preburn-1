import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

type ThemeModule = typeof import("@/lib/theme");

type SchemeListener = () => void;

const storageKey = "preburn.theme";

let systemPrefersDark = false;
const schemeListeners = new Set<SchemeListener>();

function fakeMatchMedia(query: string) {
	return {
		media: query,
		get matches() {
			return systemPrefersDark;
		},
		addEventListener: (_type: string, listener: SchemeListener) => {
			schemeListeners.add(listener);
		},
		removeEventListener: (_type: string, listener: SchemeListener) => {
			schemeListeners.delete(listener);
		},
	};
}

function changeSystemScheme(prefersDark: boolean): void {
	systemPrefersDark = prefersDark;
	for (const listener of schemeListeners) {
		listener();
	}
}

async function loadTheme(): Promise<ThemeModule> {
	vi.resetModules();
	return import("@/lib/theme");
}

function documentIsDark(): boolean {
	return document.documentElement.classList.contains("dark");
}

function throwStorageDenied(): never {
	throw new DOMException("storage denied", "SecurityError");
}

beforeEach(() => {
	window.localStorage.clear();
	document.documentElement.classList.remove("dark");
	systemPrefersDark = false;
	schemeListeners.clear();
	vi.stubGlobal("matchMedia", fakeMatchMedia);
});

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
});

test("defaults to system when nothing is stored", async () => {
	const theme = await loadTheme();

	expect(theme.getThemeChoice()).toBe("system");
});

test("ignores a stored value that is not a choice", async () => {
	window.localStorage.setItem(storageKey, "sepia");

	const theme = await loadTheme();

	expect(theme.getThemeChoice()).toBe("system");
});

test("the choice survives a reload", async () => {
	const firstLoad = await loadTheme();
	firstLoad.setThemeChoice("dark");

	const secondLoad = await loadTheme();

	expect(secondLoad.getThemeChoice()).toBe("dark");
});

test("falls back to system when reading storage throws", async () => {
	window.localStorage.setItem(storageKey, "dark");
	vi.spyOn(Storage.prototype, "getItem").mockImplementation(throwStorageDenied);

	const theme = await loadTheme();

	expect(theme.getThemeChoice()).toBe("system");
});

test("applies a choice in memory when writing storage throws", async () => {
	vi.spyOn(Storage.prototype, "setItem").mockImplementation(throwStorageDenied);
	const theme = await loadTheme();

	theme.setThemeChoice("dark");

	expect(theme.getThemeChoice()).toBe("dark");
	expect(documentIsDark()).toBe(true);
});

test("dark and light set and clear the dark class", async () => {
	const theme = await loadTheme();

	theme.setThemeChoice("dark");
	expect(documentIsDark()).toBe(true);

	theme.setThemeChoice("light");
	expect(documentIsDark()).toBe(false);
});

test("starting applies the stored choice", async () => {
	window.localStorage.setItem(storageKey, "dark");
	const theme = await loadTheme();

	theme.startTheme();

	expect(documentIsDark()).toBe(true);
});

test("system follows the color scheme until stopped", async () => {
	systemPrefersDark = true;
	const theme = await loadTheme();

	const stopTheme = theme.startTheme();
	expect(documentIsDark()).toBe(true);

	changeSystemScheme(false);
	expect(documentIsDark()).toBe(false);

	changeSystemScheme(true);
	expect(documentIsDark()).toBe(true);

	stopTheme();
	changeSystemScheme(false);
	expect(documentIsDark()).toBe(true);
});

test("an explicit choice ignores the color scheme", async () => {
	const theme = await loadTheme();
	theme.startTheme();

	theme.setThemeChoice("light");
	changeSystemScheme(true);

	expect(documentIsDark()).toBe(false);
});

test("notifies subscribers of a change and not of the same choice", async () => {
	const theme = await loadTheme();
	const listener = vi.fn();
	const unsubscribe = theme.subscribeThemeChoice(listener);

	theme.setThemeChoice("system");
	theme.setThemeChoice("dark");
	unsubscribe();
	theme.setThemeChoice("light");

	expect(listener).toHaveBeenCalledTimes(1);
});

test("the hook renders the choice and follows changes", async () => {
	const theme = await loadTheme();
	const { result } = renderHook(() => theme.useThemeChoice());

	expect(result.current).toBe("system");

	act(() => {
		theme.setThemeChoice("dark");
	});

	expect(result.current).toBe("dark");
});
