import { describe, expect, it } from "vitest";
import { navItems } from "./navItems";

describe("contractor navItems", () => {
  it("contains only supported internal Contractor routes", () => {
    expect(navItems.map(({ href, label }) => ({ href, label }))).toEqual([
      { href: "/dashboard", label: "Dashboard" },
      { href: "/clients", label: "Clients" },
      { href: "/projects", label: "Projects" },
      { href: "/suppliers", label: "Suppliers" },
      { href: "/procurement", label: "Procurement" },
      { href: "/quotations", label: "Quotations" },
      { href: "/company", label: "Company" },
    ]);
    expect(navItems.some(({ label }) => label.includes("Portal"))).toBe(false);
  });
});
