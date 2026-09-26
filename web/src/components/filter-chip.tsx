import { cn } from "cn";
import { XIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";

interface FilterChipProps
	extends Omit<ComponentProps<"button">, "children" | "aria-pressed"> {
	label: string;
	value?: string;
	pressed?: boolean;
	onClear?: () => void;
}

/**
 * A rounded filter button. As a toggle, pressed sets aria-pressed, such as
 * for All, Paying and Free. As a filter with a choice, it reads
 * "{label}: {value}" and, with onClear, gets a "Clear {label}" button. The
 * remaining button props reach the chip button, so it works as the asChild
 * trigger of a popover or menu.
 */
export function FilterChip({
	label,
	value,
	pressed,
	onClear,
	className,
	...buttonProps
}: FilterChipProps) {
	const clearable = value !== undefined && onClear !== undefined;
	return (
		<span className="inline-flex items-center">
			<Button
				type="button"
				variant={
					pressed === true || value !== undefined ? "secondary" : "outline"
				}
				size="sm"
				aria-pressed={pressed}
				className={cn("rounded-full", clearable && "rounded-r-none", className)}
				{...buttonProps}
			>
				{value === undefined ? label : `${label}: ${value}`}
			</Button>
			{clearable && (
				<Button
					type="button"
					variant="secondary"
					size="icon-sm"
					aria-label={`Clear ${label}`}
					className="rounded-l-none rounded-r-full"
					onClick={onClear}
				>
					<XIcon />
				</Button>
			)}
		</span>
	);
}
