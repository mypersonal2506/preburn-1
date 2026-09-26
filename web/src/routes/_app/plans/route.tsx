import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/plans")({
	staticData: { title: "Plans" },
});
