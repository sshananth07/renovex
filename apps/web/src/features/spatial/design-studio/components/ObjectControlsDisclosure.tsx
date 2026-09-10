import { ChevronDown } from "lucide-react";
import { ElementInspector } from "../../editor/components/ElementInspector";
import type { EditOperationPayload, Selection } from "../../editor/types";
import type { RoomDraft } from "../../api";

// Hosts the existing ElementInspector unchanged — never a third column,
// never a re-implementation of its controls inside the AI studio (M8.5C
// plan). Native <details> for the collapse: no new dependency, keyboard-
// and screen-reader-accessible for free, collapsed by default so exact
// manual editing stays available without competing with the AI flow.
export function ObjectControlsDisclosure({
  draft,
  selection,
  onSubmit,
  disabled,
}: {
  draft: RoomDraft | null | undefined;
  selection: Selection;
  onSubmit: (operation: EditOperationPayload) => void;
  disabled: boolean;
}) {
  return (
    <details className="group/disclosure rounded-lg border border-border/70">
      <summary className="flex cursor-pointer list-none items-center justify-between gap-2 px-3 py-2 text-sm font-medium text-foreground select-none">
        Manual controls
        <ChevronDown className="size-4 shrink-0 stroke-[1.75] text-muted-foreground transition-transform group-open/disclosure:rotate-180" aria-hidden="true" />
      </summary>
      <div className="border-t border-border/70 px-3 py-2">
        <ElementInspector draft={draft} selection={selection} onSubmit={onSubmit} disabled={disabled} />
      </div>
    </details>
  );
}
