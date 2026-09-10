import { cn } from "@/lib/utils";
import type { ComparisonMode } from "../types";

// Floating semantic segmented control over the canvas — two real buttons
// with aria-pressed, never a raw <select> or a third canvas/viewer (M8.5C
// plan: "A Current/Concept comparison implemented as screenshots, two
// canvases, or side-by-side viewers" is explicitly prohibited). Horizontally
// centered 16px above the canvas bottom edge; positioning applied by the
// caller (SpatialCanvasStage), same convention as SelectedObjectLabel.
export function CurrentConceptToggle({
  mode,
  onChange,
  conceptDisabled,
}: {
  mode: ComparisonMode;
  onChange: (mode: ComparisonMode) => void;
  /** True while no concept exists yet to compare against (e.g. still planning). */
  conceptDisabled?: boolean;
}) {
  return (
    <div
      role="group"
      aria-label="Compare current and concept"
      className="pointer-events-auto flex items-center gap-0.5 rounded-full border border-border/70 bg-card/95 p-0.5 shadow-sm backdrop-blur-sm"
    >
      <ToggleButton label="Current" pressed={mode === "current"} onClick={() => onChange("current")} />
      <ToggleButton label="Concept" pressed={mode === "concept"} onClick={() => onChange("concept")} disabled={conceptDisabled} />
    </div>
  );
}

function ToggleButton({
  label,
  pressed,
  onClick,
  disabled,
}: {
  label: string;
  pressed: boolean;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "rounded-full px-3 py-1 text-xs font-semibold transition-[transform,background-color] duration-150 active:scale-[.98] disabled:pointer-events-none disabled:opacity-40",
        pressed ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent/60 hover:text-foreground",
      )}
    >
      {label}
    </button>
  );
}
