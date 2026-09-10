import { ResourcesView } from "@/features/operations/components/ResourcesView";

export default async function ResourcesPage({ params }: { params: Promise<{ projectId: string }> }) {
  const { projectId } = await params;
  return <ResourcesView projectId={projectId} />;
}
