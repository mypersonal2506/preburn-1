import { useState } from "react";
import { toast } from "sonner";
import { AmountInput } from "@/components/inputs/amount-input";
import { PaceInput } from "@/components/inputs/pace-input";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import { SectionCard } from "@/components/section-card";
import { AddClause } from "@/components/sentence/add-clause";
import { Blank } from "@/components/sentence/blank";
import { BlankPhrase } from "@/components/sentence/blank-phrase";
import {
	type ConditionCatalog,
	type ConditionGroup,
	ConditionPanel,
} from "@/components/sentence/condition-panel";
import { SentenceCard } from "@/components/sentence/sentence-card";
import {
	type SentenceFactQuery,
	SentenceFacts,
} from "@/components/sentence/sentence-facts";
import { formatMoney } from "@/lib/format";

type GallerySignal = "pace" | "allowance_remaining";

const SIGNAL_CATALOG: ConditionCatalog<GallerySignal> = [
	{ signal: "pace", phrase: "pace", input: PaceInput },
	{
		signal: "allowance_remaining",
		phrase: "allowance left",
		input: AmountInput,
	},
];
const MATCHING_CUSTOMERS: SentenceFactQuery = {
	data: "12 of 40",
	status: "success",
	fetchStatus: "idle",
};
const COST_AVOIDED: SentenceFactQuery = {
	data: undefined,
	status: "pending",
	fetchStatus: "fetching",
};

export function GallerySentenceSection() {
	const [feature, setFeature] = useState<string | null>("text_to_video");
	const [conditions, setConditions] = useState<ConditionGroup<GallerySignal>>({
		all: [{ signal: "pace", operator: "gt", value: "1.5000" }],
	});
	const [limitAdded, setLimitAdded] = useState(false);
	const [limit, setLimit] = useState<string | null>(null);
	const [name, setName] = useState("Heavy video users");
	const [active, setActive] = useState(true);

	return (
		<SectionCard title="Sentence">
			<SentenceCard
				sentence={
					<>
						When{" "}
						<FeaturePicker
							value={feature}
							onChange={setFeature}
							trigger={(label) => (
								<BlankPhrase placeholder="any feature" phrase={label} />
							)}
						/>{" "}
						requests come from customers matching{" "}
						<Blank
							placeholder="which conditions"
							phrase={conditionsPhrase(conditions)}
							wide
						>
							<ConditionPanel
								catalog={SIGNAL_CATALOG}
								value={conditions}
								onChange={setConditions}
							/>
						</Blank>
						, deny them.
						{limitAdded && (
							<>
								{" "}
								Stop at{" "}
								<Blank
									placeholder="how much"
									phrase={limit === null ? null : formatMoney(limit)}
									defaultOpen
								>
									<AmountInput
										aria-label="Limit"
										value={limit}
										onChange={setLimit}
									/>
								</Blank>
								.
							</>
						)}{" "}
						<AddClause
							clauses={
								limitAdded
									? []
									: [{ label: "Spend limit", onAdd: () => setLimitAdded(true) }]
							}
						/>
					</>
				}
				facts={
					<SentenceFacts
						facts={[
							{ label: "Matching customers", query: MATCHING_CUSTOMERS },
							{ label: "Cost avoided", query: COST_AVOIDED },
						]}
					/>
				}
				errors={[]}
				name={{ value: name, onChange: setName, invalid: false }}
				status={{ active, onActiveChange: setActive }}
				advanced={{
					summary: "Soft, allow if unreachable or unpriced",
					content: <p className="text-sm">Enforcement settings</p>,
				}}
				action={{ kind: "create", label: "Create policy" }}
				pending={false}
				onSubmit={() => toast.success("Policy created")}
			/>
		</SectionCard>
	);
}

function conditionsPhrase(group: ConditionGroup<GallerySignal>): string | null {
	const members = group.all ?? group.any;
	if (members === undefined || members.length === 0) {
		return null;
	}
	return members.length === 1 ? "1 condition" : `${members.length} conditions`;
}
