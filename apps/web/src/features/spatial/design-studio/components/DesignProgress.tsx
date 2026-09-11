import { CircleCheck, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import type { DesignGenerationStatus } from "../types";

type ProgressStage = "reference" | "geometry" | "validation";

const STAGE_COPY: Record<ProgressStage, string> = {
  reference: "Preparing the visual direction",
  geometry: "Shaping the 3D concept",
  validation: "Checking the model in your room",
};

const STAGE_ORDER: ProgressStage[] = ["reference", "geometry", "validation"];

function statusMessage(status: DesignGenerationStatus): string {
  switch (status) {
    case "asset_generation_pending":
      return "Queued for generation…";
    case "asset_generation_processing":
      return "Generation is running…";
    default:
      return "Preparing your design concept…";
  }
}

// Maps the server's real attempt status to one of the three operator-safe
// stages (M8.5C plan: never mention Cloudflare/FLUX/Hugging
// Face/Hunyuan/ZeroGPU/queues/workers/retries/expected minutes). Returns
// undefined once the attempt has left the in-progress window entirely.
function stageForStatus(status: DesignGenerationStatus): ProgressStage | undefined {
  switch (status) {
    case "reserved":
    case "generating_reference":
      return "reference";
    case "reference_ready":
    case "asset_generation_pending":
    case "asset_generation_processing":
      return "geometry";
    default:
      return undefined;
  }
}

// Staged, nonblocking, no percentages (plan's exact instruction). The
// current object stays visible and usable — this never becomes a
// full-screen loader.
export function DesignProgress({ status }: { status: DesignGenerationStatus }) {
  const activeStage = stageForStatus(status);
  const activeIndex = activeStage ? STAGE_ORDER.indexOf(activeStage) : STAGE_ORDER.length;

  return (
    <div className="grid gap-2" role="status" aria-live="polite">
      <p className="text-sm font-medium text-foreground">{statusMessage(status)}</p>
      <ul className="grid gap-1.5">
        {STAGE_ORDER.map((stage, index) => {
          const isDone = index < activeIndex;
          const isActive = index === activeIndex;
          return (
            <li key={stage} className="flex items-center gap-2 text-sm leading-5">
              {isDone ? (
                <CircleCheck className="size-4 shrink-0 stroke-[1.75] text-primary" aria-hidden="true" />
              ) : isActive ? (
                <Loader2 className="size-4 shrink-0 animate-spin stroke-[1.75] text-primary" aria-hidden="true" />
              ) : (
                <span className="size-4 shrink-0 rounded-full border border-border/70" aria-hidden="true" />
              )}
              <span className={cn(isActive ? "text-foreground" : isDone ? "text-foreground/80" : "text-muted-foreground")}>
                {STAGE_COPY[stage]}
              </span>
            </li>
          );
        })}
      </ul>
      <p className="text-xs leading-4 text-muted-foreground">You can keep working in the room while this finishes.</p>
    </div>
  );
}
