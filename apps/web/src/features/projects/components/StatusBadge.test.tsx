import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusBadge } from "./StatusBadge";

describe("StatusBadge", () => {
  it("renders the human-readable label for a raw status value", () => {
    render(<StatusBadge status="in_progress" />);
    expect(screen.getByText("In Progress")).toBeInTheDocument();
  });

  it("renders completed and lead statuses with distinct visual treatment", () => {
    render(
      <>
        <StatusBadge status="completed" />
        <StatusBadge status="lead" />
      </>
    );

    const completedBadge = screen.getByText("Completed");
    const leadBadge = screen.getByText("Lead");

    expect(completedBadge.className).not.toBe(leadBadge.className);
  });
});
