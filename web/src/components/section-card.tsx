import type { ReactNode } from "react";
import {
	Card,
	CardAction,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";

interface SectionCardProps {
	title: string;
	description?: string;
	action?: ReactNode;
	className?: string;
	children: ReactNode;
}

/**
 * A detail page card with a title, an optional description that carries
 * data or a unit ("Per UTC day"), an optional header action and its body.
 */
export function SectionCard({
	title,
	description,
	action,
	className,
	children,
}: SectionCardProps) {
	return (
		<Card className={className}>
			<CardHeader>
				<CardTitle>
					<h2>{title}</h2>
				</CardTitle>
				{description !== undefined && (
					<CardDescription>{description}</CardDescription>
				)}
				{action !== undefined && <CardAction>{action}</CardAction>}
			</CardHeader>
			<CardContent>{children}</CardContent>
		</Card>
	);
}
