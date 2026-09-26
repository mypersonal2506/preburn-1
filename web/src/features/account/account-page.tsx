import { PageHeader } from "@/components/page-header";
import { AccountNameForm } from "@/features/account/account-name-form";
import { AccountPasswordForm } from "@/features/account/account-password-form";

export function AccountPage() {
	return (
		<>
			<PageHeader title="Account" />
			<div className="flex max-w-xl flex-col gap-6">
				<AccountNameForm />
				<AccountPasswordForm />
			</div>
		</>
	);
}
