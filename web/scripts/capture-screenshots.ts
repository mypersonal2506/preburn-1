/**
 * Captures review screenshots of every v0.1 dashboard screen from a running
 * server: the setup, login and link screens, then Overview, Customers and a
 * customer, Decisions and a decision, Policies, New policy and a policy,
 * Plans, New plan and a plan, Get started, API keys, SDK, the Installation
 * and Members settings and the account page, each in light and dark at 1280
 * and 390 pixels wide, plus the phone navigation sheet and the component
 * gallery. Each detail screen shows the first row of its list that is not
 * archived. A screen is captured once its tables, skeletons and lazy charts
 * have loaded. Pages open with reduced motion, so charts draw without their
 * entry animation. Writes PNG files and an `index.md` listing them to the
 * output directory.
 *
 * The screens show whatever the server's test environment holds, so seed it
 * through the API first for screens with rows. The link screen needs a
 * password reset link from `preburn admin reset-password`, read from the
 * SCREENSHOT_LINK_URL environment variable. The script consumes it with a
 * random password that it never prints, which also signs the member in for
 * the other screens. Setup is complete on such a server, so the setup screen
 * is captured with the setup status answered as pending inside the browser.
 * Production builds leave out the gallery, so it is captured from a Vite dev
 * server that proxies the API to the same server, after signing in there
 * with the member's email and that password.
 *
 * Run it with `node scripts/capture-screenshots.ts --base-url <url>
 * --development-url <url> --email <email> --output <directory>`.
 * `scripts/capture-screenshots.sh` runs the whole sequence against the test
 * services.
 */
import { randomBytes } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { parseArgs } from "node:util";
import {
	type Browser,
	type BrowserContextOptions,
	chromium,
	type Page,
} from "@playwright/test";

interface Viewport {
	width: number;
	height: number;
}

interface Screenshot {
	file: string;
	description: string;
}

interface ScreenshotRun {
	browser: Browser;
	baseUrl: string;
	output: string;
	screenshots: Screenshot[];
}

interface SignedInMember {
	storageState: StorageState;
	password: string;
}

interface ScreenCapture {
	view: View;
	path: string;
	ready: (page: Page) => Promise<void>;
}

interface DetailScreen {
	view: View;
	listPath: string;
	listTable: string;
	ready: (page: Page) => Promise<void>;
}

type ColorScheme = "light" | "dark";

type StorageState = BrowserContextOptions["storageState"];

const DESKTOP_VIEWPORT: Viewport = { width: 1280, height: 800 };
const PHONE_VIEWPORT: Viewport = { width: 390, height: 844 };
const VIEWPORTS: readonly Viewport[] = [DESKTOP_VIEWPORT, PHONE_VIEWPORT];
const COLOR_SCHEMES: readonly ColorScheme[] = ["light", "dark"];
const PASSWORD_BYTES = 24;
const SETUP_STATUS_ROUTE = "**/api/v1/setup/status";
const SETUP_PLACEHOLDER_TOKEN = "screenshot-setup-token";
const LINK_URL_VARIABLE = "SCREENSHOT_LINK_URL";
const INDEX_FILE = "index.md";
const VIEW_DESCRIPTIONS = {
	setup:
		"Setup screen with the first member form, setup status mocked as pending",
	login: "Login screen",
	link: "Password reset link screen with the member's email",
	overview: "Overview with tiles, chart, plan margins and decision mix",
	customers: "Customers list sorted by margin",
	customer: "Customer detail with tiles, signals, usage and decisions",
	decisions: "Decisions list with the live indicator",
	decision: "Decision detail with summary, lifecycle and ledger entries",
	policies: "Policies list opening on Active",
	"policy-new": "New policy page with the starter chips and sentence",
	policy: "Policy detail with the sentence card and recent decisions",
	plans: "Plans list with the Default badge",
	"plan-new": "New plan page with the plan sentence",
	plan: "Plan detail with the plan sentence and its policies",
	"get-started": "Get started checklist",
	"api-keys": "API keys list",
	sdk: "SDK page with the Python tab",
	"settings-installation": "Installation settings",
	"settings-members": "Members settings with the last login column",
	account: "Account page with the Profile and Password cards",
	"shell-menu": "Phone navigation sheet opened from the header",
	gallery:
		"Component gallery, a dev-only route, with live data from the server",
} as const;
const DEVELOPMENT_LOAD_TIMEOUT_MILLISECONDS = 120_000;
const LIVE_CUSTOMERS_TABLE = 'table[aria-label="Customers"][aria-busy="false"]';
const LOADING_CONTENT = '[aria-busy="true"], [data-slot="skeleton"]';
const CHART_SURFACE = ".recharts-surface";
const ARCHIVED_STATUS = "Archived";

type View = keyof typeof VIEW_DESCRIPTIONS;

const LIST_SCREENS: readonly ScreenCapture[] = [
	{
		view: "overview",
		path: "/",
		ready: (page) => chartReady(page, "Plan margins"),
	},
	{
		view: "customers",
		path: "/customers",
		ready: (page) => tableReady(page, "Customers"),
	},
	{
		view: "decisions",
		path: "/decisions",
		ready: (page) => tableReady(page, "Decisions"),
	},
	{
		view: "policies",
		path: "/policies",
		ready: (page) => tableReady(page, "Policies"),
	},
	{
		view: "policy-new",
		path: "/policies/new",
		ready: (page) => headingReady(page, "New policy"),
	},
	{ view: "plans", path: "/plans", ready: (page) => tableReady(page, "Plans") },
	{
		view: "plan-new",
		path: "/plans/new",
		ready: (page) => headingReady(page, "New plan"),
	},
	{
		view: "get-started",
		path: "/developers/get-started",
		ready: (page) => headingReady(page, "Get started"),
	},
	{
		view: "api-keys",
		path: "/developers/api-keys",
		ready: (page) => tableReady(page, "API keys"),
	},
	{
		view: "sdk",
		path: "/developers/sdk",
		ready: (page) => headingReady(page, "SDK"),
	},
	{
		view: "settings-installation",
		path: "/settings/installation",
		ready: (page) => page.getByLabel("Name").waitFor(),
	},
	{
		view: "settings-members",
		path: "/settings/members",
		ready: (page) => tableReady(page, "Members"),
	},
	{
		view: "account",
		path: "/account",
		ready: (page) => headingReady(page, "Account"),
	},
];

const DETAIL_SCREENS: readonly DetailScreen[] = [
	{
		view: "customer",
		listPath: "/customers",
		listTable: "Customers",
		ready: (page) => chartReady(page, "Signals"),
	},
	{
		view: "decision",
		listPath: "/decisions",
		listTable: "Decisions",
		ready: (page) => tableReady(page, "Ledger entries"),
	},
	{
		view: "policy",
		listPath: "/policies",
		listTable: "Policies",
		ready: (page) => tableReady(page, "Recent decisions"),
	},
	{
		view: "plan",
		listPath: "/plans",
		listTable: "Plans",
		ready: (page) => tableReady(page, "Policies on this plan"),
	},
];

async function headingReady(page: Page, name: string): Promise<void> {
	await page.getByRole("heading", { name }).first().waitFor();
	await contentSettled(page);
}

async function chartReady(page: Page, heading: string): Promise<void> {
	await headingReady(page, heading);
	await page.locator(CHART_SURFACE).first().waitFor();
}

async function tableReady(page: Page, label: string): Promise<void> {
	await page
		.locator(`table[aria-label="${label}"][aria-busy="false"]`)
		.waitFor();
	await contentSettled(page);
}

async function contentSettled(page: Page): Promise<void> {
	await page.locator(LOADING_CONTENT).first().waitFor({ state: "hidden" });
}

async function openPage(
	run: ScreenshotRun,
	colorScheme: ColorScheme,
	viewport: Viewport,
	storageState: StorageState,
): Promise<Page> {
	const context = await run.browser.newContext({
		colorScheme,
		viewport,
		storageState,
		reducedMotion: "reduce",
	});
	return context.newPage();
}

async function saveScreenshot(
	run: ScreenshotRun,
	page: Page,
	view: View,
	colorScheme: ColorScheme,
	viewport: Viewport,
): Promise<void> {
	const file = `${view}-${colorScheme}-${viewport.width}.png`;
	await page.evaluate(async () => {
		await document.fonts.ready;
	});
	await page.screenshot({
		path: join(run.output, file),
		fullPage: true,
		animations: "disabled",
	});
	run.screenshots.push({
		file,
		description: `${VIEW_DESCRIPTIONS[view]}, ${colorScheme}, ${viewport.width} px wide`,
	});
	console.log(`screenshot saved file=${file}`);
}

async function captureSignedOutViews(
	run: ScreenshotRun,
	linkPath: string,
): Promise<void> {
	for (const colorScheme of COLOR_SCHEMES) {
		for (const viewport of VIEWPORTS) {
			const page = await openPage(run, colorScheme, viewport, undefined);
			await page.route(SETUP_STATUS_ROUTE, (route) =>
				route.fulfill({ json: { setup_required: true } }),
			);
			await page.goto(`${run.baseUrl}/setup#${SETUP_PLACEHOLDER_TOKEN}`);
			await page.getByRole("heading", { name: "Set up" }).waitFor();
			await saveScreenshot(run, page, "setup", colorScheme, viewport);
			await page.unroute(SETUP_STATUS_ROUTE);

			await page.goto(`${run.baseUrl}/login`);
			await page.getByRole("heading", { name: "Log in" }).waitFor();
			await saveScreenshot(run, page, "login", colorScheme, viewport);

			await page.goto(`${run.baseUrl}${linkPath}`);
			await page.getByRole("heading", { name: "Reset password" }).waitFor();
			await saveScreenshot(run, page, "link", colorScheme, viewport);
			await page.context().close();
		}
	}
}

async function consumeLink(
	run: ScreenshotRun,
	linkPath: string,
): Promise<SignedInMember> {
	const page = await openPage(run, "light", DESKTOP_VIEWPORT, undefined);
	const password = randomBytes(PASSWORD_BYTES).toString("base64url");
	await page.goto(`${run.baseUrl}${linkPath}`);
	await page.getByLabel("New password").fill(password);
	await page.getByLabel("Confirm password").fill(password);
	await page.getByRole("button", { name: "Set password" }).click();
	await page.getByRole("heading", { name: "Overview" }).waitFor();
	const storageState = await page.context().storageState();
	await page.context().close();
	return { storageState, password };
}

async function logIn(
	run: ScreenshotRun,
	baseUrl: string,
	email: string,
	password: string,
): Promise<StorageState> {
	const page = await openPage(run, "light", DESKTOP_VIEWPORT, undefined);
	await page.goto(`${baseUrl}/login`);
	await page.getByLabel("Email").fill(email);
	await page.getByLabel("Password").fill(password);
	await page.getByRole("button", { name: "Log in" }).click();
	await page
		.getByRole("heading", { name: "Overview" })
		.waitFor({ timeout: DEVELOPMENT_LOAD_TIMEOUT_MILLISECONDS });
	const storageState = await page.context().storageState();
	await page.context().close();
	return storageState;
}

async function detailCaptures(
	run: ScreenshotRun,
	storageState: StorageState,
): Promise<ScreenCapture[]> {
	const page = await openPage(run, "light", DESKTOP_VIEWPORT, storageState);
	const captures: ScreenCapture[] = [];
	for (const screen of DETAIL_SCREENS) {
		await page.goto(`${run.baseUrl}${screen.listPath}`);
		await tableReady(page, screen.listTable);
		const path = await page
			.locator(`table[aria-label="${screen.listTable}"] tbody tr`)
			.filter({ hasNotText: ARCHIVED_STATUS })
			.locator("a")
			.first()
			.getAttribute("href");
		if (path === null) {
			throw new Error(`detail link missing view=${screen.view}`);
		}
		captures.push({ view: screen.view, path, ready: screen.ready });
	}
	await page.context().close();
	return captures;
}

async function captureSignedInViews(
	run: ScreenshotRun,
	storageState: StorageState,
): Promise<void> {
	const captures = [
		...LIST_SCREENS,
		...(await detailCaptures(run, storageState)),
	];
	for (const colorScheme of COLOR_SCHEMES) {
		for (const viewport of VIEWPORTS) {
			const page = await openPage(run, colorScheme, viewport, storageState);
			for (const capture of captures) {
				await page.goto(`${run.baseUrl}${capture.path}`);
				await capture.ready(page);
				await saveScreenshot(run, page, capture.view, colorScheme, viewport);
			}
			if (viewport === PHONE_VIEWPORT) {
				await page.goto(`${run.baseUrl}/`);
				await headingReady(page, "Plan margins");
				await page
					.locator("header")
					.getByRole("button", { name: "Toggle Sidebar" })
					.click();
				await page.getByRole("dialog").waitFor();
				await saveScreenshot(run, page, "shell-menu", colorScheme, viewport);
			}
			await page.context().close();
		}
	}
}

async function captureGalleryViews(
	run: ScreenshotRun,
	developmentUrl: string,
	storageState: StorageState,
): Promise<void> {
	for (const colorScheme of COLOR_SCHEMES) {
		for (const viewport of VIEWPORTS) {
			const page = await openPage(run, colorScheme, viewport, storageState);
			await page.goto(`${developmentUrl}/gallery`);
			await page
				.getByRole("heading", { name: "Gallery", level: 1 })
				.waitFor({ timeout: DEVELOPMENT_LOAD_TIMEOUT_MILLISECONDS });
			await page.locator(LIVE_CUSTOMERS_TABLE).waitFor();
			await saveScreenshot(run, page, "gallery", colorScheme, viewport);
			await page.context().close();
		}
	}
}

function writeIndex(run: ScreenshotRun): void {
	const lines = [
		"# Dashboard screenshots",
		"",
		...run.screenshots.map(
			({ file, description }) => `- \`${file}\`: ${description}.`,
		),
		"",
	];
	writeFileSync(join(run.output, INDEX_FILE), lines.join("\n"));
}

async function captureScreenshots(
	baseUrl: string,
	developmentUrl: string,
	email: string,
	output: string,
): Promise<void> {
	const linkUrl = process.env[LINK_URL_VARIABLE];
	if (linkUrl === undefined || linkUrl === "") {
		throw new Error(`screenshot link missing variable=${LINK_URL_VARIABLE}`);
	}
	const link = new URL(linkUrl);
	const linkPath = `${link.pathname}${link.hash}`;
	mkdirSync(output, { recursive: true });
	const browser = await chromium.launch();
	const run: ScreenshotRun = { browser, baseUrl, output, screenshots: [] };
	try {
		await captureSignedOutViews(run, linkPath);
		const member = await consumeLink(run, linkPath);
		await captureSignedInViews(run, member.storageState);
		await captureGalleryViews(
			run,
			developmentUrl,
			await logIn(run, developmentUrl, email, member.password),
		);
	} finally {
		await browser.close();
	}
	writeIndex(run);
	console.log(
		`screenshots captured count=${run.screenshots.length} output=${output}`,
	);
}

if (import.meta.main) {
	const { values } = parseArgs({
		options: {
			"base-url": { type: "string" },
			"development-url": { type: "string" },
			email: { type: "string" },
			output: { type: "string" },
		},
	});
	const baseUrl = values["base-url"];
	const developmentUrl = values["development-url"];
	const email = values.email;
	const output = values.output;
	if (
		baseUrl === undefined ||
		developmentUrl === undefined ||
		email === undefined ||
		output === undefined
	) {
		throw new Error(
			"screenshot arguments missing required=--base-url,--development-url,--email,--output",
		);
	}
	await captureScreenshots(baseUrl, developmentUrl, email, output);
}
