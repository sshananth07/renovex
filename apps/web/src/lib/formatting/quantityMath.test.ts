import { describe, expect, it } from "vitest";
import { addQuantity, subtractQuantity } from "./quantityMath";

describe("quantityMath", () => {
  it.each([
    ["0.1", "0.2", "0.3"],
    ["35", "8", "43"],
    ["35", "-3", "32"],
    ["1.20", "0.03", "1.23"],
    ["100000000000000000000.1", "0.2", "100000000000000000000.3"],
  ])("adds %s + %s exactly", (a, b, expected) => {
    expect(addQuantity(a, b)).toBe(expected);
  });

  it.each([
    ["1.00", "0.75", "0.25"],
    ["41", "33", "8"],
    ["30", "33", "-3"],
    ["1.2300", "0.23", "1"],
    ["0", "0.000", "0"],
  ])("subtracts %s - %s exactly", (a, b, expected) => {
    expect(subtractQuantity(a, b)).toBe(expected);
  });
});
