import type {
	CheckRequest,
	RecordRevenueRequest,
	ReportRequest,
} from "@/client";
import type { CodeSnippet } from "@/components/code-panel";

type SnippetRequest = CheckRequest | ReportRequest | RecordRevenueRequest;

const API_KEY_PLACEHOLDER = "YOUR_API_KEY";
const PYTHON_SDK_SOURCE = "git+https://github.com/preburn/sdk-python";
const PYTHON_SDK_TAG = "v0.1.0";
const SHELL_LABEL = "Shell";
const PYTHON_LABEL = "Python";
const CURL_LABEL = "curl";
const CHECK_PATH = "/api/v1/check";
const REPORT_PATH = "/api/v1/report";
const REVENUE_PATH = "/api/v1/revenue";
const JSON_INDENT_SPACES = 2;
const MONTH_DIGITS = 2;
const SAMPLE_CUSTOMER_ID = "customer_42";
const SAMPLE_FEATURE = "chat";
const SAMPLE_PROVIDER = "openai";
const SAMPLE_MODEL = "gpt-6-luna";
const SAMPLE_INPUT_TOKEN_ESTIMATE = 1200;
const SAMPLE_OUTPUT_TOKEN_ESTIMATE = 400;
const SAMPLE_DECISION_ID = "dec_01jbvagescfn78y0938nkrkayd";
const SAMPLE_SUBSCRIPTION_AMOUNT = "49.00";

const PYTHON_CHECK_LINES = [
	"decision = preburn.check(",
	`    "${SAMPLE_CUSTOMER_ID}",`,
	`    "${SAMPLE_FEATURE}",`,
	`    "${SAMPLE_PROVIDER}",`,
	`    "${SAMPLE_MODEL}",`,
	"    usage_estimate={",
	`        "input_tokens": ${SAMPLE_INPUT_TOKEN_ESTIMATE},`,
	`        "output_tokens": ${SAMPLE_OUTPUT_TOKEN_ESTIMATE},`,
	"    },",
	")",
];

const SAMPLE_CHECK_REQUEST: CheckRequest = {
	customer_id: SAMPLE_CUSTOMER_ID,
	feature: SAMPLE_FEATURE,
	provider: SAMPLE_PROVIDER,
	model: SAMPLE_MODEL,
	usage_estimate: {
		input_tokens: String(SAMPLE_INPUT_TOKEN_ESTIMATE),
		output_tokens: String(SAMPLE_OUTPUT_TOKEN_ESTIMATE),
	},
};

const SAMPLE_REPORT_REQUEST: ReportRequest = {
	decision_source: "server",
	decision_id: SAMPLE_DECISION_ID,
	usage: { input_tokens: "1180", output_tokens: "312" },
};

/** Installs the Python SDK from its release tag on GitHub. */
export const PYTHON_INSTALL_SNIPPET: CodeSnippet = {
	label: SHELL_LABEL,
	code: `pip install "preburn @ ${PYTHON_SDK_SOURCE}@${PYTHON_SDK_TAG}"`,
};

/** Installs the Python SDK with the openai extra the OpenAI wrapper needs. */
export const PYTHON_OPENAI_INSTALL_SNIPPET: CodeSnippet = {
	label: SHELL_LABEL,
	code: `pip install "preburn[openai] @ ${PYTHON_SDK_SOURCE}@${PYTHON_SDK_TAG}"`,
};

/**
 * A Python check against the Preburn server at origin that prints the
 * outcome. The API key is a placeholder to replace.
 */
export function pythonFirstCheckSnippet(origin: string): CodeSnippet {
	return {
		label: PYTHON_LABEL,
		code: [
			"from preburn import Preburn",
			"",
			...pythonClientLines(origin),
			"",
			...PYTHON_CHECK_LINES,
			"print(decision.outcome)",
		].join("\n"),
	};
}

/**
 * The Python check, run and report loop against the Preburn server at
 * origin: deny raises, a failed model call releases the reservation, and the
 * measured usage is reported. The API key is a placeholder to replace.
 */
export function pythonCheckAndReportSnippet(origin: string): CodeSnippet {
	return {
		label: PYTHON_LABEL,
		code: [
			"from preburn import Preburn",
			"",
			...pythonClientLines(origin),
			"",
			...PYTHON_CHECK_LINES,
			"decision.raise_for_denial()",
			"",
			"try:",
			"    usage = run_model(",
			"        decision.provider,",
			"        decision.model,",
			"        decision.overrides,",
			"    )",
			"except Exception:",
			"    preburn.release(decision)",
			"    raise",
			"",
			"preburn.report(decision, usage)",
		].join("\n"),
	};
}

/**
 * An OpenAI client wrapped by the Python SDK against the Preburn server at
 * origin, so each chat completion is checked and reported. The API key is a
 * placeholder to replace.
 */
export function pythonOpenAiSnippet(origin: string): CodeSnippet {
	return {
		label: PYTHON_LABEL,
		code: [
			"from openai import OpenAI",
			"",
			"from preburn import CallContext, Preburn",
			"from preburn.wrappers.openai import wrap_openai",
			"",
			...pythonClientLines(origin),
			"client = wrap_openai(OpenAI(), preburn)",
			"",
			"completion = client.chat.completions.create(",
			`    model="${SAMPLE_MODEL}",`,
			"    messages=[",
			'        {"role": "user", "content": "Summarize this."},',
			"    ],",
			`    max_completion_tokens=${SAMPLE_OUTPUT_TOKEN_ESTIMATE},`,
			"    preburn=CallContext(",
			`        customer_id="${SAMPLE_CUSTOMER_ID}",`,
			`        feature="${SAMPLE_FEATURE}",`,
			"    ),",
			")",
		].join("\n"),
	};
}

/** A curl check against the Preburn server at origin. */
export function curlCheckSnippet(origin: string): CodeSnippet {
	return curlPostSnippet(origin, CHECK_PATH, SAMPLE_CHECK_REQUEST);
}

/** A curl report of a checked decision's usage to the server at origin. */
export function curlReportSnippet(origin: string): CodeSnippet {
	return curlPostSnippet(origin, REPORT_PATH, SAMPLE_REPORT_REQUEST);
}

/**
 * A curl request recording a subscription for the UTC calendar month that
 * contains now, with a source reference naming the month, so a second run in
 * the same month returns the stored entry.
 */
export function curlRevenueSnippet(origin: string, now: Date): CodeSnippet {
	const year = now.getUTCFullYear();
	const monthIndex = now.getUTCMonth();
	const month = String(monthIndex + 1).padStart(MONTH_DIGITS, "0");
	const revenueRequest: RecordRevenueRequest = {
		customer_id: SAMPLE_CUSTOMER_ID,
		kind: "subscription",
		amount: SAMPLE_SUBSCRIPTION_AMOUNT,
		period_start: new Date(Date.UTC(year, monthIndex, 1)).toISOString(),
		period_end: new Date(Date.UTC(year, monthIndex + 1, 1)).toISOString(),
		source_reference: `invoice_${year}_${month}`,
	};
	return curlPostSnippet(origin, REVENUE_PATH, revenueRequest);
}

function pythonClientLines(origin: string): string[] {
	return [
		"preburn = Preburn(",
		`    api_key="${API_KEY_PLACEHOLDER}",`,
		`    base_url="${origin}",`,
		")",
	];
}

function curlPostSnippet(
	origin: string,
	path: string,
	body: SnippetRequest,
): CodeSnippet {
	const bodyJson = JSON.stringify(body, null, JSON_INDENT_SPACES);
	return {
		label: CURL_LABEL,
		code: [
			`curl -X POST ${origin}${path} \\`,
			`  -H "Authorization: Bearer ${API_KEY_PLACEHOLDER}" \\`,
			'  -H "Content-Type: application/json" \\',
			`  -d '${bodyJson.replaceAll("\n", "\n  ")}'`,
		].join("\n"),
	};
}
