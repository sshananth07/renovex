import { describe, expect, it } from "vitest";
import { formatDate, toRFC3339 } from "./date";

describe("formatDate", () => {
  it("formats an ISO timestamp as DD/MM/YYYY", () => {
    expect(formatDate("2026-01-15T00:00:00Z")).toBe("15/01/2026");
  });
});

describe("toRFC3339", () => {
  const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$/;

  it("converts a datetime-local value into a full RFC3339 timestamp", () => {
    const result = toRFC3339("2026-08-20T17:00");
    expect(result).not.toBeNull();
    expect(result).toMatch(rfc3339);
  });

  it("represents the same instant regardless of seconds precision in the input", () => {
    expect(toRFC3339("2026-08-20T17:00")).toBe(toRFC3339("2026-08-20T17:00:00"));
  });

  it("returns null for a blank value instead of throwing", () => {
    expect(toRFC3339("")).toBeNull();
  });

  it("returns null for an unparseable value instead of throwing", () => {
    expect(toRFC3339("not-a-date")).toBeNull();
  });

  it("never produces a bare date with no time component", () => {
    const result = toRFC3339("2026-08-20T17:00");
    expect(result).not.toBe("2026-08-20");
  });
});
