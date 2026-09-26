const LOCALE = "en-US";
const PERIOD_TIME_ZONE = "UTC";
const PERIOD_END_EXCLUSIVE_MILLISECONDS = 1;
const MARGIN_WITHOUT_REVENUE = "-inf";
const PACE_WITHOUT_ALLOWANCE = "inf";
const NO_REVENUE_TEXT = "No revenue";
const NO_ALLOWANCE_TEXT = "No allowance";
const RELATIVE_TIME_WINDOW_DAYS = 7;
const SUB_CENT_THRESHOLD = 0.01;
const SUB_CENT_SIGNIFICANT_DIGITS = 4;
const QUANTITY_DECIMALS = 6;
const BASIS_POINTS_PER_RATIO = 10_000;
const BASIS_POINTS_PER_POINT = 100;
const MILLISECONDS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;
const SECONDS_PER_HOUR = 3_600;
const SECONDS_PER_DAY = 86_400;
const DECIMAL_TEXT_PATTERN = /^-?\d+(\.\d+)?$/;
const RATIO_INFINITIES = new Map<string, Intl.StringNumericLiteral>([
	["inf", "Infinity"],
	["-inf", "-Infinity"],
]);

const moneyFormat = new Intl.NumberFormat(LOCALE, {
	style: "currency",
	currency: "USD",
	signDisplay: "negative",
});
const subCentMoneyFormat = new Intl.NumberFormat(LOCALE, {
	style: "currency",
	currency: "USD",
	signDisplay: "negative",
	minimumSignificantDigits: 1,
	maximumSignificantDigits: SUB_CENT_SIGNIFICANT_DIGITS,
});
const percentFormat = new Intl.NumberFormat(LOCALE, {
	style: "percent",
	minimumFractionDigits: 1,
	maximumFractionDigits: 1,
	signDisplay: "negative",
});
const oneDecimalFormat = new Intl.NumberFormat(LOCALE, {
	minimumFractionDigits: 1,
	maximumFractionDigits: 1,
	signDisplay: "negative",
});
const countFormat = new Intl.NumberFormat(LOCALE);
const quantityFormat = new Intl.NumberFormat(LOCALE, {
	maximumFractionDigits: QUANTITY_DECIMALS,
});
const EVENT_TIME_OPTIONS: Intl.DateTimeFormatOptions = {
	month: "short",
	day: "numeric",
	hour: "2-digit",
	minute: "2-digit",
	hourCycle: "h23",
};

const dateFormat = new Intl.DateTimeFormat(LOCALE, {
	month: "short",
	day: "numeric",
	year: "numeric",
	timeZone: PERIOD_TIME_ZONE,
});
const monthDayFormat = new Intl.DateTimeFormat(LOCALE, {
	month: "short",
	day: "numeric",
	timeZone: PERIOD_TIME_ZONE,
});
const relativeTimeFormat = new Intl.RelativeTimeFormat(LOCALE, {
	numeric: "auto",
});

/**
 * Formats an API amount in USD with two decimals, such as "$29,808.45" or
 * "-$7.20", rounding half away from zero on the exact decimal. Throws for
 * text that is not a decimal.
 */
export function formatMoney(amount: string): string {
	return moneyFormat.format(decimalLiteral(amount));
}

/**
 * Formats the API amount of a unit cost in USD: two decimals from one cent
 * up, and up to four significant digits below one cent, such as "$0.00016".
 * Zero is "$0.00". Throws for text that is not a decimal.
 */
export function formatUnitCost(amount: string): string {
	const literal = decimalLiteral(amount);
	const magnitude = Math.abs(Number(literal));
	return magnitude > 0 && magnitude < SUB_CENT_THRESHOLD
		? subCentMoneyFormat.format(literal)
		: moneyFormat.format(literal);
}

/**
 * Formats an API ratio as a percent with one decimal, such as "40.0%" for
 * "0.4000". Throws for text that is not a decimal, "inf" and "-inf"
 * included. Format a margin with formatMargin.
 */
export function formatPercent(ratio: string): string {
	return percentFormat.format(decimalLiteral(ratio));
}

/**
 * Formats an API margin as a percent with one decimal, such as "40.0%". A
 * null margin, which the API sends without revenue above zero, and "-inf",
 * the projected margin of cost without revenue, read "No revenue".
 */
export function formatMargin(margin: string | null): string {
	return margin === null || margin === MARGIN_WITHOUT_REVENUE
		? NO_REVENUE_TEXT
		: formatPercent(margin);
}

/**
 * Formats how far a margin is from its target in percentage points with one
 * decimal, such as "10.4 pts below target", or "At target" when the gap
 * rounds to zero. Both are API ratios, and a margin of "-inf", cost without
 * revenue, reads "No revenue".
 */
export function formatMarginGap(margin: string, targetMargin: string): string {
	if (margin === MARGIN_WITHOUT_REVENUE) {
		return NO_REVENUE_TEXT;
	}
	const gapInPoints =
		(ratioToBasisPoints(margin) - ratioToBasisPoints(targetMargin)) /
		BASIS_POINTS_PER_POINT;
	const points = oneDecimalFormat.format(Math.abs(gapInPoints));
	if (points === oneDecimalFormat.format(0)) {
		return "At target";
	}
	return `${points} pts ${gapInPoints < 0 ? "below" : "above"} target`;
}

/**
 * Formats an API pace ratio with one decimal and an "x", such as "2.0x".
 * "inf", the pace of cost against a zero allowance, reads "No allowance".
 */
export function formatPace(pace: string): string {
	return pace === PACE_WITHOUT_ALLOWANCE
		? NO_ALLOWANCE_TEXT
		: `${oneDecimalFormat.format(decimalLiteral(pace))}x`;
}

/** Formats a whole count with thousands separators, such as "1,234". */
export function formatCount(count: number): string {
	return countFormat.format(count);
}

/**
 * Formats an API usage quantity with thousands separators and up to six
 * decimals, such as "1,200.5". Throws for text that is not a decimal.
 */
export function formatQuantity(quantity: string): string {
	return quantityFormat.format(decimalLiteral(quantity));
}

/**
 * Returns an API ratio as a number for display arithmetic such as bar
 * widths and tone thresholds, with "inf" and "-inf" as the infinities. Never
 * use the result to build a request. Throws for other text that is not a
 * decimal.
 */
export function ratioToNumber(ratio: string): number {
	return Number(ratioLiteral(ratio));
}

/**
 * Formats the UTC date of an API timestamp, such as "Sep 16, 2026", for
 * billing periods, period boundaries and daily buckets, which follow UTC
 * days. Throws for a timestamp that does not parse.
 */
export function formatDate(timestamp: string): string {
	return dateFormat.format(parseTimestamp(timestamp));
}

/**
 * Formats the last UTC day a billing period includes from its exclusive API
 * end, such as "Sep 30, 2026" for a period ending at Oct 1 00:00 UTC, so
 * period ends read the same everywhere. Throws for a timestamp that does not
 * parse.
 */
export function formatPeriodEnd(periodEnd: string): string {
	return dateFormat.format(lastIncludedMoment(periodEnd));
}

/**
 * Formats a billing period from two API timestamps, the end exclusive, as
 * the UTC days it covers with the time zone named once, such as
 * "Sep 1 to Sep 30, 2026 UTC". Use it wherever a period shows next to event
 * times. Throws for a timestamp that does not parse.
 */
export function formatPeriod(periodStart: string, periodEnd: string): string {
	const start = parseTimestamp(periodStart);
	const lastMoment = lastIncludedMoment(periodEnd);
	const startText =
		start.getUTCFullYear() === lastMoment.getUTCFullYear()
			? monthDayFormat.format(start)
			: dateFormat.format(start);
	return `${startText} to ${dateFormat.format(lastMoment)} ${PERIOD_TIME_ZONE}`;
}

/**
 * Formats the date and 24-hour time of an event's API timestamp, such as a
 * decision or an update, in the viewer's time zone, such as "Sep 16, 03:32".
 * Throws for a timestamp that does not parse.
 */
export function formatDateTime(timestamp: string): string {
	return new Intl.DateTimeFormat(LOCALE, EVENT_TIME_OPTIONS).format(
		parseTimestamp(timestamp),
	);
}

/**
 * Formats an API timestamp relative to now when it is less than 7 days away,
 * such as "2 hours ago", "yesterday" or "in 3 days", and as formatDateTime,
 * in the viewer's time zone, otherwise. Pair it with formatDateTime in a
 * tooltip. Throws for a timestamp that does not parse.
 */
export function formatRelativeTime(timestamp: string, now: Date): string {
	const seconds = Math.trunc(
		(parseTimestamp(timestamp).getTime() - now.getTime()) /
			MILLISECONDS_PER_SECOND,
	);
	const distance = Math.abs(seconds);
	if (distance >= RELATIVE_TIME_WINDOW_DAYS * SECONDS_PER_DAY) {
		return formatDateTime(timestamp);
	}
	if (distance >= SECONDS_PER_DAY) {
		return relativeTimeFormat.format(
			Math.trunc(seconds / SECONDS_PER_DAY),
			"day",
		);
	}
	if (distance >= SECONDS_PER_HOUR) {
		return relativeTimeFormat.format(
			Math.trunc(seconds / SECONDS_PER_HOUR),
			"hour",
		);
	}
	if (distance >= SECONDS_PER_MINUTE) {
		return relativeTimeFormat.format(
			Math.trunc(seconds / SECONDS_PER_MINUTE),
			"minute",
		);
	}
	return relativeTimeFormat.format(seconds, "second");
}

function decimalLiteral(text: string): Intl.StringNumericLiteral {
	if (!isDecimalLiteral(text)) {
		throw new Error(`decimal value invalid value=${text}`);
	}
	return text;
}

function isDecimalLiteral(text: string): text is `${number}` {
	return DECIMAL_TEXT_PATTERN.test(text);
}

function ratioLiteral(ratio: string): Intl.StringNumericLiteral {
	return RATIO_INFINITIES.get(ratio) ?? decimalLiteral(ratio);
}

function ratioToBasisPoints(ratio: string): number {
	return Math.round(Number(decimalLiteral(ratio)) * BASIS_POINTS_PER_RATIO);
}

function lastIncludedMoment(periodEnd: string): Date {
	return new Date(
		parseTimestamp(periodEnd).getTime() - PERIOD_END_EXCLUSIVE_MILLISECONDS,
	);
}

function parseTimestamp(timestamp: string): Date {
	const date = new Date(timestamp);
	if (Number.isNaN(date.getTime())) {
		throw new Error(`timestamp invalid value=${timestamp}`);
	}
	return date;
}
