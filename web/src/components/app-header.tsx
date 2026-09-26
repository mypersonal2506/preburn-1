import { Link, useMatches } from "@tanstack/react-router";
import { Fragment } from "react";
import { Badge } from "@/components/ui/badge";
import {
	Breadcrumb,
	BreadcrumbItem,
	BreadcrumbLink,
	BreadcrumbList,
	BreadcrumbPage,
	BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";
import { useEnvironment } from "@/lib/environment-store";

/**
 * How the header breadcrumb names the record a detail route shows, which
 * the route puts in its context as `recordTitle`: the record's id, and
 * useName, a hook returning the record's name from the query its page
 * reads, or undefined until that query has the record.
 */
export interface RecordTitle {
	id: string;
	useName: (id: string) => string | undefined;
}

interface RecordNameProps {
	recordTitle: RecordTitle;
	fallback: string;
}

/**
 * The bar above every page of the app shell: the sidebar toggle, the
 * breadcrumb of a detail page, and a "Live data" badge while the dashboard
 * works in the live environment. Only a page nested under a section has a
 * breadcrumb, which links back to the section ("Policies / Heavy video
 * users"). List pages such as Overview and Policies show only their page
 * title. Crumbs come from the `title` static data of the matched routes,
 * and a detail route with a `recordTitle` in its context shows the
 * record's name instead of its title once the record loads.
 */
export function AppHeader() {
	const environment = useEnvironment();
	const crumbs = useMatches().flatMap((match) =>
		match.staticData.title === undefined
			? []
			: [
					{
						id: match.id,
						title: match.staticData.title,
						to: match.pathname,
						recordTitle:
							"recordTitle" in match.context
								? match.context.recordTitle
								: undefined,
					},
				],
	);
	const pageCrumb = crumbs.at(-1);
	const sectionCrumbs = crumbs.slice(0, -1);

	return (
		<header className="flex h-[3.5rem] shrink-0 items-center gap-2 border-b px-4">
			<SidebarTrigger className="-ml-1" />
			{pageCrumb !== undefined && sectionCrumbs.length > 0 && (
				<>
					<Separator
						orientation="vertical"
						className="mr-2 data-vertical:h-4 data-vertical:self-center"
					/>
					<Breadcrumb>
						<BreadcrumbList>
							{sectionCrumbs.map((crumb) => (
								<Fragment key={crumb.id}>
									<BreadcrumbItem>
										<BreadcrumbLink asChild>
											<Link to={crumb.to}>{crumb.title}</Link>
										</BreadcrumbLink>
									</BreadcrumbItem>
									<BreadcrumbSeparator>/</BreadcrumbSeparator>
								</Fragment>
							))}
							<BreadcrumbItem>
								<BreadcrumbPage>
									{pageCrumb.recordTitle === undefined ? (
										pageCrumb.title
									) : (
										<RecordName
											key={pageCrumb.id}
											recordTitle={pageCrumb.recordTitle}
											fallback={pageCrumb.title}
										/>
									)}
								</BreadcrumbPage>
							</BreadcrumbItem>
						</BreadcrumbList>
					</Breadcrumb>
				</>
			)}
			{environment === "live" && (
				<Badge
					variant="outline"
					className="ml-auto border-warning/40 bg-warning/10 text-warning"
				>
					Live data
				</Badge>
			)}
		</header>
	);
}

function RecordName({ recordTitle, fallback }: RecordNameProps) {
	return recordTitle.useName(recordTitle.id) ?? fallback;
}
