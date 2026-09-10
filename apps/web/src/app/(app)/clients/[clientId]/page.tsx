import { ClientDetail } from "@/features/clients/components/ClientDetail";

export default async function ClientDetailPage({
  params,
}: {
  params: Promise<{ clientId: string }>;
}) {
  const { clientId } = await params;
  return <ClientDetail clientId={clientId} />;
}
