import { describe, expect, it } from "vitest";
import { resolveSourceUrl } from "./sourceUrl";

describe("resolveSourceUrl", () => {
  it("resolves a builtin source to its public path unchanged", () => {
    expect(resolveSourceUrl({ kind: "builtin", path: "/models/fixture-boiler-v1.glb" })).toBe(
      "/models/fixture-boiler-v1.glb",
    );
  });
});
