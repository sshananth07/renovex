import { CostsView } from "@/features/operations/components/CostsView";

export default async function CostsPage({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <CostsView projectId={projectId} />;
}
