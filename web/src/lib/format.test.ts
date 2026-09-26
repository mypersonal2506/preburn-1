import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import {
	formatCount,
	formatDate,
	formatDateTime,
	formatMargin,
	formatMarginGap,
	formatMoney,
	formatPace,
	formatPercent,
	formatPeriod,
	formatPeriodEnd,
	formatQuantity,
	formatRelativeTime,
	formatUnitCost,
	ratioToNumber,
} from "@/lib/format";

const now = new Date("2026-09-26T12:00:00Z");
const VIEWER_TIME_ZONE = "America/New_York";

beforeEach(() => {
	vi.stubEnv("TZ", VIEWER_TIME_ZONE);
});

afterEach(() => {
	vi.unstubAllEnvs();
});

describe("formatMoney", () => {
	test.each([
		["29808.450000000", "$29,808.45"],
		["-7.200000000", "-$7.20"],
		["0.000000000", "$0.00"],
		["-0.001000000", "$0.00"],
		["0.004000000", "$0.00"],
		["1.005000000", "$1.01"],
		["-1.005000000", "-$1.01"],
		["30", "$30.00"],
		["999999999.999999999", "$1,000,000,000.00"],
	])("%s is %s", (amount, text) => {
		expect(formatMoney(amount)).toBe(text);
	});

	test.each(["", "inf", "12,5", "1e3", "$5"])("rejects %j", (amount) => {
		expect(() => formatMoney(amount)).toThrow(
			`decimal value invalid value=${amount}`,
		);
	});
});

describe("formatUnitCost", () => {
	test.each([
		["0.000160000", "$0.00016"],
		["0.000163456", "$0.0001635"],
		["0.000000001", "$0.000000001"],
		["-0.000500000", "-$0.0005"],
		["0.009999999", "$0.01"],
		["0.010000000", "$0.01"],
		["0.150000000", "$0.15"],
		["12.500000000", "$12.50"],
		["0.000000000", "$0.00"],
	])("%s is %s", (amount, text) => {
		expect(formatUnitCost(amount)).toBe(text);
	});
});

describe("formatPercent", () => {
	test.each([
		["0.4000", "40.0%"],
		["-0.0720", "-7.2%"],
		["0.1045", "10.5%"],
		["1.0000", "100.0%"],
		["-0.0004", "0.0%"],
		["12.3456", "1,234.6%"],
	])("%s is %s", (ratio, text) => {
		expect(formatPercent(ratio)).toBe(text);
	});

	test.each(["inf", "-inf"])("rejects %s", (ratio) => {
		expect(() => formatPercent(ratio)).toThrow(
			`decimal value invalid value=${ratio}`,
		);
	});
});

describe("formatMargin", () => {
	test.each([
		["0.4000", "40.0%"],
		["-0.0720", "-7.2%"],
		[null, "No revenue"],
		["-inf", "No revenue"],
	])("%s is %s", (margin, text) => {
		expect(formatMargin(margin)).toBe(text);
	});
});

describe("formatMarginGap", () => {
	test.each([
		["0.3000", "0.4040", "10.4 pts below target"],
		["0.5045", "0.4000", "10.5 pts above target"],
		["-0.0720", "0.4000", "47.2 pts below target"],
		["0.4000", "0.4000", "At target"],
		["0.4004", "0.4000", "At target"],
		["0.3996", "0.4000", "At target"],
		["0.3995", "0.4000", "0.1 pts below target"],
		["-inf", "0.4000", "No revenue"],
	])("margin %s against target %s is %s", (margin, target, text) => {
		expect(formatMarginGap(margin, target)).toBe(text);
	});
});

describe("formatPace", () => {
	test.each([
		["2.0000", "2.0x"],
		["0.4500", "0.5x"],
		["12.3456", "12.3x"],
		["1234.0000", "1,234.0x"],
		["0.0000", "0.0x"],
		["inf", "No allowance"],
	])("%s is %s", (pace, text) => {
		expect(formatPace(pace)).toBe(text);
	});
});

describe("formatCount", () => {
	test.each([
		[0, "0"],
		[7, "7"],
		[1234567, "1,234,567"],
	])("%d is %s", (count, text) => {
		expect(formatCount(count)).toBe(text);
	});
});

describe("formatQuantity", () => {
	test.each([
		["1200", "1,200"],
		["8.5", "8.5"],
		["0.000001", "0.000001"],
		["1200.500000", "1,200.5"],
	])("%s is %s", (quantity, text) => {
		expect(formatQuantity(quantity)).toBe(text);
	});
});

describe("ratioToNumber", () => {
	test.each([
		["0.4000", 0.4],
		["-0.0720", -0.072],
		["inf", Number.POSITIVE_INFINITY],
		["-inf", Number.NEGATIVE_INFINITY],
	])("%s is %d", (ratio, value) => {
		expect(ratioToNumber(ratio)).toBe(value);
	});

	test("rejects an object property name", () => {
		expect(() => ratioToNumber("toString")).toThrow(
			"decimal value invalid value=toString",
		);
	});

	test("rejects text that is not a ratio", () => {
		expect(() => ratioToNumber("NaN")).toThrow(
			"decimal value invalid value=NaN",
		);
	});
});

describe("dates", () => {
	test.each([
		["2026-09-16T03:32:00Z", "Sep 16, 2026"],
		["2026-09-16", "Sep 16, 2026"],
		["2026-09-30T23:59:59Z", "Sep 30, 2026"],
		["2026-09-16T23:30:00-05:00", "Sep 17, 2026"],
	])("the UTC date of %s is %s", (timestamp, text) => {
		expect(formatDate(timestamp)).toBe(text);
	});

	test.each([
		["2026-09-16T03:32:00Z", "Sep 15, 23:32"],
		["2026-09-16T15:04:59Z", "Sep 16, 11:04"],
		["2026-01-02T00:00:00Z", "Jan 1, 19:00"],
	])("the local time of %s is %s", (timestamp, text) => {
		expect(formatDateTime(timestamp)).toBe(text);
	});

	test.each([
		[
			"2026-09-01T00:00:00Z",
			"2026-10-01T00:00:00Z",
			"Sep 1 to Sep 30, 2026 UTC",
		],
		[
			"2026-09-14T14:23:00Z",
			"2026-10-14T14:23:00Z",
			"Sep 14 to Oct 14, 2026 UTC",
		],
		[
			"2026-12-15T00:00:00Z",
			"2027-01-15T00:00:00Z",
			"Dec 15, 2026 to Jan 14, 2027 UTC",
		],
	])("the period from %s to %s is %s", (start, end, text) => {
		expect(formatPeriod(start, end)).toBe(text);
	});

	test.each([
		["2026-10-01T00:00:00Z", "Sep 30, 2026"],
		["2026-10-14T14:23:00Z", "Oct 14, 2026"],
		["2027-01-01T00:00:00Z", "Dec 31, 2026"],
	])("a period ending at %s ends on %s", (periodEnd, text) => {
		expect(formatPeriodEnd(periodEnd)).toBe(text);
	});

	test("rejects a timestamp that does not parse", () => {
		expect(() => formatDate("yesterday")).toThrow(
			"timestamp invalid value=yesterday",
		);
	});
});

describe("formatRelativeTime", () => {
	test.each([
		["2026-09-26T12:00:00Z", "now"],
		["2026-09-26T11:59:30Z", "30 seconds ago"],
		["2026-09-26T11:55:00Z", "5 minutes ago"],
		["2026-09-26T11:00:01Z", "59 minutes ago"],
		["2026-09-26T10:00:00Z", "2 hours ago"],
		["2026-09-25T11:00:00Z", "yesterday"],
		["2026-09-20T12:00:01Z", "5 days ago"],
		["2026-09-19T12:00:01Z", "6 days ago"],
		["2026-09-26T12:00:59Z", "in 59 seconds"],
		["2026-09-29T12:00:00Z", "in 3 days"],
	])("%s is %s", (timestamp, text) => {
		expect(formatRelativeTime(timestamp, now)).toBe(text);
	});

	test.each([
		["2026-09-19T12:00:00Z", "Sep 19, 08:00"],
		["2026-08-01T08:15:00Z", "Aug 1, 04:15"],
		["2026-10-03T12:00:00Z", "Oct 3, 08:00"],
	])("%s is 7 days or more away and shows %s", (timestamp, text) => {
		expect(formatRelativeTime(timestamp, now)).toBe(text);
	});
});
