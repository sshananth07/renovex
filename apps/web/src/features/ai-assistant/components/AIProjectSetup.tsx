"use client";

import { useState } from "react";
import { Check } from "lucide-react";
import { cn } from "@/lib/utils";
import { useProject } from "@/features/projects/queries";
import { useProjectSpacesTotal } from "@/features/spaces/queries";
import { useProjectWorkItemsTotal } from "@/features/work-items/queries";
import { deriveAISetupStage, type AISetupStage } from "../aiSetupProgress";
import { isSetupComplete, latestCompletedBatch } from "../aiSetupSummary";
import { useAIBatches, useWorkResourceRequirements } from "../queries";
import { ProjectBriefForm } from "./ProjectBriefForm";
import { SpaceSuggestionReview } from "./SpaceSuggestionReview";
import { WorkItemSuggestionReview } from "./WorkItemSuggestionReview";
import { ResourceSuggestionReview } from "./ResourceSuggestionReview";
import { AISetupCompletionSummary } from "./AISetupCompletionSummary";
import { RerunSetupDialog } from "./RerunSetupDialog";
import { AISetupHistory } from "./AISetupHistory";

const stageOrder: AISetupStage[] = ["brief", "spaces", "workItems", "resources"];
const stageLabels: Record<AISetupStage, string> = {
  brief: "Project brief saved",
  spaces: "Review Spaces",
  workItems: "Generate Work Items",
  resources: "Suggest Resources",
};

function StageProgress({ current }: { current: AISetupStage }) {
  const currentIndex = stageOrder.indexOf(current);
  return (
    <ol className="flex flex-col gap-2">
      {stageOrder.map((stage, index) => {
        const isDone = index < currentIndex;
        const isCurrent = index === currentIndex;
        return (
          <li key={stage} className="flex items-center gap-2 text-sm">
            <span
              className={cn(
                "flex size-5 shrink-0 items-center justify-center rounded-full border text-xs",
                isDone ? "border-primary/20 bg-primary/10 text-primary" : "border-border text-muted-foreground"
              )}
            >
              {isDone ? <Check className="size-3" /> : index + 1}
            </span>
            <span className={cn(isCurrent ? "font-medium text-foreground" : "text-muted-foreground")}>
              {stageLabels[stage]}
            </span>
          </li>
        );
      })}
    </ol>
  );
}

export function AIProjectSetup({ projectId }: { projectId: string }) {
  const { data: project, isLoading: projectLoading } = useProject(projectId);
  const { data: spaces } = useProjectSpacesTotal(projectId);
  const { data: workItems } = useProjectWorkItemsTotal(projectId);
  const { data: resources } = useWorkResourceRequirements(projectId);

  const spaceBatchesQuery = useAIBatches(projectId, "space_suggestions");
  const workItemBatchesQuery = useAIBatches(projectId, "work_item_suggestions");
  const resourceBatchesQuery = useAIBatches(projectId, "resource_suggestions");

  const [showRerunDialog, setShowRerunDialog] = useState(false);
  const [showHistory, setShowHistory] = useState(false);
  const [forceExpanded, setForceExpanded] = useState(false);

  if (projectLoading || !project) {
    return null;
  }

  const stage = deriveAISetupStage({
    scopeBrief: project.scopeBrief ?? "",
    confirmedSpaces: spaces?.total ?? 0,
    confirmedWorkItems: workItems?.total ?? 0,
  });

  const spaceBatches = spaceBatchesQuery.data?.batches ?? [];
  const workItemBatches = workItemBatchesQuery.data?.batches ?? [];
  const resourceBatches = resourceBatchesQuery.data?.batches ?? [];

  // Completion can only be evaluated once the flow has reached the
  // Resources stage (deriveAISetupStage already proves all three domain
  // counts are non-empty at that point) — pendingCounts stays "unknown" (0)
  // before that, but isSetupComplete's own latestCompletedBatch checks
  // already gate on the earlier stages regardless.
  const setupComplete =
    stage === "resources" &&
    !forceExpanded &&
    isSetupComplete({
      spaceBatches,
      workItemBatches,
      resourceBatches,
      pendingCounts: { spaces: 0, workItems: 0, resources: 0 },
    });

  const latestSpaceBatch = latestCompletedBatch(spaceBatches);
  const briefUnchangedSinceLastRun = Boolean(
    latestSpaceBatch?.sourceBrief && latestSpaceBatch.sourceBrief.trim() === (project.scopeBrief ?? "").trim()
  );

  const handleRerunClick = () => {
    setForceExpanded(true);
    if (briefUnchangedSinceLastRun) {
      setShowRerunDialog(true);
    }
  };

  return (
    <section className="surface-card flex flex-col gap-5 p-5">
      <div>
        <h2 className="font-heading text-base font-semibold">AI Project Setup</h2>
        <p className="mt-0.5 text-xs text-muted-foreground">Turn your renovation brief into structured scope.</p>
      </div>

      {project.scopeBrief && (
        <p className="rounded-lg bg-muted/50 p-3 text-sm text-muted-foreground">{project.scopeBrief}</p>
      )}

      {setupComplete ? (
        <>
          <AISetupCompletionSummary
            spaceBatches={spaceBatches}
            spacesTotal={spaces?.total ?? 0}
            workItemsTotal={workItems?.total ?? 0}
            resourcesTotal={resources?.requirements?.length ?? 0}
            onViewHistory={() => setShowHistory((v) => !v)}
            onRerun={handleRerunClick}
          />
          {showHistory && (
            <AISetupHistory spaceBatches={spaceBatches} workItemBatches={workItemBatches} resourceBatches={resourceBatches} />
          )}
        </>
      ) : (
        <>
          <StageProgress current={stage} />

          <ProjectBriefForm projectId={projectId} initialBrief={project.scopeBrief ?? ""} />

          {stage !== "brief" && <SpaceSuggestionReview projectId={projectId} />}
          {(stage === "workItems" || stage === "resources") && <WorkItemSuggestionReview projectId={projectId} />}
          {stage === "resources" && <ResourceSuggestionReview projectId={projectId} />}
        </>
      )}

      <RerunSetupDialog
        open={showRerunDialog}
        onOpenChange={setShowRerunDialog}
        onConfirm={() => {
          // The actual "Suggest Spaces" / "Generate fresh suggestions"
          // click inside SpaceSuggestionReview performs the real rerun —
          // this dialog's job is only the warn-and-confirm gate before the
          // active review flow (already expanded via forceExpanded) is
          // shown, matching "the comparison workflow lives inline on the
          // page, not inside this modal."
        }}
      />
    </section>
  );
}
