// Authoritative frontend representation of the backend's 8-value ProjectStatus
// enum (phase1.md §5 and backend/internal/projects/project.go). Keep selection
// options, labels and badge tones sourced from this one collection.
export const PROJECT_STATUSES = [
  "lead",
  "site_visit",
  "estimating",
  "quotation_sent",
  "quotation_approved",
  "in_progress",
  "completed",
  "closed",
] as const;

export type ProjectStatus = (typeof PROJECT_STATUSES)[number];

const LABELS: Record<ProjectStatus, string> = {
  lead: "Lead",
  site_visit: "Site Visit",
  estimating: "Estimating",
  quotation_sent: "Quotation Sent",
  quotation_approved: "Quotation Approved",
  in_progress: "In Progress",
  completed: "Completed",
  closed: "Closed",
};

export function projectStatusLabel(status: string): string {
  return LABELS[status as ProjectStatus] ?? status;
}

export type StatusTone = "neutral" | "active" | "success";

// Three-tone system: early-pipeline/terminal statuses read as neutral,
// active work is the one accent-bearing state, completion reads as success
// — matches the product register's "accent for state indicators only".
const ACTIVE_STATUSES = new Set([
  "site_visit",
  "estimating",
  "quotation_sent",
  "quotation_approved",
  "in_progress",
]);

export function projectStatusTone(status: string): StatusTone {
  if (status === "completed") return "success";
  if (ACTIVE_STATUSES.has(status)) return "active";
  return "neutral";
}
