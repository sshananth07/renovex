import { Building2, FileText, FolderKanban, LayoutDashboard, Settings, ShoppingCart, Users, type LucideIcon } from "lucide-react";

export interface NavItem {
  href: string;
  label: string;
  icon: LucideIcon;
}

export const navItems: NavItem[] = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/clients", label: "Clients", icon: Users },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/suppliers", label: "Suppliers", icon: Building2 },
  { href: "/procurement", label: "Procurement", icon: ShoppingCart },
  { href: "/quotations", label: "Quotations", icon: FileText },
  { href: "/company", label: "Company", icon: Settings },
];
