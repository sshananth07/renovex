import type { ApiError } from "@/lib/api/errors";

// Distinguishes edit-submission failures into the categories RP4C1 requires
// distinct UX for (plan §9-10): invalid edit, stale revision, operation-id
// conflict, not found, authorization failure, network/server failure. Reads
// the structured ErrorModel.type/`code` field the backend now populates for
// the two RoomDraft-edit 409s (backend addendum #2) rather than matching
// prose in `detail` — kept local to this feature, not a shared classifier,
// since it depends on a contract only these two routes currently honor.
export type EditConflictKind = "stale_revision" | "operation_id_conflict" | "unknown_conflict";

export function classifyEditError(error: ApiError): EditConflictKind | "validation" | "not_found" | "forbidden" | "network" | "unknown" {
  if (error.kind === "network") return "network";
  if (error.status === 409) {
    if (error.code === "stale_revision") return "stale_revision";
    if (error.code === "operation_id_conflict") return "operation_id_conflict";
    return "unknown_conflict";
  }
  if (error.status === 422) return "validation";
  if (error.status === 404) return "not_found";
  if (error.status === 403) return "forbidden";
  return "unknown";
}

export function EditStatusBanner({ pending, error }: { pending: boolean; error: ApiError | null }) {
  if (pending) {
    return (
      <p className="rounded-lg border border-border/70 bg-muted/50 p-3 text-sm text-muted-foreground">
        Saving your change…
      </p>
    );
  }

  if (!error) return null;

  const kind = classifyEditError(error);
  const message = messageFor(kind, error);

  return (
    <p className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">{message}</p>
  );
}

function messageFor(kind: ReturnType<typeof classifyEditError>, error: ApiError): string {
  switch (kind) {
    case "stale_revision":
      return "The draft changed since you loaded it. Reloading the latest version — please retry your edit.";
    case "operation_id_conflict":
      return "This edit conflicts with a previous request. Please try the edit again.";
    case "unknown_conflict":
      return "This change conflicts with the draft's current state. Please retry.";
    case "validation":
      return error.kind === "api" && error.detail ? error.detail : "This edit isn't valid. Please check the values and try again.";
    case "not_found":
      return "This item could not be found. It may have been removed by another edit.";
    case "forbidden":
      return "You don't have permission to make this change.";
    case "network":
      return "Network error — your edit was not saved. Please try again.";
    default:
      return "Something went wrong saving your edit. Please try again.";
  }
}
