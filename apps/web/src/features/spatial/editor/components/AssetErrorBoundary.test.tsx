import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import AssetErrorBoundary from "./AssetErrorBoundary";

function Boom(): never {
  throw new Error("load failed");
}

function Fine() {
  return <div data-testid="loaded" />;
}

describe("AssetErrorBoundary", () => {
  it("renders the fallback when its child throws", () => {
    render(
      <AssetErrorBoundary assetKey="a:1" fallback={<div data-testid="fallback" />}>
        <Boom />
      </AssetErrorBoundary>,
    );
    expect(screen.getByTestId("fallback")).toBeInTheDocument();
  });

  it("renders children normally when nothing throws", () => {
    render(
      <AssetErrorBoundary assetKey="a:1" fallback={<div data-testid="fallback" />}>
        <Fine />
      </AssetErrorBoundary>,
    );
    expect(screen.getByTestId("loaded")).toBeInTheDocument();
    expect(screen.queryByTestId("fallback")).not.toBeInTheDocument();
  });

  it("recovers when assetKey changes after a trip, instead of staying stuck on the stale error", () => {
    const { rerender } = render(
      <AssetErrorBoundary assetKey="a:1" fallback={<div data-testid="fallback" />}>
        <Boom />
      </AssetErrorBoundary>,
    );
    expect(screen.getByTestId("fallback")).toBeInTheDocument();

    rerender(
      <AssetErrorBoundary assetKey="a:2" fallback={<div data-testid="fallback" />}>
        <Fine />
      </AssetErrorBoundary>,
    );
    expect(screen.getByTestId("loaded")).toBeInTheDocument();
    expect(screen.queryByTestId("fallback")).not.toBeInTheDocument();
  });

  it("stays tripped on the fallback if assetKey does not change, even after a re-render", () => {
    const { rerender } = render(
      <AssetErrorBoundary assetKey="a:1" fallback={<div data-testid="fallback" />}>
        <Boom />
      </AssetErrorBoundary>,
    );
    expect(screen.getByTestId("fallback")).toBeInTheDocument();

    rerender(
      <AssetErrorBoundary assetKey="a:1" fallback={<div data-testid="fallback" />}>
        <Fine />
      </AssetErrorBoundary>,
    );
    // Same key: React error boundaries don't naturally re-run children on
    // prop changes alone once tripped — still on the fallback.
    expect(screen.getByTestId("fallback")).toBeInTheDocument();
  });
});
