import { SupplierOutcomePage } from "@/features/portals/components/SupplierOutcomePage";

export default async function SupplierOutcomeRoute({
  params,
}: {
  params: Promise<{ invitationId: string; outcomeId: string }>;
}) {
  const { invitationId, outcomeId } = await params;
  return <SupplierOutcomePage invitationId={invitationId} outcomeId={outcomeId} />;
}
