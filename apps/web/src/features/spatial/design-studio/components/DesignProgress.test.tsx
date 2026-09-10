import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DesignProgress } from "./DesignProgress";
import type { DesignGenerationStatus } from "../types";

describe("DesignProgress", () => {
  it("shows the reference stage as active while reserved or generating_reference", () => {
    for (const status of ["reserved", "generating_reference"] as DesignGenerationStatus[]) {
      const { unmount } = render(<DesignProgress status={status} />);
      expect(screen.getByText("Preparing the visual direction")).toBeInTheDocument();
      unmount();
    }
  });

  it("marks reference done and geometry active for reference_ready/asset_generation_pending/asset_generation_processing", () => {
    for (const status of ["reference_ready", "asset_generation_pending", "asset_generation_processing"] as DesignGenerationStatus[]) {
      const { unmount } = render(<DesignProgress status={status} />);
      expect(screen.getByText("Shaping the 3D concept")).toBeInTheDocument();
      unmount();
    }
  });

  it("marks reference and geometry done once concept_ready", () => {
    render(<DesignProgress status="concept_ready" />);
    expect(screen.getByText("Checking the model in your room")).toBeInTheDocument();
  });

  it("never renders prohibited infra terms", () => {
    render(<DesignProgress status="asset_generation_processing" />);
    const text = document.body.textContent ?? "";
    for (const banned of ["Cloudflare", "FLUX", "Hugging Face", "Hunyuan", "ZeroGPU", "queue", "worker", "retr", "minutes"]) {
      expect(text).not.toContain(banned);
    }
  });
});
