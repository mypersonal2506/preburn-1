import { createFileRoute, stripSearchParams } from "@tanstack/react-router";
import { SdkPage } from "@/features/developers/sdk-page";
import {
	sdkSearchDefaults,
	sdkSearchSchema,
} from "@/features/developers/sdk-search";

export const Route = createFileRoute("/_app/developers/sdk")({
	staticData: { title: "SDK" },
	validateSearch: sdkSearchSchema,
	search: { middlewares: [stripSearchParams(sdkSearchDefaults)] },
	component: SdkPage,
});
