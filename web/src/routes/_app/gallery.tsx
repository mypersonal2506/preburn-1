import { createFileRoute, notFound } from "@tanstack/react-router";
import { lazy } from "react";

// The import sits in a DEV-only branch so production builds drop the gallery.
const DevelopmentGallery = import.meta.env.DEV
	? lazy(() =>
			import("@/features/gallery/component-gallery").then((module) => ({
				default: module.ComponentGallery,
			})),
		)
	: null;

export const Route = createFileRoute("/_app/gallery")({
	staticData: { title: "Gallery" },
	beforeLoad: () => {
		if (import.meta.env.PROD) {
			throw notFound();
		}
	},
	component: GalleryPage,
});

function GalleryPage() {
	return DevelopmentGallery !== null && <DevelopmentGallery />;
}
