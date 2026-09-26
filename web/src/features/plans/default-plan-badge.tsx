import { Badge } from "@/components/ui/badge";

/**
 * Marks the default plan of the environment, which customers without a
 * plan follow.
 */
export function DefaultPlanBadge() {
	return <Badge variant="outline">Default</Badge>;
}
