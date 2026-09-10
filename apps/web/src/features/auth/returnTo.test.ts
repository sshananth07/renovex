import { describe, expect, it } from "vitest";
import { parseSafeReturnTo } from "./returnTo";

describe("parseSafeReturnTo", () => {
  it("accepts a single-leading-slash internal path", () => {
    expect(parseSafeReturnTo("/projects/123")).toBe("/projects/123");
  });

  it("rejects an absolute external URL", () => {
    expect(parseSafeReturnTo("https://evil.example")).toBe("/dashboard");
  });

  it("rejects a protocol-relative URL", () => {
    expect(parseSafeReturnTo("//evil.example")).toBe("/dashboard");
  });

  it("rejects a value containing ://", () => {
    expect(parseSafeReturnTo("/redirect?to=javascript://evil")).toBe("/dashboard");
  });

  it("falls back to /dashboard for null", () => {
    expect(parseSafeReturnTo(null)).toBe("/dashboard");
  });

  it("falls back to /dashboard for an empty string", () => {
    expect(parseSafeReturnTo("")).toBe("/dashboard");
  });
});
