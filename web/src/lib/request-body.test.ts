import { expect, test } from "vitest";
import { zCreatePlanBody } from "@/client/zod.gen";
import { parseRequestBody } from "@/lib/request-body";

test("a body matching its schema comes back unchanged, int64 values as numbers", () => {
	const body = {
		name: "Creator",
		mode: "margin_target" as const,
		target_margin: "0.4000",
		hold_times: { text_to_video: 600 },
	};

	const checkedBody = parseRequestBody(zCreatePlanBody, body);

	expect(checkedBody).toBe(body);
	expect(checkedBody.hold_times?.text_to_video).toBe(600);
});

test("a body that drifted from its schema throws naming the paths", () => {
	expect(() =>
		parseRequestBody(zCreatePlanBody, {
			name: "Creator",
			mode: "margin_target",
			target_margin: "0.4000",
			hold_times: { text_to_video: 1.5 },
		}),
	).toThrow("request schema mismatch path=hold_times.text_to_video");
});
