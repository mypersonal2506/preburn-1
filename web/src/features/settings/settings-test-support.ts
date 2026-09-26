import type {
	AddedMemberResponse,
	MemberResponse,
	PageBodyMemberResponse,
	PageBodyPlanResponse,
	PlanResponse,
	ResetLinkResponse,
	SettingsResponse,
} from "@/client";
import { installationSettings, signedInMember } from "@/routes/-render-app";

const creatorPlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay1",
	name: "Creator",
	mode: "margin_target",
	target_margin: "0.4000",
	allowance: null,
	customer_count: 12,
	hold_times: {},
	status: "active",
	created_at: "2026-09-01T10:00:00Z",
};

/** An active fixed allowance plan the fake API lists. */
export const freePlan: PlanResponse = {
	id: "pln_01jbvagescfn78y0938nkrkay2",
	name: "Free",
	mode: "fixed_allowance",
	target_margin: "0.0000",
	allowance: "2.000000000",
	customer_count: 40,
	hold_times: {},
	status: "active",
	created_at: "2026-09-02T10:00:00Z",
};

/** A page holding creatorPlan and freePlan. */
export const planPage: PageBodyPlanResponse = {
	items: [creatorPlan, freePlan],
	next_cursor: null,
};

/** Settings of the live environment, whose default plan is creatorPlan. */
export const liveSettings: SettingsResponse = {
	...installationSettings,
	default_plan_id: creatorPlan.id,
};

/** A member who has not accepted their invite yet. */
export const invitedMember: MemberResponse = {
	id: "mem_01jbvagescfn78y0938nkrkay2",
	email: "jordan@example.com",
	display_name: "Jordan Lee",
	has_password: false,
	status: "active",
	last_login_at: null,
	created_at: "2026-09-10T00:00:00Z",
};

/** A removed member. */
export const removedMember: MemberResponse = {
	id: "mem_01jbvagescfn78y0938nkrkay3",
	email: "alex@example.com",
	display_name: "Alex Kim",
	has_password: true,
	status: "disabled",
	last_login_at: "2026-09-12T09:30:00Z",
	created_at: "2026-09-05T00:00:00Z",
};

/** The signed-in member, the invited member and the removed member. */
export const memberPage: PageBodyMemberResponse = {
	items: [signedInMember, removedMember, invitedMember],
	next_cursor: null,
};

/** The answer to adding Morgan Ellis, with their invite link. */
export const addedMember: AddedMemberResponse = {
	member: {
		id: "mem_01jbvagescfn78y0938nkrkay4",
		email: "morgan@example.com",
		display_name: "Morgan Ellis",
		has_password: false,
		status: "active",
		last_login_at: null,
		created_at: "2026-09-26T00:00:00Z",
	},
	link_url: "http://localhost:8480/link#invite-link-for-tests",
};

/** The answer to a reset link request. */
export const resetLink: ResetLinkResponse = {
	link_url: "http://localhost:8480/link#reset-link-for-tests",
};

/** Answers a JSON body with status. */
export function statusAnswer(status: number, body: unknown): () => Response {
	return () =>
		new Response(JSON.stringify(body), {
			status,
			headers: { "Content-Type": "application/json" },
		});
}

/** Answers a problem with status, code and the server's detail. */
export function problemWithDetailAnswer(
	status: number,
	code: string,
	detail: string,
): () => Response {
	return () =>
		new Response(
			JSON.stringify({
				type: `https://github.com/preburn/preburn/blob/main/docs/errors.md#${code}`,
				title: "Error",
				status,
				detail,
				code,
			}),
			{ status, headers: { "Content-Type": "application/problem+json" } },
		);
}

/** Answers 204 without a body. */
export function noContentAnswer(): Response {
	return new Response(null, { status: 204 });
}
