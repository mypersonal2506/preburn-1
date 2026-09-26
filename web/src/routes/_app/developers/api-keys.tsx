import { createFileRoute } from "@tanstack/react-router";
import { ApiKeysPage } from "@/features/developers/api-keys-page";

export const Route = createFileRoute("/_app/developers/api-keys")({
	staticData: { title: "API keys" },
	component: ApiKeysPage,
});
