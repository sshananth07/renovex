"use client";

import Link from "next/link";
import { toast } from "sonner";
import { Scan, CheckCircle2, Clock, AlertTriangle, LayoutGrid } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { useSpatialCaptureList, useSpatialSpaceState } from "../queries";
import { useStartSpatialCapture } from "../mutations";
import type { SpatialCapture } from "../api";

// Design spec §43.1/Task 2: this is the Space's entry point into Spatial
// Intelligence — not a generic admin table. It communicates either "no scan
// yet" or the current room scan plus prior scan history/upload status.
export function SpatialEntry({ projectId, spaceId }: { projectId: string; spaceId: string }) {
  const { data: state, isLoading: stateLoading } = useSpatialSpaceState(projectId, spaceId);
  const { data: captures, isLoading: capturesLoading } = useSpatialCaptureList(spaceId);
  const startCapture = useStartSpatialCapture(projectId, spaceId);

  const isLoading = stateLoading || capturesLoading;
  const hasCaptures = (captures?.length ?? 0) > 0;

  if (isLoading) {
    return <div className="surface-card min-h-72 p-5"><Skeleton className="h-full w-full" /></div>;
  }

  if (!hasCaptures) {
    return (
      <div className="surface-card flex min-h-72 flex-col items-center justify-center gap-3 border-dashed p-8 text-center">
        <span className="flex size-12 items-center justify-center rounded-2xl bg-accent text-accent-foreground"><Scan className="size-6" /></span>
        <div>
          <p className="font-heading text-base font-semibold">No spatial scan yet</p>
          <p className="mt-1 max-w-md text-sm text-muted-foreground">
            Capture this room with the Renovex mobile app to unlock 2D plans, 3D visualization, and AI renovation concepts.
          </p>
        </div>
        <Button className="mt-1" onClick={() => startCapture.mutate()} disabled={startCapture.isPending}>
          <Scan className="size-4" /> Scan with Renovex Capture
        </Button>
        {startCapture.isError && (
          <p className="text-sm text-destructive">Could not start a capture. Please try again.</p>
        )}
      </div>
    );
  }

  const currentVersionCapture = captures?.find((c) => c.roomVersionId === state?.currentRoomVersionId);
  const previousCaptures = (captures ?? []).filter((c) => c.id !== currentVersionCapture?.id);

  return (
    <div className="surface-card min-w-0 p-5">
      <div className="flex items-center justify-between border-b pb-4">
        <h2 className="font-heading text-base font-semibold">Spatial scans</h2>
        <Button size="sm" onClick={() => startCapture.mutate()} disabled={startCapture.isPending}>
          <Scan className="size-4" /> New scan
        </Button>
      </div>

      {currentVersionCapture ? (
        <div className="mt-4">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Current room scan</p>
          <CaptureRow capture={currentVersionCapture} isCurrent projectId={projectId} spaceId={spaceId} />
        </div>
      ) : (
        <div className="mt-4 flex items-center gap-2 rounded-lg bg-muted/50 p-3 text-sm text-muted-foreground">
          <Clock className="size-4 shrink-0" /> No confirmed room version yet — complete review and confirmation on the mobile app.
        </div>
      )}

      {previousCaptures.length > 0 && (
        <div className="mt-5">
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Previous scans</p>
          <div className="mt-2 flex flex-col gap-2">
            {previousCaptures.map((capture) => (
              <CaptureRow key={capture.id} capture={capture} projectId={projectId} spaceId={spaceId} />
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

const STATUS_LABEL: Record<string, string> = {
  draft: "Draft",
  capturing: "Capturing",
  uploading: "Uploading",
  uploaded: "Uploaded",
  review: "Awaiting review",
  confirmed: "Confirmed",
  superseded: "Superseded",
  failed: "Failed",
};

// Captures still short of confirmation but past raw capture/upload are the
// "Continue Review" states (design spec §8.24, plan §RP3 §8) — the
// contractor has a durably persisted, resumable RoomDraft waiting on the
// mobile app, distinct from in-flight capturing/uploading states where
// there is nothing yet to resume reviewing.
const CONTINUE_REVIEW_STATUSES = new Set(["uploaded", "review"]);

function CaptureRow({
  capture,
  isCurrent,
  projectId,
  spaceId,
}: {
  capture: SpatialCapture;
  isCurrent?: boolean;
  projectId: string;
  spaceId: string;
}) {
  const label = STATUS_LABEL[capture.status] ?? capture.status;
  const isConfirmed = capture.status === "confirmed";
  const isFailed = capture.status === "failed";
  const canContinueReview = CONTINUE_REVIEW_STATUSES.has(capture.status);

  return (
    <div className="mt-2 flex items-center justify-between gap-3 rounded-lg border border-border/70 p-3">
      <div className="flex items-center gap-2 text-sm">
        {isConfirmed ? (
          <CheckCircle2 className="size-4 shrink-0 text-primary" />
        ) : isFailed ? (
          <AlertTriangle className="size-4 shrink-0 text-destructive" />
        ) : (
          <Clock className="size-4 shrink-0 text-muted-foreground" />
        )}
        <span className="font-medium text-foreground">Scan #{capture.captureNumber}</span>
        <span className="text-muted-foreground">{new Date(capture.createdAt).toLocaleString()}</span>
      </div>
      <div className="flex items-center gap-2">
        {isCurrent && <Badge className="bg-primary/10 text-primary">Current</Badge>}
        <Badge variant="outline" className="bg-muted/50 text-muted-foreground">{label}</Badge>
        {canContinueReview && (
          <Badge variant="outline" className="border-primary/40 text-primary">Continue Review</Badge>
        )}
        {capture.roomDraftId ? (
          <Link
            href={`/projects/${projectId}/spaces/${spaceId}/spatial/${capture.roomDraftId}`}
            className={buttonVariants({ size: "sm", variant: "outline" })}
          >
            <LayoutGrid className="size-4" /> Open floor plan
          </Link>
        ) : (
          <Button
            size="sm"
            variant="outline"
            className="text-muted-foreground"
            onClick={() =>
              toast.info("No floor plan yet for this scan.", {
                description: "This scan hasn't been processed into an editable floor plan yet. Complete review on the mobile app first.",
              })
            }
          >
            <LayoutGrid className="size-4" /> Open floor plan
          </Button>
        )}
      </div>
    </div>
  );
}
