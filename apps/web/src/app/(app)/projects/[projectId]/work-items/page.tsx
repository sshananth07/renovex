import { WorkItemList } from "@/features/work-items/components/WorkItemList";

export default async function ProjectWorkItemsPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return <WorkItemList projectId={projectId} />;
}
