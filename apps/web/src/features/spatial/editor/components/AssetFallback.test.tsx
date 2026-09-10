import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import AssetFallback from "./AssetFallback";

describe("AssetFallback", () => {
  it("renders a boxGeometry sized from the given dimensions", () => {
    const { container } = render(<AssetFallback dimensions={{ x: 1, y: 2, z: 3 }} color="#8b5cf6" />);
    const box = container.querySelector("boxgeometry");
    expect(box).not.toBeNull();
  });

  it("renders exactly one mesh, matching the existing procedural fixture look", () => {
    const { container } = render(<AssetFallback dimensions={{ x: 0.5, y: 0.5, z: 0.5 }} color="#64748b" />);
    expect(container.querySelectorAll("mesh").length).toBe(1);
    expect(container.querySelectorAll("meshstandardmaterial").length).toBe(1);
  });

  it("uses the plain selection/hover color and leaves roughness/metalness unset when no appearance is given", () => {
    const { container } = render(<AssetFallback dimensions={{ x: 0.5, y: 0.5, z: 0.5 }} color="#64748b" />);
    const material = container.querySelector("meshstandardmaterial")!;
    expect(material.getAttribute("color")).toBe("#64748b");
    expect(material.hasAttribute("roughness")).toBe(false);
    expect(material.hasAttribute("metalness")).toBe(false);
  });

  it("maps an appearance override to the plan's exact roughness table and overrides the plain color", () => {
    const { container } = render(
      <AssetFallback
        dimensions={{ x: 0.5, y: 0.5, z: 0.5 }}
        color="#64748b"
        appearance={{ baseColor: "#2f4f3a", materialFamily: "fabric", roughness: "satin", metallic: false }}
      />,
    );
    const material = container.querySelector("meshstandardmaterial")!;
    expect(material.getAttribute("color")).toBe("#2f4f3a");
    expect(material.getAttribute("roughness")).toBe("0.48");
    expect(material.getAttribute("metalness")).toBe("0");
  });

  it("maps metallic=true to metalness 1", () => {
    const { container } = render(
      <AssetFallback
        dimensions={{ x: 0.5, y: 0.5, z: 0.5 }}
        color="#64748b"
        appearance={{ baseColor: "#c8a464", materialFamily: "metal", roughness: "glossy", metallic: true }}
      />,
    );
    const material = container.querySelector("meshstandardmaterial")!;
    expect(material.getAttribute("roughness")).toBe("0.18");
    expect(material.getAttribute("metalness")).toBe("1");
  });
});
