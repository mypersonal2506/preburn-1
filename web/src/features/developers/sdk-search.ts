import * as z from "zod";

/** The code language tabs of the SDK page, in tab order. */
export const sdkLanguageSchema = z.enum(["python", "curl"]);

/** A code language tab of the SDK page. */
export type SdkLanguage = z.output<typeof sdkLanguageSchema>;

/** The SDK page's language tab as a URL search param, python when omitted. */
export const sdkSearchSchema = z.object({
	language: sdkLanguageSchema.default("python"),
});

/** The search params of the Python tab, left out of its URL. */
export const sdkSearchDefaults = sdkSearchSchema.parse({});
