import { describe, expect, it } from "vitest";
import { PROJECT_STATUSES, projectStatusLabel, projectStatusTone } from "./statusPresentation";

describe("projectStatusLabel", () => {
  it("exposes exactly the backend's eight selectable statuses", () => {
    expect(PROJECT_STATUSES).toEqual([
      "lead",
      "site_visit",
      "estimating",
      "quotation_sent",
      "quotation_approved",
      "in_progress",
      "completed",
      "closed",
    ]);
    expect(PROJECT_STATUSES.map(projectStatusLabel)).toEqual([
      "Lead",
      "Site Visit",
      "Estimating",
      "Quotation Sent",
      "Quotation Approved",
      "In Progress",
      "Completed",
      "Closed",
    ]);
  });

  it("renders each backend status as a readable label", () => {
    expect(projectStatusLabel("lead")).toBe("Lead");
    expect(projectStatusLabel("site_visit")).toBe("Site Visit");
    expect(projectStatusLabel("quotation_sent")).toBe("Quotation Sent");
    expect(projectStatusLabel("in_progress")).toBe("In Progress");
    expect(projectStatusLabel("completed")).toBe("Completed");
    expect(projectStatusLabel("closed")).toBe("Closed");
  });

  it("falls back to the raw value for an unrecognized status", () => {
    expect(projectStatusLabel("mystery_status")).toBe("mystery_status");
  });
});

describe("projectStatusTone", () => {
  it("assigns distinct tones for early-pipeline, active, and terminal statuses", () => {
    expect(projectStatusTone("lead")).toBe("neutral");
    expect(projectStatusTone("in_progress")).toBe("active");
    expect(projectStatusTone("completed")).toBe("success");
    expect(projectStatusTone("closed")).toBe("neutral");
  });
});
