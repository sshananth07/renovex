import { describe, expect, it } from "vitest";
import { majorToMinor, minorToMajorString } from "./money";

describe("minorToMajorString", () => {
  it("converts whole and fractional minor amounts exactly", () => {
    expect(minorToMajorString(0)).toBe("0.00");
    expect(minorToMajorString(100)).toBe("1.00");
    expect(minorToMajorString(12345)).toBe("123.45");
    expect(minorToMajorString(5)).toBe("0.05");
    expect(minorToMajorString(-850000)).toBe("-8500.00");
  });

  it("round-trips through majorToMinor for a range of values", () => {
    for (const minor of [0, 1, 50, 850000, 8500, 123456789]) {
      expect(majorToMinor(minorToMajorString(minor))).toBe(minor);
    }
  });
});
