import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { expect, test } from "vitest";
import { FilterBar } from "@/components/filter-bar";

test("the search box reports each change", async () => {
	const user = userEvent.setup();
	function CustomerFilters() {
		const [search, setSearch] = useState("");
		return (
			<>
				<FilterBar
					label="Customer filters"
					search={{
						value: search,
						placeholder: "Search customers",
						onChange: setSearch,
					}}
				/>
				<p>Searching {search}</p>
			</>
		);
	}
	render(<CustomerFilters />);

	await user.type(screen.getByRole("searchbox", { name: "Search" }), "acme");

	expect(screen.getByText("Searching acme")).toBeInTheDocument();
	expect(
		screen.getByRole("group", { name: "Customer filters" }),
	).toContainElement(screen.getByRole("searchbox"));
});
