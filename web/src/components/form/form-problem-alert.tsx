import { Alert, AlertDescription } from "@/components/ui/alert";

interface FormProblemAlertProps {
	messages: readonly string[] | undefined;
}

/**
 * The form list of a form's problem, `FormProblem.form`, as a destructive
 * alert with one line per message. Renders nothing without messages.
 */
export function FormProblemAlert({ messages }: FormProblemAlertProps) {
	if (messages === undefined) {
		return null;
	}
	return (
		<Alert variant="destructive">
			<AlertDescription>
				{messages.map((message) => (
					<p key={message}>{message}</p>
				))}
			</AlertDescription>
		</Alert>
	);
}
