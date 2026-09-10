import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const REFINEMENT_CHIPS = ["Softer curves", "Darker finish", "A little narrower"];

// Fixed button order (M8.5C plan): Use Design primary, Regenerate outline,
// Cancel ghost/destructive-text. Current/Concept toggle stays on the canvas
// and is never repeated here. Refinement chips start a new prompt draft —
// they never silently trigger a regenerate call themselves.
export function ConceptActions({
  onUseDesign,
  onRegenerate,
  onCancel,
  onRefine,
  useDesignDisabled,
  regenerateDisabled,
}: {
  onUseDesign: () => void;
  onRegenerate: () => void;
  onCancel: () => void;
  onRefine: (chipText: string) => void;
  useDesignDisabled?: boolean;
  regenerateDisabled?: boolean;
}) {
  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Refinement suggestions">
        {REFINEMENT_CHIPS.map((chip) => (
          <button
            key={chip}
            type="button"
            onClick={() => onRefine(chip)}
            className="rounded-full border border-border/70 bg-accent/40 px-3 py-1 text-xs font-medium text-foreground transition-transform duration-100 hover:bg-accent/70 active:scale-[.98]"
          >
            {chip}
          </button>
        ))}
      </div>
      <div className="flex items-center justify-between gap-2">
        <Button variant="ghost" size="sm" className={cn("text-destructive hover:text-destructive")} onClick={onCancel}>
          Cancel
        </Button>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={onRegenerate} disabled={regenerateDisabled}>
            Regenerate
          </Button>
          <Button size="sm" onClick={onUseDesign} disabled={useDesignDisabled}>
            Use Design
          </Button>
        </div>
      </div>
    </div>
  );
}
