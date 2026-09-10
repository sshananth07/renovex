import { describe, expect, it } from "vitest";
import { aiKeys } from "./queryKeys";

describe("aiKeys", () => {
  it("batches key includes projectId and optional type", () => {
    expect(aiKeys.batches("p1")).toEqual(["ai", "batches", "p1", undefined]);
    expect(aiKeys.batches("p1", "space_suggestions")).toEqual(["ai", "batches", "p1", "space_suggestions"]);
  });

  it("suggestions key includes batchId", () => {
    expect(aiKeys.suggestions("batch1")).toEqual(["ai", "suggestions", "batch1"]);
  });

  it("resources key includes projectId and optional workItemId", () => {
    expect(aiKeys.resources("p1")).toEqual(["ai", "resources", "p1", undefined]);
    expect(aiKeys.resources("p1", "w1")).toEqual(["ai", "resources", "p1", "w1"]);
  });
});
