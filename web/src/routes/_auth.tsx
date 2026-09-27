import { createFileRoute, Outlet } from "@tanstack/react-router";
import { PreburnMark } from "@/components/preburn-mark";
import { Card, CardContent } from "@/components/ui/card";

export const Route = createFileRoute("/_auth")({
	component: AuthLayout,
});

function AuthLayout() {
	return (
		<div className="flex min-h-svh flex-col items-center justify-center gap-6 bg-background p-6">
			<div className="flex items-center gap-2">
				<PreburnMark className="size-6" />
				<span className="text-lg font-semibold tracking-tight">Preburn</span>
			</div>
			<Card className="w-full max-w-sm">
				<CardContent>
					<Outlet />
				</CardContent>
			</Card>
		</div>
	);
}
