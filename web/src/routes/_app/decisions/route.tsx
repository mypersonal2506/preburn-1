import { createFileRoute } from "@tanstack/react-router";

export const Route = createFileRoute("/_app/decisions")({
	staticData: { title: "Decisions" },
});
