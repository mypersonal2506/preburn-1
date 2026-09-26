import { useEffect, useState } from "react";

const FACTS_DEBOUNCE_MILLISECONDS = 400;

/**
 * Returns the draft as it stood 400 ms after its last change, for the fact
 * queries of a sentence, so they load while the sentence is edited without
 * firing on every keystroke. The first render returns the draft at once.
 * Pass a value whose identity changes only when its content does, such as
 * the form values from the form store.
 */
export function useFactsDraft<Draft>(draft: Draft): Draft {
	const [settledDraft, setSettledDraft] = useState(draft);

	useEffect(() => {
		const timer = window.setTimeout(
			() => setSettledDraft(() => draft),
			FACTS_DEBOUNCE_MILLISECONDS,
		);
		return () => window.clearTimeout(timer);
	}, [draft]);

	return settledDraft;
}
