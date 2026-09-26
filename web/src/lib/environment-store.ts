import { useSyncExternalStore } from "react";
import type { StreamDashboardDecisionsData } from "@/client";

/** The Preburn environment the dashboard reads and writes, test or live. */
export type Environment = StreamDashboardDecisionsData["query"]["environment"];

type EnvironmentListener = () => void;

const ENVIRONMENT_STORAGE_KEY = "preburn.environment";
const DEFAULT_ENVIRONMENT: Environment = "test";
const ENVIRONMENTS: readonly Environment[] = ["test", "live"];

const listeners = new Set<EnvironmentListener>();
let currentEnvironment = readStoredEnvironment();

/** Returns the environment the dashboard works in. */
export function getEnvironment(): Environment {
	return currentEnvironment;
}

/**
 * Switches the dashboard to environment, remembers it in local storage for
 * the next visit, and notifies subscribers. Switching to the current
 * environment does nothing. When storage is unavailable the switch still
 * applies until the page reloads.
 */
export function setEnvironment(environment: Environment): void {
	if (environment === currentEnvironment) {
		return;
	}
	currentEnvironment = environment;
	storeEnvironment(environment);
	for (const listener of listeners) {
		listener();
	}
}

/**
 * Calls listener after every environment switch and returns the function
 * that stops it.
 */
export function subscribeEnvironment(
	listener: EnvironmentListener,
): () => void {
	listeners.add(listener);
	return () => {
		listeners.delete(listener);
	};
}

/** Returns the current environment and rerenders the component on a switch. */
export function useEnvironment(): Environment {
	return useSyncExternalStore(subscribeEnvironment, getEnvironment);
}

function readStoredEnvironment(): Environment {
	try {
		const stored = window.localStorage.getItem(ENVIRONMENT_STORAGE_KEY);
		return (
			ENVIRONMENTS.find((environment) => environment === stored) ??
			DEFAULT_ENVIRONMENT
		);
	} catch {
		return DEFAULT_ENVIRONMENT;
	}
}

function storeEnvironment(environment: Environment): void {
	try {
		window.localStorage.setItem(ENVIRONMENT_STORAGE_KEY, environment);
	} catch {}
}
