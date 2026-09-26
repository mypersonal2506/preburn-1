import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import { Card, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";

/**
 * Not-found state of the dashboard: a card reading "Page not found" with a
 * link to Overview. The router shows it for a path that no route matches and
 * for a route that throws `notFound()`, inside the app shell when the path
 * belongs to a section of the shell.
 */
export function RouteNotFound() {
	return (
		<Card className="mx-auto w-full max-w-md">
			<CardHeader>
				<CardTitle>
					<h2>Page not found</h2>
				</CardTitle>
			</CardHeader>
			<CardFooter>
				<Button variant="outline" asChild>
					<Link to="/">Go to Overview</Link>
				</Button>
			</CardFooter>
		</Card>
	);
}
