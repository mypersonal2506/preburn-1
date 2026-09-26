import { SearchIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
	InputGroup,
	InputGroupAddon,
	InputGroupInput,
} from "@/components/ui/input-group";

interface FilterSearch {
	value: string;
	placeholder: string;
	onChange: (value: string) => void;
}

interface FilterBarProps {
	label: string;
	search?: FilterSearch;
	children?: ReactNode;
	end?: ReactNode;
}

/**
 * The one toolbar row above a list: an optional search box, the filters in
 * children, such as FilterChips and pickers, and end content pushed to the
 * right, such as a LiveIndicator. label names the group for screen readers.
 */
export function FilterBar({ label, search, children, end }: FilterBarProps) {
	return (
		<fieldset
			aria-label={label}
			className="flex min-w-0 flex-wrap items-center gap-2"
		>
			{search !== undefined && (
				<InputGroup className="w-64">
					<InputGroupAddon>
						<SearchIcon />
					</InputGroupAddon>
					<InputGroupInput
						type="search"
						aria-label="Search"
						placeholder={search.placeholder}
						value={search.value}
						onChange={(event) => search.onChange(event.target.value)}
					/>
				</InputGroup>
			)}
			{children}
			{end !== undefined && (
				<div className="ml-auto flex items-center gap-2">{end}</div>
			)}
		</fieldset>
	);
}
