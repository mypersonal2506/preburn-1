import type { ReactNode } from "react";

interface GalleryRowProps {
	label: string;
	children: ReactNode;
}

export function GalleryRow({ label, children }: GalleryRowProps) {
	return (
		<div className="grid gap-2 md:grid-cols-[10rem_1fr] md:items-center">
			<span className="text-sm text-muted-foreground">{label}</span>
			<div className="flex min-w-0 flex-wrap items-center gap-3">
				{children}
			</div>
		</div>
	);
}
