import { SupplierDetail } from "@/features/procurement/components/SupplierDetail";
export default async function SupplierPage({ params }: { params: Promise<{ supplierId: string }> }) { const { supplierId } = await params; return <SupplierDetail supplierId={supplierId} />; }
