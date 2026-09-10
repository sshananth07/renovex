import { describe, expect, it } from "vitest";
import { deriveSetupProgress } from "./setupProgress";

describe("deriveSetupProgress", () => {
  it("marks client and project complete unconditionally, everything else incomplete when nothing exists", () => {
    const progress = deriveSetupProgress({
      hasProperty: false,
      spacesTotal: 0,
      workItemsTotal: 0,
    });

    expect(progress).toEqual({
      client: "complete",
      project: "complete",
      property: "incomplete",
      spaces: "incomplete",
      workItems: "incomplete",
    });
  });

  it("marks property complete once a Property exists", () => {
    const progress = deriveSetupProgress({
      hasProperty: true,
      spacesTotal: 0,
      workItemsTotal: 0,
    });

    expect(progress.property).toBe("complete");
    expect(progress.spaces).toBe("incomplete");
  });

  it("marks spaces complete once the Spaces total is greater than zero", () => {
    const progress = deriveSetupProgress({
      hasProperty: true,
      spacesTotal: 3,
      workItemsTotal: 0,
    });

    expect(progress.spaces).toBe("complete");
    expect(progress.workItems).toBe("incomplete");
  });

  it("marks all steps complete once every resource is present", () => {
    const progress = deriveSetupProgress({
      hasProperty: true,
      spacesTotal: 2,
      workItemsTotal: 5,
    });

    expect(progress).toEqual({
      client: "complete",
      project: "complete",
      property: "complete",
      spaces: "complete",
      workItems: "complete",
    });
  });
});
