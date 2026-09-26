import {
	createMemoryHistory,
	createRootRoute,
	createRoute,
	createRouter,
	Outlet,
	RouterProvider,
	useRouterState,
} from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { UsersIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { expect, test, vi } from "vitest";
import {
	DataTable,
	type DataTableColumn,
	type DataTableSort,
} from "@/components/data-table";
import { EmptyState } from "@/components/empty-state";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { useCursorPagination } from "@/components/use-cursor-pagination";

interface Customer {
	id: string;
	name: string;
	revenue: string;
}

interface CustomerPage {
	items: Customer[];
	next_cursor: string | null;
}

type CustomerSortKey = "revenue";

const acme: Customer = { id: "cust_acme", name: "Acme", revenue: "$120.00" };
const cedar: Customer = {
	id: "cust_cedar",
	name: "Cedar Games",
	revenue: "$80.00",
};
const lumber: Customer = {
	id: "cust_lumber",
	name: "Lumber Co",
	revenue: "$40.00",
};

const customerColumns: readonly DataTableColumn<Customer, CustomerSortKey>[] = [
	{ header: "Customer", cell: (customer) => customer.name },
	{
		header: "Revenue",
		cell: (customer) => customer.revenue,
		numeric: true,
		sortKey: "revenue",
	},
];

const FIRST_PAGE = "first";

const customerPages = new Map<string, CustomerPage>([
	[FIRST_PAGE, { items: [acme], next_cursor: "cursor-2" }],
	["cursor-2", { items: [cedar], next_cursor: "cursor-3" }],
	["cursor-3", { items: [lumber], next_cursor: null }],
]);

const emptyCustomers = <EmptyState icon={UsersIcon} title="No customers yet" />;

function customerLink(customer: Customer) {
	return {
		to: "/customers/$customerId",
		params: { customerId: customer.id },
	};
}

function customerPageAt(cursor: string | undefined): CustomerPage {
	const page = customerPages.get(cursor ?? FIRST_PAGE);
	if (page === undefined) {
		throw new Error(`test page missing cursor=${cursor}`);
	}
	return page;
}

function CurrentPath() {
	const pathname = useRouterState({
		select: (state) => state.location.pathname,
	});
	return <h1>Path {pathname}</h1>;
}

function renderInRouter(list: () => ReactNode) {
	const rootRoute = createRootRoute({ component: Outlet });
	const listRoute = createRoute({
		getParentRoute: () => rootRoute,
		path: "/",
		component: list,
	});
	const detailRoute = createRoute({
		getParentRoute: () => rootRoute,
		path: "/customers/$customerId",
		component: CurrentPath,
	});
	const router = createRouter({
		routeTree: rootRoute.addChildren([listRoute, detailRoute]),
		history: createMemoryHistory({ initialEntries: ["/"] }),
	});
	render(<RouterProvider router={router} />);
}

async function findRow(name: string): Promise<HTMLElement> {
	const row = (await screen.findByText(name)).closest("tr");
	if (row === null) {
		throw new Error(`test row missing name=${name}`);
	}
	return row;
}

test("clicking a row opens its link", async () => {
	const user = userEvent.setup();
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={[acme, cedar]}
			rowKey={(customer) => customer.id}
			rowLink={customerLink}
			emptyState={emptyCustomers}
		/>
	));

	await user.click(within(await findRow("Cedar Games")).getByText("$80.00"));

	expect(
		await screen.findByRole("heading", { name: "Path /customers/cust_cedar" }),
	).toBeInTheDocument();
});

test("the first cell of a linked row is a link for keyboard users", async () => {
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={[acme]}
			rowKey={(customer) => customer.id}
			rowLink={customerLink}
			emptyState={emptyCustomers}
		/>
	));

	expect(await screen.findByRole("link", { name: "Acme" })).toHaveAttribute(
		"href",
		"/customers/cust_acme",
	);
});

test("an inner button runs its action without opening the row", async () => {
	const user = userEvent.setup();
	const onRefresh = vi.fn();
	const columns: readonly DataTableColumn<Customer>[] = [
		{ header: "Customer", cell: (customer) => customer.name },
		{
			header: "Refresh",
			cell: () => <Button onClick={onRefresh}>Refresh</Button>,
		},
	];
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={columns}
			rows={[acme]}
			rowKey={(customer) => customer.id}
			rowLink={customerLink}
			emptyState={emptyCustomers}
		/>
	));

	await user.click(await screen.findByRole("button", { name: "Refresh" }));

	expect(onRefresh).toHaveBeenCalledOnce();
	expect(screen.getByRole("table", { name: "Customers" })).toBeInTheDocument();
	expect(screen.queryByRole("heading")).toBeNull();
});

test("a row menu item runs its action without opening the row", async () => {
	const user = userEvent.setup();
	const onRemove = vi.fn();
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={[acme]}
			rowKey={(customer) => customer.id}
			rowLink={customerLink}
			rowMenu={() => (
				<DropdownMenuItem onSelect={onRemove}>Remove</DropdownMenuItem>
			)}
			emptyState={emptyCustomers}
		/>
	));

	await user.click(await screen.findByRole("button", { name: "Row actions" }));
	await user.click(await screen.findByRole("menuitem", { name: "Remove" }));

	expect(onRemove).toHaveBeenCalledOnce();
	expect(screen.getByRole("table", { name: "Customers" })).toBeInTheDocument();
	expect(screen.queryByRole("heading")).toBeNull();
});

test("shows skeleton rows while the rows load", async () => {
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={undefined}
			rowKey={(customer) => customer.id}
			emptyState={emptyCustomers}
		/>
	));

	const table = await screen.findByRole("table", { name: "Customers" });
	expect(table).toHaveAttribute("aria-busy", "true");
	expect(within(table).getAllByRole("row")).toHaveLength(6);
	expect(screen.queryByText("No customers yet")).toBeNull();
});

test("shows the empty state inside the body when there are no rows", async () => {
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={[]}
			rowKey={(customer) => customer.id}
			emptyState={emptyCustomers}
		/>
	));

	const emptyCell = (await screen.findByText("No customers yet")).closest("td");
	expect(emptyCell).toHaveAttribute("colspan", "2");
	expect(
		screen.getByRole("columnheader", { name: "Customer" }),
	).toBeInTheDocument();
});

test("Previous and Next move through the cursor stack and a new filter starts over", async () => {
	const user = userEvent.setup();
	function PaginatedCustomers() {
		const [revenueFilter, setRevenueFilter] = useState("all");
		const pagination = useCursorPagination(revenueFilter);
		const page = customerPageAt(pagination.cursor);
		return (
			<>
				<Button onClick={() => setRevenueFilter("all")}>All</Button>
				<Button onClick={() => setRevenueFilter("paying")}>Paying</Button>
				<DataTable
					label="Customers"
					columns={customerColumns}
					rows={page.items}
					rowKey={(customer) => customer.id}
					emptyState={emptyCustomers}
					pagination={pagination}
					nextCursor={page.next_cursor}
				/>
			</>
		);
	}
	renderInRouter(PaginatedCustomers);

	const previous = await screen.findByRole("button", { name: "Previous" });
	const next = screen.getByRole("button", { name: "Next" });
	expect(screen.getByText("Acme")).toBeInTheDocument();
	expect(previous).toBeDisabled();

	await user.click(next);
	expect(screen.getByText("Cedar Games")).toBeInTheDocument();
	expect(previous).toBeEnabled();

	await user.click(next);
	expect(screen.getByText("Lumber Co")).toBeInTheDocument();
	expect(next).toBeDisabled();

	await user.click(previous);
	expect(screen.getByText("Cedar Games")).toBeInTheDocument();

	await user.click(screen.getByRole("button", { name: "Paying" }));
	expect(screen.getByText("Acme")).toBeInTheDocument();
	expect(previous).toBeDisabled();

	await user.click(screen.getByRole("button", { name: "All" }));
	expect(screen.getByText("Acme")).toBeInTheDocument();
	expect(previous).toBeDisabled();
});

test("a sortable header reports the flipped sort", async () => {
	const user = userEvent.setup();
	const onSortChange = vi.fn<(sort: DataTableSort<CustomerSortKey>) => void>();
	renderInRouter(() => (
		<DataTable
			label="Customers"
			columns={customerColumns}
			rows={[acme]}
			rowKey={(customer) => customer.id}
			emptyState={emptyCustomers}
			sort={{ key: "revenue", direction: "ascending" }}
			onSortChange={onSortChange}
		/>
	));

	const revenueHeader = await screen.findByRole("columnheader", {
		name: "Revenue",
	});
	expect(revenueHeader).toHaveAttribute("aria-sort", "ascending");
	expect(
		within(screen.getByRole("columnheader", { name: "Customer" })).queryByRole(
			"button",
		),
	).toBeNull();

	await user.click(within(revenueHeader).getByRole("button"));

	expect(onSortChange).toHaveBeenCalledWith({
		key: "revenue",
		direction: "descending",
	});
});
