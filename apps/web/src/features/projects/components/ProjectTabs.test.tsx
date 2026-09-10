import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ProjectTabs } from "./ProjectTabs";

vi.mock("next/navigation", () => ({
  usePathname: () => "/projects/p1/property",
}));

describe("ProjectTabs", () => {
  it("renders a tab for Overview, Property, Spaces, and Work Items, each linking into the project", () => {
    render(<ProjectTabs projectId="p1" />);

    expect(screen.getByRole("link", { name: "Overview" })).toHaveAttribute("href", "/projects/p1");
    expect(screen.getByRole("link", { name: "Property" })).toHaveAttribute(
      "href",
      "/projects/p1/property"
    );
    expect(screen.getByRole("link", { name: "Spaces" })).toHaveAttribute(
      "href",
      "/projects/p1/spaces"
    );
    expect(screen.getByRole("link", { name: "Work Items" })).toHaveAttribute(
      "href",
      "/projects/p1/work-items"
    );
  });

  it("marks the tab matching the current pathname as active", () => {
    render(<ProjectTabs projectId="p1" />);

    expect(screen.getByRole("link", { name: "Property" })).toHaveAttribute("data-active", "true");
    expect(screen.getByRole("link", { name: "Overview" })).not.toHaveAttribute("data-active", "true");
  });
});
