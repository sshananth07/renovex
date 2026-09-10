import { describe, expect, it } from "vitest";
import { safeSupplierReturnTo } from "./safeSupplierReturnTo";

describe("safeSupplierReturnTo", () => {
  it("accepts a relative path inside /supplier-access/", () => {
    expect(safeSupplierReturnTo(
      "/supplier-access/invitations/inv-1/outcomes/out-1",
    )).toBe("/supplier-access/invitations/inv-1/outcomes/out-1");
  });

  it("preserves query string and hash on an accepted path", () => {
    expect(safeSupplierReturnTo("/supplier-access/open?tab=history#section")).toBe(
      "/supplier-access/open?tab=history#section",
    );
  });

  it("rejects null and undefined", () => {
    expect(safeSupplierReturnTo(null)).toBeNull();
    expect(safeSupplierReturnTo(undefined)).toBeNull();
  });

  it("rejects an empty string", () => {
    expect(safeSupplierReturnTo("")).toBeNull();
  });

  it("rejects an absolute https URL", () => {
    expect(safeSupplierReturnTo("https://evil.com")).toBeNull();
  });

  it("rejects an absolute http URL", () => {
    expect(safeSupplierReturnTo("http://evil.com")).toBeNull();
  });

  it("rejects a protocol-relative URL", () => {
    expect(safeSupplierReturnTo("//evil.com")).toBeNull();
  });

  it("rejects a backslash-based URL", () => {
    expect(safeSupplierReturnTo("/\\evil.com")).toBeNull();
  });

  it("rejects a path missing its leading slash", () => {
    expect(safeSupplierReturnTo("supplier-access/open")).toBeNull();
  });

  it("rejects a path outside /supplier-access/", () => {
    expect(safeSupplierReturnTo("/admin/dashboard")).toBeNull();
  });

  it("rejects a path that only prefix-matches supplier-access without the boundary slash", () => {
    expect(safeSupplierReturnTo("/supplier-access-evil/open")).toBeNull();
  });

  it("rejects a bare /supplier-access without a trailing segment", () => {
    expect(safeSupplierReturnTo("/supplier-access")).toBeNull();
  });
});
