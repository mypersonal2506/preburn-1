import { cn } from "cn";
import type {
	ApiKeyResponse,
	DecisionResponse,
	MemberResponse,
	ModelResponse,
	PlanResponse,
	PolicyResponse,
} from "@/client";

type RecordStatus =
	| ApiKeyResponse["status"]
	| DecisionResponse["status"]
	| MemberResponse["status"]
	| ModelResponse["status"]
	| PlanResponse["status"]
	| PolicyResponse["status"];

interface StatusDotProps {
	status: RecordStatus;
}

type StatusDisplay = {
	label: string;
	dotClass: string;
};

const STATUS_DISPLAYS: Record<RecordStatus, StatusDisplay> = {
	active: { label: "Active", dotClass: "bg-success" },
	disabled: { label: "Disabled", dotClass: "bg-muted-foreground" },
	archived: { label: "Archived", dotClass: "bg-muted-foreground" },
	deprecated: { label: "Deprecated", dotClass: "bg-warning" },
	reserved: { label: "Reserved", dotClass: "bg-foreground" },
	settled: { label: "Settled", dotClass: "bg-success" },
	released: { label: "Released", dotClass: "bg-muted-foreground" },
	expired: { label: "Expired", dotClass: "bg-warning" },
	unreserved: { label: "Unreserved", dotClass: "bg-muted-foreground" },
};

/**
 * A status label with a colored dot, the one status mapping of the
 * dashboard: success for active and settled, warning for deprecated and
 * expired, ink for reserved, muted for the rest.
 */
export function StatusDot({ status }: StatusDotProps) {
	const display = STATUS_DISPLAYS[status];
	return (
		<span className="inline-flex items-center gap-1.5">
			<span
				aria-hidden
				className={cn("size-1.5 rounded-full", display.dotClass)}
			/>
			{display.label}
		</span>
	);
}
