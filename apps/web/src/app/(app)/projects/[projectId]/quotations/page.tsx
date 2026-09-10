import { QuotationsView } from "@/features/operations/components/QuotationsView";

export default async function QuotationsPage({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <QuotationsView projectId={projectId} />;
}
