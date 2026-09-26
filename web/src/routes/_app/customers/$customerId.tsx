import { useQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { getDashboardCustomerOptions } from "@/client/@tanstack/react-query.gen";
import type { RecordTitle } from "@/components/app-header";
import { CustomerDetailPage } from "@/features/customers/customer-detail-page";

export const Route = createFileRoute("/_app/customers/$customerId")({
	staticData: { title: "Customer" },
	context: ({ params }): { recordTitle: RecordTitle } => ({
		recordTitle: { id: params.customerId, useName: useCustomerName },
	}),
	component: CustomerDetailPage,
});

function useCustomerName(customerId: string): string | undefined {
	return useQuery({
		...getDashboardCustomerOptions({ path: { customer_id: customerId } }),
		select: (customer) => customer.display_name ?? customer.external_id,
	}).data;
}
