import { describe, expect, it } from "vitest";
import { normalizeApiError, unwrapOrThrow } from "./errors";

describe("normalizeApiError", () => {
  it("maps a problem+json ErrorModel body into a typed ApiError", () => {
    const result = normalizeApiError({
      status: 422,
      body: {
        type: "about:blank",
        status: 422,
        title: "Unprocessable Entity",
        detail: "Property name is required but is missing.",
        errors: [{ location: "body.name", message: "is required" }],
      },
    });

    expect(result).toEqual({
      kind: "api",
      status: 422,
      title: "Unprocessable Entity",
      detail: "Property name is required but is missing.",
      fieldErrors: [{ location: "body.name", message: "is required" }],
      code: "about:blank",
    });
  });

  it("defaults fieldErrors to an empty array when errors is absent", () => {
    const result = normalizeApiError({
      status: 404,
      body: { type: "about:blank", status: 404, title: "Not Found" },
    });

    expect(result.kind).toBe("api");
    if (result.kind === "api") {
      expect(result.fieldErrors).toEqual([]);
    }
  });

  it("returns a network-kind error when there is no response body", () => {
    const result = normalizeApiError({ status: undefined, body: undefined });

    expect(result).toEqual({ kind: "network" });
  });
});

describe("unwrapOrThrow", () => {
  it("returns data unchanged when there is no error", () => {
    const result = unwrapOrThrow({ data: { id: "abc" }, response: new Response(null, { status: 200 }) });

    expect(result).toEqual({ id: "abc" });
  });

  it("throws a normalized ApiError carrying the HTTP status when openapi-fetch returns an error", () => {
    const body = { type: "about:blank", status: 422, title: "Unprocessable Entity", errors: [{ location: "body.category", message: "invalid" }] };

    expect(() => unwrapOrThrow({ error: body, response: new Response(null, { status: 422 }) })).toThrowError();
    try {
      unwrapOrThrow({ error: body, response: new Response(null, { status: 422 }) });
    } catch (thrown) {
      expect(thrown).toEqual({
        kind: "api",
        status: 422,
        title: "Unprocessable Entity",
        detail: undefined,
        fieldErrors: [{ location: "body.category", message: "invalid" }],
        code: "about:blank",
      });
    }
  });
});
