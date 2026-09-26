import { Link } from "@tanstack/react-router";
import type { AttentionResponse } from "@/client";
import { AttentionBanner } from "@/components/attention-banner";
import { Button } from "@/components/ui/button";
import { strongestAttention } from "@/features/overview/overview-attention";

interface OverviewAttentionBannerProps {
	attention: AttentionResponse;
}

/**
 * The overview's AttentionBanner with the strongest message of attention
 * and a link to the list behind it, or nothing when no count needs
 * attention.
 */
export function OverviewAttentionBanner({
	attention,
}: OverviewAttentionBannerProps) {
	const message = strongestAttention(attention);
	if (message === null) {
		return null;
	}
	return (
		<AttentionBanner
			title={message.title}
			description={message.description}
			action={
				message.action === null ? undefined : (
					<Button variant="outline" size="sm" asChild>
						<Link {...message.action.link}>{message.action.label}</Link>
					</Button>
				)
			}
		/>
	);
}
