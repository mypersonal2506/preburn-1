import { Fragment, type ReactNode } from "react";

interface KeyValueItem {
	label: string;
	value: ReactNode;
}

interface KeyValueListProps {
	items: readonly KeyValueItem[];
}

/**
 * A description list of labelled values for detail cards. Items whose value
 * is null, undefined, false or an empty string are left out, so pages pass
 * optional fields as they come. Zero is a value and shows.
 */
export function KeyValueList({ items }: KeyValueListProps) {
	return (
		<dl className="grid grid-cols-[minmax(0,12rem)_1fr] gap-x-4 gap-y-2 text-sm">
			{items.filter(hasValue).map((item) => (
				<Fragment key={item.label}>
					<dt className="text-muted-foreground">{item.label}</dt>
					<dd className="min-w-0 break-words">{item.value}</dd>
				</Fragment>
			))}
		</dl>
	);
}

function hasValue(item: KeyValueItem): boolean {
	return (
		item.value !== null &&
		item.value !== undefined &&
		item.value !== false &&
		item.value !== ""
	);
}
