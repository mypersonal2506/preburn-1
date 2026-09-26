import { PlusIcon, XIcon } from "lucide-react";
import { type ReactElement, useRef } from "react";
import { HelpTip } from "@/components/help-tip";
import { DurationInput } from "@/components/inputs/duration-input";
import { FeaturePicker } from "@/components/pickers/feature-picker";
import { Button } from "@/components/ui/button";
import type { HoldTimeRow } from "@/features/plans/plan-draft";
import type { HoldTimeProblem } from "@/features/plans/plan-problems";

/** Props of PlanHoldTimes. */
export interface PlanHoldTimesProps {
	rows: readonly HoldTimeRow[];
	problems: readonly (HoldTimeProblem | null)[];
	onChange: (rows: HoldTimeRow[]) => void;
}

interface RowKeys {
	keyOf: (row: HoldTimeRow) => string;
	carry: (row: HoldTimeRow, nextRow: HoldTimeRow) => void;
}

/**
 * The Advanced settings of the plan sentence: the reservation hold times, a
 * row per feature with a FeaturePicker, a DurationInput and a remove button,
 * then Add hold time for an empty row. `problems` holds a row's problem at
 * its index: it marks the control it names and shows under the row.
 */
export function PlanHoldTimes({
	rows,
	problems,
	onChange,
}: PlanHoldTimesProps): ReactElement {
	const rowKeys = useRowKeys();

	function replaceRow(
		index: number,
		row: HoldTimeRow,
		nextRow: HoldTimeRow,
	): void {
		rowKeys.carry(row, nextRow);
		onChange(rows.with(index, nextRow));
	}

	return (
		<div className="flex flex-col gap-3">
			<div className="flex items-center gap-1 font-medium text-sm">
				Hold times
				<HelpTip topic="hold times">
					How long a check holds its cost. Default 10 minutes.
				</HelpTip>
			</div>
			<div className="flex max-h-96 flex-col gap-4 overflow-y-auto">
				{rows.map((row, index) => {
					const problem = problems[index] ?? null;
					return (
						<div key={rowKeys.keyOf(row)} className="flex flex-col gap-2">
							<div className="flex items-center gap-1">
								<div className="min-w-0 flex-1">
									<FeaturePicker
										value={row.feature}
										invalid={problem?.part === "feature"}
										onChange={(feature) =>
											replaceRow(index, row, { ...row, feature })
										}
									/>
								</div>
								<Button
									type="button"
									variant="ghost"
									size="icon-xs"
									aria-label="Remove hold time"
									onClick={() => onChange(rows.toSpliced(index, 1))}
								>
									<XIcon />
								</Button>
							</div>
							<DurationInput
								aria-label="Hold time"
								value={row.seconds}
								invalid={problem?.part === "seconds"}
								onChange={(seconds) =>
									replaceRow(index, row, { ...row, seconds })
								}
							/>
							{problem !== null && (
								<p className="text-destructive text-xs">{problem.message}</p>
							)}
						</div>
					);
				})}
			</div>
			<Button
				type="button"
				variant="ghost"
				size="xs"
				className="self-start"
				onClick={() => onChange([...rows, { feature: null, seconds: null }])}
			>
				<PlusIcon />
				Add hold time
			</Button>
		</div>
	);
}

function useRowKeys(): RowKeys {
	const keys = useRef(new WeakMap<HoldTimeRow, string>());
	const keyCount = useRef(0);

	function keyOf(row: HoldTimeRow): string {
		const key = keys.current.get(row);
		if (key !== undefined) {
			return key;
		}
		keyCount.current += 1;
		const newKey = String(keyCount.current);
		keys.current.set(row, newKey);
		return newKey;
	}

	function carry(row: HoldTimeRow, nextRow: HoldTimeRow): void {
		keys.current.set(nextRow, keyOf(row));
	}

	return { keyOf, carry };
}
