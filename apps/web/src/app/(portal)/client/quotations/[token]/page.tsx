import { ClientQuotationPortal } from "@/features/portals/components/ClientQuotationPortal";
export default async function ClientQuotationPage({ params }: { params: Promise<{ token: string }> }) { const { token } = await params; return <ClientQuotationPortal token={token} />; }
