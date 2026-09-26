import { PageHeader } from "@/components/page-header";
import { GalleryDetailSection } from "@/features/gallery/gallery-detail-section";
import { GalleryHeaderSection } from "@/features/gallery/gallery-header-section";
import { GalleryInputSection } from "@/features/gallery/gallery-input-section";
import { GalleryLiveSection } from "@/features/gallery/gallery-live-section";
import { GalleryPickerSection } from "@/features/gallery/gallery-picker-section";
import { GallerySentenceSection } from "@/features/gallery/gallery-sentence-section";
import { GalleryStatusSection } from "@/features/gallery/gallery-status-section";
import { GalleryTableSection } from "@/features/gallery/gallery-table-section";

export function ComponentGallery() {
	return (
		<>
			<PageHeader title="Gallery" />
			<div className="flex flex-col gap-6">
				<GalleryHeaderSection />
				<GalleryStatusSection />
				<GalleryDetailSection />
				<GalleryTableSection />
				<GalleryInputSection />
				<GalleryPickerSection />
				<GallerySentenceSection />
				<GalleryLiveSection />
			</div>
		</>
	);
}
