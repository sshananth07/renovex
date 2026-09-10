import { SpaceList } from "@/features/spaces/components/SpaceList";

export default async function ProjectSpacesPage({
  params,
}: {
  params: Promise<{ projectId: string }>;
}) {
  const { projectId } = await params;
  return <SpaceList projectId={projectId} />;
}
