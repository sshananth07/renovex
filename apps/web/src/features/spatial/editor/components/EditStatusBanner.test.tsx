import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { classifyEditError, EditStatusBanner } from "./EditStatusBanner";
import type { ApiError } from "@/lib/api/errors";

function apiError(overrides: Partial<Extract<ApiError, { kind: "api" }>>): ApiError {
  return { kind: "api", status: 500, fieldErrors: [], ...overrides };
}

describe("classifyEditError", () => {
  it("classifies a 409 with code=stale_revision", () => {
    expect(classifyEditError(apiError({ status: 409, code: "stale_revision" }))).toBe("stale_revision");
  });

  it("classifies a 409 with code=operation_id_conflict distinctly from stale_revision", () => {
    expect(classifyEditError(apiError({ status: 409, code: "operation_id_conflict" }))).toBe("operation_id_conflict");
  });

  it("does not reinterpret an operation_id_conflict as stale_revision", () => {
    const result = classifyEditError(apiError({ status: 409, code: "operation_id_conflict" }));
    expect(result).not.toBe("stale_revision");
  });

  it("falls back to unknown_conflict for a 409 without a recognized code", () => {
    expect(classifyEditError(apiError({ status: 409, code: undefined }))).toBe("unknown_conflict");
  });

  it("classifies 422 as validation, 404 as not_found, 403 as forbidden", () => {
    expect(classifyEditError(apiError({ status: 422 }))).toBe("validation");
    expect(classifyEditError(apiError({ status: 404 }))).toBe("not_found");
    expect(classifyEditError(apiError({ status: 403 }))).toBe("forbidden");
  });

  it("classifies a network error distinctly", () => {
    expect(classifyEditError({ kind: "network" })).toBe("network");
  });
});

describe("EditStatusBanner", () => {
  it("shows a pending message while saving", () => {
    render(<EditStatusBanner pending error={null} />);
    expect(screen.getByText(/saving your change/i)).toBeInTheDocument();
  });

  it("renders nothing when there is no error and nothing is pending", () => {
    const { container } = render(<EditStatusBanner pending={false} error={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows a distinct message for stale_revision vs operation_id_conflict", () => {
    const { rerender } = render(<EditStatusBanner pending={false} error={apiError({ status: 409, code: "stale_revision" })} />);
    expect(screen.getByText(/draft changed/i)).toBeInTheDocument();

    rerender(<EditStatusBanner pending={false} error={apiError({ status: 409, code: "operation_id_conflict" })} />);
    expect(screen.getByText(/conflicts with a previous request/i)).toBeInTheDocument();
  });

  it("shows the network-failure message without implying success", () => {
    render(<EditStatusBanner pending={false} error={{ kind: "network" }} />);
    expect(screen.getByText(/not saved/i)).toBeInTheDocument();
  });
});
