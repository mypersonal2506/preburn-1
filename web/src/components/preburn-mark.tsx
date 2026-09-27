import { cn } from "cn";

/** Props of PreburnMark. */
export interface PreburnMarkProps {
	/** Classes for the svg, such as its size. */
	className?: string;
}

/**
 * The Preburn mark: a stem forking into two paths with a dot on the right
 * one. The paths take the foreground color and the dot the ring color, so
 * the mark follows the light and dark theme. It is decorative and hidden
 * from assistive technology, so place it next to the "Preburn" wordmark.
 */
export function PreburnMark({ className }: PreburnMarkProps) {
	return (
		<svg
			data-slot="preburn-mark"
			viewBox="0 0 64 64"
			aria-hidden="true"
			className={cn("shrink-0", className)}
		>
			<g
				fill="none"
				strokeWidth={7}
				strokeLinecap="round"
				className="stroke-foreground"
			>
				<path d="M32 58 L32 38 C32 28 20 28 20 18" strokeLinejoin="round" />
				<path d="M32 38 C32 28 44 28 44 20" />
			</g>
			<circle cx={44} cy={12} r={7} className="fill-ring" />
		</svg>
	);
}
