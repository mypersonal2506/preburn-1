import { useRef } from "react";

/**
 * React keys for the items of an edited list by item identity. `carry`
 * hands an item's key to the item that replaces it, so an edited item keeps
 * its element and a removed one takes only its own element away.
 */
export interface ItemKeys<Item extends object> {
	keyOf: (item: Item) => string;
	carry: (item: Item, nextItem: Item) => void;
}

/**
 * Keeps ItemKeys for the lifetime of the component. Each item object gets
 * its own key, so a list must never hold one object twice.
 */
export function useItemKeys<Item extends object>(): ItemKeys<Item> {
	const keys = useRef(new WeakMap<Item, string>());
	const keyCount = useRef(0);

	function keyOf(item: Item): string {
		const key = keys.current.get(item);
		if (key !== undefined) {
			return key;
		}
		keyCount.current += 1;
		const newKey = String(keyCount.current);
		keys.current.set(item, newKey);
		return newKey;
	}

	function carry(item: Item, nextItem: Item): void {
		keys.current.set(nextItem, keyOf(item));
	}

	return { keyOf, carry };
}
