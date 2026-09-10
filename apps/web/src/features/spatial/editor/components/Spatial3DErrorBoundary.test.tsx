import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Spatial3DErrorBoundary } from "./Spatial3DErrorBoundary";

function Boom(): never {
  throw new Error("WebGL context creation failed");
}

describe("Spatial3DErrorBoundary", () => {
  it("renders children normally when there is no error", () => {
    render(
      <Spatial3DErrorBoundary>
        <div>3D scene</div>
      </Spatial3DErrorBoundary>,
    );
    expect(screen.getByText("3D scene")).toBeInTheDocument();
  });

  it("shows a graceful fallback instead of crashing when the child throws (e.g. WebGL init failure)", () => {
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <Spatial3DErrorBoundary>
        <Boom />
      </Spatial3DErrorBoundary>,
    );
    expect(screen.getByText(/3d view unavailable/i)).toBeInTheDocument();
    expect(screen.getByText(/switch to the 2d floor plan/i)).toBeInTheDocument();
    consoleError.mockRestore();
  });
});
