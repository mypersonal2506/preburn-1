import { formatPercent } from "@/lib/format";

const RATIO_DECIMALS = 4;

/**
 * Formats part as a share of whole with one decimal, such as "38.7%", for
 * display only. whole must be above zero.
 */
export function formatShare(part: number, whole: number): string {
	return formatPercent((part / whole).toFixed(RATIO_DECIMALS));
}
