import { createFileRoute } from "@tanstack/react-router";
import { LoginPage } from "@/features/auth/login-page";

export const Route = createFileRoute("/_auth/login")({
	staticData: { title: "Log in" },
	component: LoginPage,
});
