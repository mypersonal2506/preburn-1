import { cn } from "cn";
import { CircleCheckIcon, CircleIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Card } from "@/components/ui/card";

/** One step of the Get started checklist and what it shows while expanded. */
export interface OnboardingStep {
	title: string;
	done: boolean;
	content: ReactNode;
}

interface OnboardingChecklistProps {
	steps: readonly OnboardingStep[];
}

/**
 * The Get started checklist: every step in order with a done mark, and only
 * the first step that is not done expanded to show its content, marked as
 * the current step.
 */
export function OnboardingChecklist({ steps }: OnboardingChecklistProps) {
	const expandedStep = steps.find((step) => !step.done);

	return (
		<Card className="py-0">
			<ol aria-label="Setup steps" className="divide-y">
				{steps.map((step) => {
					const expanded = step === expandedStep;
					const StepIcon = step.done ? CircleCheckIcon : CircleIcon;
					return (
						<li
							key={step.title}
							aria-current={expanded ? "step" : undefined}
							className="flex gap-3 p-4"
						>
							<StepIcon
								aria-hidden
								className={cn(
									"mt-0.5 size-4 shrink-0",
									step.done ? "text-success" : "text-muted-foreground",
								)}
							/>
							<div className="flex min-w-0 flex-1 flex-col gap-3">
								<h2
									className={cn(
										"font-medium",
										step.done && "text-muted-foreground",
									)}
								>
									{step.title}
								</h2>
								{step.done && <span className="sr-only">Done</span>}
								{expanded && step.content}
							</div>
						</li>
					);
				})}
			</ol>
		</Card>
	);
}
