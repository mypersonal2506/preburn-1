import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon, PlusIcon, TriangleAlertIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";
import type { ApiKeyResponse } from "@/client";
import {
	getDashboardOnboardingQueryKey,
	listApiKeysOptions,
	listApiKeysQueryKey,
	revokeApiKeyMutation,
} from "@/client/@tanstack/react-query.gen";
import { ConfirmModal } from "@/components/confirm-modal";
import { DataTable, type DataTableColumn } from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { PageHeader } from "@/components/page-header";
import { RelativeTime } from "@/components/relative-time";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useCursorPagination } from "@/components/use-cursor-pagination";
import { API_KEY_SCOPE_DISPLAYS } from "@/features/developers/api-key-scopes";
import { CreateApiKeyModal } from "@/features/developers/create-api-key-modal";

const MASKED_SECRET_PREFIX = "••••";
const UNFILTERED_LIST_KEY = "";

export function ApiKeysPage() {
	const queryClient = useQueryClient();
	const [createOpen, setCreateOpen] = useState(false);
	const [revokingKey, setRevokingKey] = useState<ApiKeyResponse | null>(null);
	const pagination = useCursorPagination(UNFILTERED_LIST_KEY);
	const keysQuery = useQuery(
		listApiKeysOptions({ query: { cursor: pagination.cursor } }),
	);
	const revokeKey = useMutation(revokeApiKeyMutation());

	function closeRevoke(): void {
		setRevokingKey(null);
		revokeKey.reset();
	}

	function revoke(apiKey: ApiKeyResponse): void {
		revokeKey.mutate(
			{ path: { api_key_id: apiKey.id } },
			{
				onSuccess: () => {
					toast.success("API key revoked");
					closeRevoke();
					void queryClient.invalidateQueries({
						queryKey: listApiKeysQueryKey(),
					});
					void queryClient.invalidateQueries({
						queryKey: getDashboardOnboardingQueryKey(),
					});
				},
			},
		);
	}

	function emptyState(): ReactNode {
		if (keysQuery.isError) {
			return (
				<EmptyState
					icon={TriangleAlertIcon}
					title="Something went wrong"
					action={
						<Button variant="outline" onClick={() => keysQuery.refetch()}>
							Try again
						</Button>
					}
				/>
			);
		}
		return (
			<EmptyState
				icon={KeyRoundIcon}
				title="No API keys yet"
				description="Create one to send checks."
				action={
					<Button onClick={() => setCreateOpen(true)}>Create API key</Button>
				}
			/>
		);
	}

	return (
		<>
			<PageHeader
				title="API keys"
				actions={
					<Button onClick={() => setCreateOpen(true)}>
						<PlusIcon />
						Create API key
					</Button>
				}
			/>
			<DataTable
				label="API keys"
				columns={apiKeyColumns(new Date())}
				rows={keysQuery.isError ? [] : keysQuery.data?.items}
				rowKey={(apiKey) => apiKey.id}
				rowMenu={(apiKey) => (
					<DropdownMenuItem
						variant="destructive"
						disabled={apiKey.status === "disabled"}
						onSelect={() => setRevokingKey(apiKey)}
					>
						Revoke
					</DropdownMenuItem>
				)}
				emptyState={emptyState()}
				pagination={pagination}
				nextCursor={keysQuery.data?.next_cursor}
			/>
			<CreateApiKeyModal open={createOpen} onOpenChange={setCreateOpen} />
			{revokingKey !== null && (
				<ConfirmModal
					open
					onOpenChange={(open) => {
						if (!open) {
							closeRevoke();
						}
					}}
					title={`Revoke ${revokingKey.name}?`}
					description="Requests that send this key fail at once."
					confirmLabel="Revoke key"
					pending={revokeKey.isPending}
					error={revokeKey.error}
					onConfirm={() => revoke(revokingKey)}
				/>
			)}
		</>
	);
}

function apiKeyColumns(now: Date): DataTableColumn<ApiKeyResponse>[] {
	return [
		{ header: "Name", cell: (apiKey) => apiKey.name },
		{
			header: "Key",
			cell: (apiKey) => (
				<span className="font-mono">{`${MASKED_SECRET_PREFIX}${apiKey.secret_last_four}`}</span>
			),
		},
		{
			header: "Scope",
			cell: (apiKey) => API_KEY_SCOPE_DISPLAYS[apiKey.scope].label,
		},
		{
			header: "Last used",
			cell: (apiKey) =>
				apiKey.last_used_at === null ? (
					"Never"
				) : (
					<RelativeTime timestamp={apiKey.last_used_at} now={now} />
				),
		},
		{
			header: "Created",
			cell: (apiKey) => (
				<RelativeTime timestamp={apiKey.created_at} now={now} />
			),
		},
		{
			header: "Status",
			cell: (apiKey) => <StatusDot status={apiKey.status} />,
		},
	];
}
