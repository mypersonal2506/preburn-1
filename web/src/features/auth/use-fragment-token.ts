import { useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect } from "react";

declare module "@tanstack/react-router" {
	interface HistoryState {
		/** Token that useFragmentToken moved out of the URL fragment. */
		fragmentToken?: string;
	}
}

/**
 * Returns the token of a link that carries it in the URL fragment, such as
 * `/setup#<token>`, or "" when the link has none. The token moves from the
 * address bar into the state of the history entry, which replaces the entry,
 * so it leaves the URL and the browser history but survives a reload.
 */
export function useFragmentToken(): string {
	const location = useLocation();
	const navigate = useNavigate();
	const fragmentToken = location.hash;

	useEffect(() => {
		if (fragmentToken !== "") {
			void navigate({ to: ".", replace: true, state: { fragmentToken } });
		}
	}, [fragmentToken, navigate]);

	if (fragmentToken !== "") {
		return fragmentToken;
	}
	return location.state.fragmentToken ?? "";
}
