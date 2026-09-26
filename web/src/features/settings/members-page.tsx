import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { TriangleAlertIcon, UsersIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import type { AddedMemberResponse, MemberResponse } from "@/client";
import {
	createResetLinkMutation,
	listMembersOptions,
	listMembersQueryKey,
	removeMemberMutation,
} from "@/client/@tanstack/react-query.gen";
import { ConfirmModal } from "@/components/confirm-modal";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { RelativeTime } from "@/components/relative-time";
import { SectionCard } from "@/components/section-card";
import { StatusDot } from "@/components/status-dot";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { AddMemberModal } from "@/features/settings/add-member-modal";
import { MemberLinkModal } from "@/features/settings/member-link-modal";
import { SettingsLayout } from "@/features/settings/settings-layout";
import type { CodeMessages } from "@/lib/form-problem";

const REMOVE_CODE_MESSAGES: CodeMessages = {
	last_member: () =>
		"Cannot remove yourself or the last member who can log in.",
};

export function MembersPage() {
	const queryClient = useQueryClient();
	const pagination = useCursorPagination("");
	const membersQuery = useQuery(
		listMembersOptions({ query: { cursor: pagination.cursor } }),
	);
	const [adding, setAdding] = useState(false);
	const [invitation, setInvitation] = useState<AddedMemberResponse | null>(
		null,
	);
	const [removalMember, setRemovalMember] = useState<MemberResponse | null>(
		null,
	);
	const [resetLinkMember, setResetLinkMember] = useState<MemberResponse | null>(
		null,
	);
	const removeMember = useMutation({
		...removeMemberMutation(),
		onSuccess: async () => {
			setRemovalMember(null);
			toast.success("Member removed");
			await queryClient.invalidateQueries({ queryKey: listMembersQueryKey() });
		},
	});
	const createResetLink = useMutation({
		...createResetLinkMutation(),
		gcTime: 0,
	});

	function closeRemoval(): void {
		setRemovalMember(null);
		removeMember.reset();
	}

	function requestResetLink(member: MemberResponse): void {
		setResetLinkMember(member);
		createResetLink.mutate({ path: { member_id: member.id } });
	}

	function closeResetLink(): void {
		setResetLinkMember(null);
		createResetLink.reset();
	}

	function emptyState(): ReactNode {
		if (membersQuery.isError) {
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => membersQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		return <EmptyState icon={UsersIcon} title="No members" />;
	}

	return (
		<SettingsLayout>
			<SectionCard
				title="Members"
				action={<Button onClick={() => setAdding(true)}>Add member</Button>}
			>
				<DataTable
					label="Members"
					columns={memberColumns(new Date())}
					rows={membersQuery.isError ? [] : membersQuery.data?.items}
					rowKey={(member) => member.id}
					rowMenu={(member) => (
						<>
							<DropdownMenuItem
								disabled={member.status === "disabled"}
								onSelect={() => requestResetLink(member)}
							>
								Reset link
							</DropdownMenuItem>
							<DropdownMenuItem
								variant="destructive"
								disabled={member.status === "disabled"}
								onSelect={() => setRemovalMember(member)}
							>
								Remove
							</DropdownMenuItem>
						</>
					)}
					emptyState={emptyState()}
					pagination={pagination}
					nextCursor={membersQuery.data?.next_cursor}
				/>
			</SectionCard>
			<AddMemberModal
				open={adding}
				onOpenChange={setAdding}
				onAdded={(addedMember) => {
					setAdding(false);
					setInvitation(addedMember);
				}}
			/>
			<MemberLinkModal
				title="Invite link"
				member={invitation?.member ?? null}
				linkUrl={invitation?.link_url}
				error={null}
				onClose={() => setInvitation(null)}
			/>
			<MemberLinkModal
				title="Reset link"
				member={resetLinkMember}
				linkUrl={createResetLink.data?.link_url}
				error={createResetLink.error}
				onClose={closeResetLink}
			/>
			{removalMember !== null && (
				<ConfirmModal
					open
					onOpenChange={(open) => {
						if (!open) {
							closeRemoval();
						}
					}}
					title={`Remove ${removalMember.display_name}`}
					description="They are signed out and can no longer log in."
					confirmLabel="Remove"
					pending={removeMember.isPending}
					error={removeMember.error}
					codeMessages={REMOVE_CODE_MESSAGES}
					onConfirm={() =>
						removeMember.mutate({ path: { member_id: removalMember.id } })
					}
				/>
			)}
		</SettingsLayout>
	);
}

function memberColumns(now: Date): DataTableColumn<MemberResponse>[] {
	return [
		{
			header: "Name",
			cell: (member) => (
				<span className="inline-flex items-center gap-2">
					{member.display_name}
					{member.status === "active" && !member.has_password && (
						<Badge variant="outline">Invited</Badge>
					)}
				</span>
			),
		},
		{ header: "Email", cell: (member) => member.email },
		{
			header: "Status",
			cell: (member) => <StatusDot status={member.status} />,
		},
		{
			header: "Last login",
			cell: (member) =>
				member.last_login_at === null ? (
					"Never"
				) : (
					<RelativeTime timestamp={member.last_login_at} now={now} />
				),
		},
	];
}
