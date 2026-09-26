import { UsersIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CodePanel } from "@/components/code-panel";
import { ConfirmModal } from "@/components/confirm-modal";
import { CustomerLabel } from "@/components/customer-label";
import { EmptyState } from "@/components/empty-state";
import { FeatureLabel } from "@/components/feature-label";
import { FormModal } from "@/components/form-modal";
import { HelpTip } from "@/components/help-tip";
import { KeyValueList } from "@/components/key-value-list";
import { SectionCard } from "@/components/section-card";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { GalleryRow } from "@/features/gallery/gallery-row";

const PYTHON_SAMPLE = [
	"decision = preburn.check(",
	'    customer_id="acme",',
	'    feature="text_to_video",',
	'    provider="fal_ai",',
	'    model="fal-ai/veo3.1/fast",',
	")",
].join("\n");
const CURL_SAMPLE = [
	"curl -X POST http://localhost:8480/api/v1/check \\",
	'  -H "Authorization: Bearer $PREBURN_API_KEY" \\',
	`  -d '{"customer_id": "acme", "feature": "chat"}'`,
].join("\n");
const PLAN_NAME_FIELD = "gallery-plan-name";

export function GalleryDetailSection() {
	const [formOpen, setFormOpen] = useState(false);
	const [confirmOpen, setConfirmOpen] = useState(false);
	const [planName, setPlanName] = useState("Creator");

	return (
		<SectionCard title="Details">
			<div className="flex flex-col gap-6">
				<KeyValueList
					items={[
						{
							label: "Customer",
							value: <CustomerLabel displayName="Acme" externalId="acme" />,
						},
						{ label: "Plan", value: "Creator" },
						{
							label: "Feature",
							value: <FeatureLabel feature="text_to_video" />,
						},
						{ label: "Matched policy", value: null },
					]}
				/>
				<CodePanel
					snippets={[
						{ label: "Python", code: PYTHON_SAMPLE },
						{ label: "curl", code: CURL_SAMPLE },
					]}
				/>
				<EmptyState
					icon={UsersIcon}
					title="No customers yet"
					description="Customers appear after their first check."
					action={<Button size="sm">Get started</Button>}
				/>
				<GalleryRow label="Help">
					<span className="inline-flex items-center gap-1 text-sm">
						Reservation
						<HelpTip topic="Reservation">
							Cost held for a request until its usage is reported.
						</HelpTip>
					</span>
				</GalleryRow>
				<GalleryRow label="Modals">
					<Button variant="outline" size="sm" onClick={() => setFormOpen(true)}>
						Rename plan
					</Button>
					<Button
						variant="outline"
						size="sm"
						onClick={() => setConfirmOpen(true)}
					>
						Revoke key
					</Button>
				</GalleryRow>
			</div>
			<FormModal
				open={formOpen}
				onOpenChange={setFormOpen}
				title="Rename plan"
				submitLabel="Save"
				pending={false}
				onSubmit={() => {
					setFormOpen(false);
					toast.success("Plan renamed");
				}}
			>
				<Field>
					<FieldLabel htmlFor={PLAN_NAME_FIELD}>Name</FieldLabel>
					<Input
						id={PLAN_NAME_FIELD}
						value={planName}
						onChange={(event) => setPlanName(event.target.value)}
					/>
				</Field>
			</FormModal>
			<ConfirmModal
				open={confirmOpen}
				onOpenChange={setConfirmOpen}
				title="Revoke API key?"
				description="Requests that send this key stop working."
				confirmLabel="Revoke"
				pending={false}
				onConfirm={() => {
					setConfirmOpen(false);
					toast.success("Key revoked");
				}}
			/>
		</SectionCard>
	);
}
