import { describe, expect, it } from "vitest";
import type { ApiError } from "@/lib/api/errors";
import { translateProcurementError } from "./procurementErrors";

function apiError(overrides: Partial<Extract<ApiError, { kind: "api" }>> = {}): ApiError {
  return { kind: "api", status: 422, fieldErrors: [], ...overrides };
}

describe("translateProcurementError", () => {
  it("maps a network error", () => {
    expect(translateProcurementError({ kind: "network" })).toEqual({
      kind: "network",
      message: "The request could not be completed. Check your connection and try again.",
    });
  });

  it("maps a resolution-operation-conflict detail before the generic eligibility branch", () => {
    const result = translateProcurementError(
      apiError({ status: 409, detail: "materialrequirements: resolution operation id belongs to a different resolution" })
    );
    expect(result.kind).toBe("resolution-operation-conflict");
    expect(result.message).toMatch(/close this review and start again/i);
  });

  it("maps an unresolved unit mismatch detail", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "unit mismatch is unresolved" }));
    expect(result.kind).toBe("unit-mismatch-unresolved");
  });

  it("maps an unresolved source discrepancy detail", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "source discrepancy is unresolved" }));
    expect(result.kind).toBe("source-discrepancy-unresolved");
  });

  it("maps an already-claimed detail from the discrepancy-resolution endpoint", () => {
    const result = translateProcurementError(
      apiError({ status: 409, detail: "requirement is claimed by an active RFQ chain; remove the RFQ line first" })
    );
    expect(result.kind).toBe("already-claimed");
  });

  it("maps an already-claimed detail from the RFQ add-line endpoint", () => {
    const result = translateProcurementError(
      apiError({ status: 409, detail: "that material requirement is already claimed by another rfq chain" })
    );
    expect(result.kind).toBe("already-claimed");
  });

  it("maps a stale-revision detail", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "requirement changed since it was read" }));
    expect(result.kind).toBe("stale-revision");
  });

  it("specific matches win over the broad requirement-not-eligible catch-all", () => {
    const result = translateProcurementError(
      apiError({ status: 422, detail: "an rfq line's requirement is not eligible: unit mismatch is unresolved" })
    );
    expect(result.kind).toBe("unit-mismatch-unresolved");
  });

  it("falls back to requirement-not-eligible for the rfqs-endpoint message", () => {
    const result = translateProcurementError(apiError({ status: 422, detail: "an rfq line's requirement is not eligible" }));
    expect(result.kind).toBe("requirement-not-eligible");
    expect(result.message).toMatch(/not currently eligible/i);
  });

  it("falls back to requirement-not-eligible for the materialrequirements-endpoint message", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "requirement is not eligible for an RFQ" }));
    expect(result.kind).toBe("requirement-not-eligible");
  });

  it("falls back to validation for other 422s", () => {
    const result = translateProcurementError(apiError({ status: 422, detail: "some other validation problem" }));
    expect(result.kind).toBe("validation");
    expect(result.message).toBe("some other validation problem");
  });

  it("falls back to conflict for other 409s", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "some other conflict" }));
    expect(result.kind).toBe("conflict");
  });

  it("falls back to unknown for anything else", () => {
    const result = translateProcurementError(apiError({ status: 500, detail: "boom" }));
    expect(result.kind).toBe("unknown");
    expect(result.message).toBe("boom");
  });

  it("is side-effect-free and never claims a refetch happened", () => {
    const result = translateProcurementError(apiError({ status: 409, detail: "requirement changed since it was read" }));
    expect(result.message).not.toMatch(/latest data has been loaded/i);
  });
});
