import { cn } from "@/lib/utils";
import type { ActiveEditorView } from "../types";

// Compact 2D/3D segmented switch (plan §6/RP4C3): switching view never
// refetches the RoomDraft and never touches selection/pendingEdit — both
// live in the shared editor state SpatialEditor already owns. This
// component only reports the user's choice; SpatialEditor decides which
// viewport to mount.
export function EditorViewSwitcher({
  activeView,
  onChange,
  disabled = false,
}: {
  activeView: ActiveEditorView;
  onChange: (view: ActiveEditorView) => void;
  disabled?: boolean;
}) {
  return (
    <div role="radiogroup" aria-label="Editor view" className="inline-flex items-center gap-0.5 rounded-lg border bg-muted/40 p-0.5">
      <ViewOption label="2D" value="2d" activeView={activeView} onChange={onChange} disabled={disabled} />
      <ViewOption label="3D" value="3d" activeView={activeView} onChange={onChange} disabled={disabled} />
    </div>
  );
}

function ViewOption({
  label,
  value,
  activeView,
  onChange,
  disabled,
}: {
  label: string;
  value: ActiveEditorView;
  activeView: ActiveEditorView;
  onChange: (view: ActiveEditorView) => void;
  disabled: boolean;
}) {
  const active = activeView === value;
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      disabled={disabled}
      onClick={() => onChange(value)}
      className={cn(
        "rounded-md px-3 py-1 text-sm font-medium transition-colors disabled:pointer-events-none disabled:opacity-50",
        active ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
      )}
    >
      {label}
    </button>
  );
}
