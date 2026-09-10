import { EstimatesView } from "@/features/operations/components/EstimatesView";

export default async function EstimatesPage({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <EstimatesView projectId={projectId} />;
}
