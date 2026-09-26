import { TriangleAlertIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
	Alert,
	AlertAction,
	AlertDescription,
	AlertTitle,
} from "@/components/ui/alert";

interface AttentionBannerProps {
	title: string;
	description?: string;
	action?: ReactNode;
}

/**
 * The one message that most needs attention, with a warning icon, an
 * optional muted second line and one action. Pages pick the strongest
 * message and show one banner.
 */
export function AttentionBanner({
	title,
	description,
	action,
}: AttentionBannerProps) {
	return (
		<Alert className="*:[svg]:text-warning">
			<TriangleAlertIcon />
			<AlertTitle>{title}</AlertTitle>
			{description !== undefined && (
				<AlertDescription>{description}</AlertDescription>
			)}
			{action !== undefined && <AlertAction>{action}</AlertAction>}
		</Alert>
	);
}
