/**
 * Writes text to the clipboard and resolves true, or false when the browser
 * refuses. The Clipboard API rejects without permission and does not exist
 * outside a secure context, such as a dashboard served over plain http on
 * another host, so every caller needs a fallback for false.
 */
export async function copyText(text: string): Promise<boolean> {
	try {
		await navigator.clipboard.writeText(text);
		return true;
	} catch {
		return false;
	}
}
