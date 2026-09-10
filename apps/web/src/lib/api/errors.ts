import type { components } from "./generated/schema";

// Converts the backend's RFC7807 (application/problem+json) error body into
// a discriminated union feature code can switch on, so callers never touch
// the raw HTTP response shape directly. "network" covers the case where
// there was no response at all (request never reached the server).
type ErrorModel = components["schemas"]["ErrorModel"];
type ErrorDetail = components["schemas"]["ErrorDetail"];

export type ApiError =
  | {
      kind: "api";
      status: number;
      title?: string;
      detail?: string;
      fieldErrors: ErrorDetail[];
      // The RFC 9457 `type` field, when a route populates it with a stable
      // machine-readable code instead of leaving it at Huma's default. Most
      // routes don't set this — treat it as absent unless a specific route's
      // contract documents otherwise (e.g. the spatial RoomDraft-edit 409s;
      // see features/spatial/editor/operations.ts).
      code?: string;
    }
  | { kind: "network" };

interface RawErrorResponse {
  status?: number;
  body?: ErrorModel;
}

export function normalizeApiError(response: RawErrorResponse): ApiError {
  if (response.status === undefined || response.body === undefined) {
    return { kind: "network" };
  }

  return {
    kind: "api",
    status: response.status,
    title: response.body.title,
    detail: response.body.detail,
    fieldErrors: response.body.errors ?? [],
    code: response.body.type,
  };
}

/**
 * Unwraps an openapi-fetch `{ data, error, response }` result, throwing a
 * normalized ApiError (carrying HTTP status + field-level errors) instead of
 * the raw error body. Call sites that need field-level 422 mapping (e.g. via
 * setError) should use this instead of `if (error) throw error;` so status
 * survives past the throw.
 */
export function unwrapOrThrow<T>(result: { data?: T; error?: ErrorModel; response: Response }): T {
  if (result.error !== undefined) {
    throw normalizeApiError({ status: result.response.status, body: result.error });
  }
  return result.data as T;
}
