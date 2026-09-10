import { SupplierRFQPortal } from "@/features/portals/components/SupplierRFQPortal";

export default async function SupplierAccessPage({
  searchParams,
}: {
  searchParams: Promise<{ token?: string | string[]; returnTo?: string | string[] }>;
}) {
  const query = await searchParams;
  const token = typeof query.token === "string" ? query.token : undefined;
  const returnTo = typeof query.returnTo === "string" ? query.returnTo : undefined;
  return <SupplierRFQPortal token={token} returnTo={returnTo} />;
}
