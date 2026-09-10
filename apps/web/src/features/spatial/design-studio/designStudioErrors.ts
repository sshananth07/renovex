import type { ApiError } from "@/lib/api/errors";

// Classifies design-session/design-generation failures into the categories
// the studio needs distinct copy for — mirrors editor/components/
// EditStatusBanner.tsx's classifyEditError convention exactly, but reads
// this feature's own error `code` vocabulary
// (backend design_handler.go's mapDesignError + designgeneration_handler.go's
// mapDesignGenerationError).
export type DesignStudioErrorKind =
  | "stale_plan"
  | "turn_in_progress"
  | "generation_in_progress"
  | "attempt_superseded"
  | "attempt_not_ready"
  | "attempt_abandoned"
  | "reasoning_unavailable"
  | "not_found"
  | "unsupported_target"
  | "request_conflict"
  | "forbidden"
  | "network"
  | "unknown";

export function classifyDesignStudioError(error: ApiError): DesignStudioErrorKind {
  if (error.kind === "network") return "network";
  if (error.status === 404) return "not_found";
  if (error.status === 403) return "forbidden";
  if (error.status === 422) {
    if (error.code === "design_attempt_not_ready") return "attempt_not_ready";
    if (error.code === "unsupported_design_target") return "unsupported_target";
    return "attempt_abandoned";
  }
  if (error.status === 409) {
    switch (error.code) {
      case "design_plan_stale":
        return "stale_plan";
      case "design_turn_in_progress":
        return "turn_in_progress";
      case "design_generation_in_progress":
        return "generation_in_progress";
      case "design_generation_superseded":
        return "attempt_superseded";
      default:
        return "request_conflict";
    }
  }
  if (error.status === 503 || error.status === 502 || error.status === 504) return "reasoning_unavailable";
  return "unknown";
}

export function designStudioErrorMessage(kind: DesignStudioErrorKind): string {
  switch (kind) {
    case "stale_plan":
      return "The room changed. Review this idea again.";
    case "turn_in_progress":
      return "Still working on the previous idea — one at a time.";
    case "generation_in_progress":
      return "Already creating a concept for this idea.";
    case "attempt_superseded":
      return "A newer concept replaced this one.";
    case "attempt_not_ready":
      return "This concept isn't ready yet.";
    case "attempt_abandoned":
      return "This concept was cancelled.";
    case "reasoning_unavailable":
      return "AI Design is temporarily unavailable. Please try again shortly.";
    case "not_found":
      return "This design session could not be found.";
    case "unsupported_target":
      return "AI Design currently works with movable objects and fixtures.";
    case "forbidden":
      return "You don't have permission to do this.";
    case "network":
      return "Network error — please try again.";
    case "request_conflict":
    case "unknown":
    default:
      return "Something went wrong. Please try again.";
  }
}
