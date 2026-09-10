import { describe, expect, it } from "vitest";
import { deriveAISetupStage } from "./aiSetupProgress";

describe("deriveAISetupStage", () => {
  it("is 'brief' when scope brief is empty", () => {
    expect(deriveAISetupStage({ scopeBrief: "", confirmedSpaces: 0, confirmedWorkItems: 0 })).toBe("brief");
    expect(deriveAISetupStage({ scopeBrief: "   ", confirmedSpaces: 0, confirmedWorkItems: 0 })).toBe("brief");
  });

  it("is 'spaces' when brief is set but no confirmed Spaces exist", () => {
    expect(deriveAISetupStage({ scopeBrief: "Full renovation.", confirmedSpaces: 0, confirmedWorkItems: 0 })).toBe(
      "spaces"
    );
  });

  it("is 'workItems' when Spaces exist but no confirmed WorkItems exist", () => {
    expect(
      deriveAISetupStage({ scopeBrief: "Full renovation.", confirmedSpaces: 2, confirmedWorkItems: 0 })
    ).toBe("workItems");
  });

  it("is 'resources' when both Spaces and WorkItems are confirmed", () => {
    expect(
      deriveAISetupStage({ scopeBrief: "Full renovation.", confirmedSpaces: 2, confirmedWorkItems: 3 })
    ).toBe("resources");
  });

  it("does not require accepting every suggestion — only real confirmed counts matter", () => {
    // A project with manually-created Spaces (never touched by AI at all)
    // still unlocks Work Item generation.
    expect(
      deriveAISetupStage({ scopeBrief: "Full renovation.", confirmedSpaces: 1, confirmedWorkItems: 0 })
    ).toBe("workItems");
  });
});
