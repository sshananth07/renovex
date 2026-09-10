import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { AlertTriangle, Sparkles } from "lucide-react";
import type { components } from "@/lib/api/generated/schema";

type ChangePlan = components["schemas"]["ChangePlanDTO"];
type WorkingDesignGeometry = components["schemas"]["WorkingDesignGeometryDTO"];
type WorkingDesignMaterial = components["schemas"]["WorkingDesignMaterialDTO"];
type Execution = components["schemas"]["ExecutionDTO"];
type FitAnalysis = components["schemas"]["FitAnalysisDTO"];

// Exactly one outer Card with Separator-divided rows — never four nested
// cards (M8.5C plan's explicit prohibited-outcomes list). Rows are omitted
// only when the server contract cannot express them, never hidden by
// client-side guesswork.
export function AIChangePlanCard({
  plan,
  execution,
  fitAnalysis,
  onConfirm,
  onEditPrompt,
  confirmDisabled,
}: {
  plan: ChangePlan;
  execution?: Execution;
  fitAnalysis?: FitAnalysis;
  onConfirm: () => void;
  onEditPrompt: () => void;
  confirmDisabled?: boolean;
}) {
  const geometry = extractGeometry(plan);
  const material = extractMaterial(plan);
  const placementMoved = plan.turnDelta.spatial.mode !== "preserve";
  const gpuRequired = Boolean(execution?.hunyuanRequired);
  const primaryLabel = gpuRequired ? "Create concept" : "Preview change";

  return (
    <Card size="sm" className="gap-0 divide-y-0">
      <CardHeader>
        <CardTitle className="flex items-center gap-1.5">
          <Sparkles className="size-4 stroke-[1.75] text-primary" aria-hidden="true" />
          Change plan
        </CardTitle>
      </CardHeader>
      <CardContent className="grid gap-0">
        {plan.summary && plan.summary.length > 0 && (
          <>
            <ul className="grid gap-0.5 py-2 text-sm leading-5 text-foreground">
              {plan.summary.map((line, index) => (
                <li key={index}>{line}</li>
              ))}
            </ul>
            <Separator />
          </>
        )}
        <PlanRow label="Shape">{geometry ? geometry.shapeDescription : "Keep the current shape"}</PlanRow>
        <Separator />
        <PlanRow label="Look">
          {material ? <MaterialSummary material={material} /> : "Keep the current finish"}
        </PlanRow>
        <Separator />
        <PlanRow label="Placement">{placementMoved ? "Move or resize to the new position" : "Stay in the current position"}</PlanRow>
        <Separator />
        <PlanRow label="Fit">
          <FitSummary fitAnalysis={fitAnalysis} />
        </PlanRow>
      </CardContent>
      <CardFooter className="grid gap-2 border-t-0 bg-transparent px-(--card-spacing) pt-3">
        {gpuRequired && (
          <p className="flex items-start gap-1.5 text-xs leading-4 text-muted-foreground">
            <AlertTriangle className="mt-0.5 size-3.5 shrink-0 stroke-[1.75]" aria-hidden="true" />
            Generating this shape takes a minute or two on shared GPU capacity.
          </p>
        )}
        <div className="flex items-center justify-between gap-2">
          <Button variant="ghost" size="sm" onClick={onEditPrompt}>
            Edit prompt
          </Button>
          <Button size="sm" onClick={onConfirm} disabled={confirmDisabled}>
            {primaryLabel}
          </Button>
        </div>
      </CardFooter>
    </Card>
  );
}

function PlanRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-[72px_1fr] items-start gap-3 py-2 text-sm leading-5">
      <span className="pt-px text-xs font-medium tracking-wide text-muted-foreground uppercase">{label}</span>
      <div className="text-foreground">{children}</div>
    </div>
  );
}

function MaterialSummary({ material }: { material: WorkingDesignMaterial }) {
  return (
    <div className="flex items-center gap-2">
      <span
        className="inline-block size-7 shrink-0 rounded-md border border-border/70"
        style={{ backgroundColor: material.baseColor }}
        aria-hidden="true"
      />
      <span className="capitalize">
        {material.materialFamily}, {material.roughness}
        {material.metallic ? ", metallic" : ""}
      </span>
    </div>
  );
}

function FitSummary({ fitAnalysis }: { fitAnalysis?: FitAnalysis }) {
  if (!fitAnalysis) return <span className="text-muted-foreground">No fit checks reported</span>;

  const blockers = fitAnalysis.blockers ?? [];
  const warnings = fitAnalysis.warnings ?? [];
  const observations = fitAnalysis.observations ?? [];

  if (blockers.length > 0) {
    return (
      <div className="grid gap-1">
        {blockers.map((blocker, index) => (
          <Badge key={index} variant="destructive" className="w-fit font-normal">
            {blocker.message}
          </Badge>
        ))}
      </div>
    );
  }

  if (warnings.length > 0) {
    return (
      <div className="grid gap-1">
        {warnings.map((warning, index) => (
          <Badge key={index} variant="outline" className={cn("w-fit border-amber-500/40 font-normal text-amber-700 dark:text-amber-400")}>
            {warning.message}
          </Badge>
        ))}
      </div>
    );
  }

  if (observations.length > 0) {
    return <span className="text-muted-foreground">{observations.map((observation) => observation.message).join(" · ")}</span>;
  }

  return <span className="text-muted-foreground">Passed all fit checks</span>;
}

function extractGeometry(plan: ChangePlan): WorkingDesignGeometry | undefined {
  return plan.workingDesign.geometry ?? undefined;
}

function extractMaterial(plan: ChangePlan): WorkingDesignMaterial | undefined {
  return plan.workingDesign.material ?? undefined;
}
