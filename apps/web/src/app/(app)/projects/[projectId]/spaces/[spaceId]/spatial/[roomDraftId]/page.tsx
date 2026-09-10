import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { SpatialEditor } from "@/features/spatial/editor/components/SpatialEditor";

export default async function SpatialEditorPage({
  params,
}: {
  params: Promise<{ projectId: string; spaceId: string; roomDraftId: string }>;
}) {
  const { projectId, spaceId, roomDraftId } = await params;
  return (
    <div className="grid gap-4 p-6">
      <Link
        href={`/projects/${projectId}/spaces/${spaceId}`}
        className="inline-flex w-fit items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="size-4" /> Back to space
      </Link>
      <SpatialEditor roomDraftId={roomDraftId} />
    </div>
  );
}
