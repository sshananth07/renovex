import { SpaceDetail } from "@/features/spaces/components/SpaceDetail";

export default async function SpaceDetailPage({
  params,
}: {
  params: Promise<{ projectId: string; spaceId: string }>;
}) {
  const { projectId, spaceId } = await params;
  return <SpaceDetail projectId={projectId} spaceId={spaceId} />;
}
