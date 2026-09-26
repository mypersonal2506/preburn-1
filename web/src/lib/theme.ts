import { useSyncExternalStore } from "react";

/** The theme a member picks: Light, Dark, or System to follow the device. */
export type ThemeChoice = "light" | "dark" | "system";

type ThemeListener = () => void;

const THEME_STORAGE_KEY = "preburn.theme";
const DEFAULT_THEME_CHOICE: ThemeChoice = "system";
const THEME_CHOICES: readonly ThemeChoice[] = ["light", "dark", "system"];
const DARK_CLASS_NAME = "dark";
const DARK_COLOR_SCHEME_QUERY = "(prefers-color-scheme: dark)";

const listeners = new Set<ThemeListener>();
let currentChoice = readStoredChoice();

/** Returns the theme choice, read from local storage at startup. */
export function getThemeChoice(): ThemeChoice {
	return currentChoice;
}

/**
 * Switches the theme, applies it to the document, remembers it in local
 * storage for the next visit, and notifies subscribers. Choosing the current
 * theme does nothing. When storage is unavailable the choice still applies
 * until the page reloads.
 */
export function setThemeChoice(choice: ThemeChoice): void {
	if (choice === currentChoice) {
		return;
	}
	currentChoice = choice;
	storeChoice(choice);
	applyThemeChoice();
	for (const listener of listeners) {
		listener();
	}
}

/**
 * Calls listener after every theme switch and returns the function that
 * stops it.
 */
export function subscribeThemeChoice(listener: ThemeListener): () => void {
	listeners.add(listener);
	return () => {
		listeners.delete(listener);
	};
}

/** Returns the theme choice and rerenders the component on a switch. */
export function useThemeChoice(): ThemeChoice {
	return useSyncExternalStore(subscribeThemeChoice, getThemeChoice);
}

/**
 * Applies the theme choice to the document as the `dark` class on the root
 * element, then reapplies it whenever the device color scheme changes, which
 * matters while the choice is System. Call it once at startup. Returns the
 * function that stops following the color scheme.
 */
export function startTheme(): () => void {
	applyThemeChoice();
	const colorScheme = window.matchMedia(DARK_COLOR_SCHEME_QUERY);
	colorScheme.addEventListener("change", applyThemeChoice);
	return () => {
		colorScheme.removeEventListener("change", applyThemeChoice);
	};
}

function applyThemeChoice(): void {
	const dark =
		currentChoice === "system"
			? window.matchMedia(DARK_COLOR_SCHEME_QUERY).matches
			: currentChoice === "dark";
	document.documentElement.classList.toggle(DARK_CLASS_NAME, dark);
}

function readStoredChoice(): ThemeChoice {
	try {
		const stored = window.localStorage.getItem(THEME_STORAGE_KEY);
		return (
			THEME_CHOICES.find((choice) => choice === stored) ?? DEFAULT_THEME_CHOICE
		);
	} catch {
		return DEFAULT_THEME_CHOICE;
	}
}

function storeChoice(choice: ThemeChoice): void {
	try {
		window.localStorage.setItem(THEME_STORAGE_KEY, choice);
	} catch {
		return;
	}
}
