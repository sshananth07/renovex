import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SetupProgress } from "./SetupProgress";

describe("SetupProgress", () => {
  it("shows all three steps as incomplete when nothing is set up", () => {
    render(
      <SetupProgress
        projectId="p1"
        progress={{
          client: "complete",
          project: "complete",
          property: "incomplete",
          spaces: "incomplete",
          workItems: "incomplete",
        }}
      />
    );

    expect(screen.getByText("0 of 3 complete")).toBeInTheDocument();
    expect(screen.getAllByText("Set up →")).toHaveLength(3);
  });

  it("shows completed steps distinctly and links to the right routes", () => {
    render(
      <SetupProgress
        projectId="p1"
        progress={{
          client: "complete",
          project: "complete",
          property: "complete",
          spaces: "incomplete",
          workItems: "incomplete",
        }}
      />
    );

    expect(screen.getByText("1 of 3 complete")).toBeInTheDocument();
    expect(screen.getAllByText("Complete")).toHaveLength(1);
    expect(screen.getByRole("link", { name: /property/i })).toHaveAttribute(
      "href",
      "/projects/p1/property"
    );
    expect(screen.getByRole("link", { name: /spaces/i })).toHaveAttribute(
      "href",
      "/projects/p1/spaces"
    );
    expect(screen.getByRole("link", { name: /work items/i })).toHaveAttribute(
      "href",
      "/projects/p1/work-items"
    );
  });

  it("shows every step complete once all resources exist", () => {
    render(
      <SetupProgress
        projectId="p1"
        progress={{
          client: "complete",
          project: "complete",
          property: "complete",
          spaces: "complete",
          workItems: "complete",
        }}
      />
    );

    expect(screen.getByText("3 of 3 complete")).toBeInTheDocument();
    expect(screen.getAllByText("Complete")).toHaveLength(3);
  });
});
