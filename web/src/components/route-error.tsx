import { useQueryErrorResetBoundary } from "@tanstack/react-query";
import { type ErrorComponentProps, useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { Card, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";

/**
 * Error state of a dashboard route: a card reading "Something went wrong"
 * with a Try again button that reloads the route. The raw message goes to the
 * console as `route error message=...`. The router shows it in place of a
 * route whose loading or rendering failed, so a page of the app shell keeps
 * the sidebar and header around it.
 */
export function RouteError({ error }: ErrorComponentProps) {
	const router = useRouter();
	const queryErrorResetBoundary = useQueryErrorResetBoundary();

	useEffect(() => {
		const message = error instanceof Error ? error.message : String(error);
		console.error(`route error message=${message}`);
	}, [error]);

	const retry = () => {
		// Without this reset a failed suspense query rethrows its cached error.
		queryErrorResetBoundary.reset();
		void router.invalidate();
	};

	return (
		<Card className="mx-auto w-full max-w-md">
			<CardHeader>
				<CardTitle>
					<h2>Something went wrong</h2>
				</CardTitle>
			</CardHeader>
			<CardFooter>
				<Button variant="outline" onClick={retry}>
					Try again
				</Button>
			</CardFooter>
		</Card>
	);
}
