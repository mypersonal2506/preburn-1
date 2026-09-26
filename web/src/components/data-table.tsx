import {
	Link,
	type LinkOptions,
	type RegisteredRouter,
	useNavigate,
} from "@tanstack/react-router";
import { cn } from "cn";
import {
	ArrowDownIcon,
	ArrowUpDownIcon,
	ArrowUpIcon,
	ChevronLeftIcon,
	ChevronRightIcon,
	EllipsisIcon,
	type LucideIcon,
} from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import type { ListDashboardCustomersData } from "@/client";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "@/components/ui/table";
import type { CursorPagination } from "@/components/use-cursor-pagination";

type SortDirection = NonNullable<
	NonNullable<ListDashboardCustomersData["query"]>["direction"]
>;

/**
 * The sort a DataTable shows: an API sort key and its direction, spelled
 * like the API's direction query parameter.
 */
export interface DataTableSort<SortKey extends string> {
	key: SortKey;
	direction: SortDirection;
}

/**
 * One column of a DataTable. cell renders the column's content for a row.
 * numeric right-aligns the column in numeric type, for money and counts.
 * A column with a sortKey gets a sort button in its header.
 */
export interface DataTableColumn<Row, SortKey extends string = never> {
	header: string;
	cell: (row: Row) => ReactNode;
	numeric?: boolean;
	sortKey?: SortKey;
}

type DataTableRowLink = LinkOptions<RegisteredRouter, string, string>;

type HeaderSort = SortDirection | "none";

interface DataTableProps<Row, SortKey extends string> {
	label: string;
	columns: readonly DataTableColumn<Row, SortKey>[];
	rows: readonly Row[] | undefined;
	rowKey: (row: Row) => string;
	rowLink?: (row: Row) => DataTableRowLink;
	rowMenu?: (row: Row) => ReactNode;
	emptyState: ReactNode;
	pagination?: CursorPagination;
	nextCursor?: string | null;
	sort?: DataTableSort<SortKey>;
	onSortChange?: (sort: DataTableSort<SortKey>) => void;
}

interface DataTableHeadProps<Row, SortKey extends string> {
	column: DataTableColumn<Row, SortKey>;
	sort: DataTableSort<SortKey> | undefined;
	onSortChange: ((sort: DataTableSort<SortKey>) => void) | undefined;
}

interface PaginationButtonsProps {
	pagination: CursorPagination;
	nextCursor: string | null | undefined;
}

const SKELETON_ROW_COUNT = 5;
const SKELETON_ROW_KEYS = Array.from(
	{ length: SKELETON_ROW_COUNT },
	(_, position) => `skeleton-${position}`,
);
const INNER_CONTROL_SELECTOR = "a, button, input, select, textarea, label";
const SORT_ICONS: Record<HeaderSort, LucideIcon> = {
	none: ArrowUpDownIcon,
	ascending: ArrowUpIcon,
	descending: ArrowDownIcon,
};

/**
 * A list table. rows is undefined while the first page loads, which shows
 * skeleton rows, and an empty array shows emptyState inside the body under
 * the header. With rowLink, a click anywhere on a row opens its link and
 * the first cell is a real link for the keyboard. Build the link with
 * `linkOptions({ to: "/customers/$customerId", params: { customerId } })`
 * so the route and its params are checked. Clicks on inner links,
 * buttons and form controls, and on anything portaled out of the row such
 * as its menu, never open the row. rowMenu adds a trailing menu of
 * DropdownMenuItems. pagination from useCursorPagination with the page's
 * nextCursor adds Previous and Next. sort and onSortChange drive the sort
 * buttons of columns with a sortKey, and a new column starts ascending.
 */
export function DataTable<Row, SortKey extends string = never>({
	label,
	columns,
	rows,
	rowKey,
	rowLink,
	rowMenu,
	emptyState,
	pagination,
	nextCursor,
	sort,
	onSortChange,
}: DataTableProps<Row, SortKey>) {
	const navigate = useNavigate();
	const columnCount = columns.length + (rowMenu === undefined ? 0 : 1);

	function openRow(
		event: MouseEvent<HTMLTableRowElement>,
		link: DataTableRowLink,
	): void {
		const row = event.currentTarget;
		const target = event.target;
		// React bubbles clicks from portaled menus and dialogs through the row.
		if (!(target instanceof Element) || !row.contains(target)) {
			return;
		}
		const control = target.closest(INNER_CONTROL_SELECTOR);
		if (control !== null && row.contains(control)) {
			return;
		}
		navigate(link);
	}

	function bodyRows(): ReactNode {
		if (rows === undefined) {
			return SKELETON_ROW_KEYS.map((skeletonKey) => (
				<TableRow key={skeletonKey}>
					{columns.map((column) => (
						<TableCell key={column.header}>
							<Skeleton className="h-4 w-full max-w-32" />
						</TableCell>
					))}
					{rowMenu !== undefined && <TableCell />}
				</TableRow>
			));
		}
		if (rows.length === 0) {
			return (
				<TableRow className="hover:bg-transparent">
					<TableCell colSpan={columnCount}>{emptyState}</TableCell>
				</TableRow>
			);
		}
		return rows.map((row) => {
			const link = rowLink?.(row);
			return (
				<TableRow
					key={rowKey(row)}
					className={cn(link !== undefined && "cursor-pointer")}
					onClick={
						link === undefined ? undefined : (event) => openRow(event, link)
					}
				>
					{columns.map((column, columnIndex) => (
						<TableCell
							key={column.header}
							className={cn(column.numeric && "numeric text-right")}
						>
							{columnIndex === 0 && link !== undefined ? (
								<Link
									{...link}
									className="rounded-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
								>
									{column.cell(row)}
								</Link>
							) : (
								column.cell(row)
							)}
						</TableCell>
					))}
					{rowMenu !== undefined && (
						<TableCell className="w-0">
							<DropdownMenu>
								<DropdownMenuTrigger asChild>
									<Button
										variant="ghost"
										size="icon-sm"
										aria-label="Row actions"
									>
										<EllipsisIcon />
									</Button>
								</DropdownMenuTrigger>
								<DropdownMenuContent align="end" className="w-auto">
									{rowMenu(row)}
								</DropdownMenuContent>
							</DropdownMenu>
						</TableCell>
					)}
				</TableRow>
			);
		});
	}

	return (
		<div className="flex flex-col gap-3">
			<Table aria-label={label} aria-busy={rows === undefined}>
				<TableHeader>
					<TableRow>
						{columns.map((column) => (
							<DataTableHead
								key={column.header}
								column={column}
								sort={sort}
								onSortChange={onSortChange}
							/>
						))}
						{rowMenu !== undefined && (
							<TableHead className="w-0">
								<span className="sr-only">Actions</span>
							</TableHead>
						)}
					</TableRow>
				</TableHeader>
				<TableBody>{bodyRows()}</TableBody>
			</Table>
			{pagination !== undefined && (
				<PaginationButtons pagination={pagination} nextCursor={nextCursor} />
			)}
		</div>
	);
}

function DataTableHead<Row, SortKey extends string>({
	column,
	sort,
	onSortChange,
}: DataTableHeadProps<Row, SortKey>) {
	const alignment = cn(column.numeric && "text-right");
	const sortKey = column.sortKey;
	if (sortKey === undefined || onSortChange === undefined) {
		return <TableHead className={alignment}>{column.header}</TableHead>;
	}
	const headerSort: HeaderSort =
		sort?.key === sortKey ? sort.direction : "none";
	const SortIcon = SORT_ICONS[headerSort];
	return (
		<TableHead className={alignment} aria-sort={headerSort}>
			<Button
				variant="ghost"
				size="sm"
				className="-mx-2"
				onClick={() =>
					onSortChange({
						key: sortKey,
						direction: headerSort === "ascending" ? "descending" : "ascending",
					})
				}
			>
				{column.header}
				<SortIcon />
			</Button>
		</TableHead>
	);
}

function PaginationButtons({ pagination, nextCursor }: PaginationButtonsProps) {
	const goToNext =
		typeof nextCursor === "string"
			? () => pagination.goToNext(nextCursor)
			: undefined;
	return (
		<div className="flex items-center justify-end gap-2">
			<Button
				variant="outline"
				size="sm"
				disabled={!pagination.hasPrevious}
				onClick={pagination.goToPrevious}
			>
				<ChevronLeftIcon />
				Previous
			</Button>
			<Button
				variant="outline"
				size="sm"
				disabled={goToNext === undefined}
				onClick={goToNext}
			>
				Next
				<ChevronRightIcon />
			</Button>
		</div>
	);
}
