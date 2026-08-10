export interface NavigationItem {
  readonly to: string;
  readonly label: string;
}

export interface NavigationGroup {
  readonly label:
    "Overview" | "Explore" | "Run" | "Monitor" | "Portfolio & Risk" | "System";
  readonly items: readonly NavigationItem[];
}

// The one owner sees the complete product. Route handlers retain the
// reauthentication and fail-closed safety checks for consequential actions.
export const navigationGroups: readonly NavigationGroup[] = [
  {
    label: "Overview",
    items: [
      { to: "/", label: "Dashboard" },
      { to: "/getting-started", label: "Getting Started" },
      { to: "/glossary", label: "Glossary" },
    ],
  },
  {
    label: "Explore",
    items: [
      { to: "/strategies", label: "Strategies" },
      { to: "/exchanges", label: "Exchanges" },
      { to: "/data-catalogue", label: "Data Catalogue" },
      { to: "/opportunities", label: "Opportunities" },
    ],
  },
  {
    label: "Run",
    items: [
      { to: "/run-lab", label: "New Run" },
      { to: "/run-lab", label: "Run History" },
      { to: "/guided-demonstrations", label: "Guided Demonstrations" },
      { to: "/shadow", label: "Live Shadow" },
      { to: "/operations/sandbox", label: "Exchange Sandbox" },
    ],
  },
  {
    label: "Monitor",
    items: [
      { to: "/activity/decisions-orders", label: "Decisions" },
      { to: "/operations/orders", label: "Orders & Fills" },
      { to: "/activity/system-events", label: "System Events" },
      { to: "/operations/alerts", label: "Alerts & Incidents" },
    ],
  },
  {
    label: "Portfolio & Risk",
    items: [
      { to: "/portfolios", label: "Portfolio" },
      { to: "/inventory", label: "Inventory" },
      { to: "/risk", label: "Risk & Controls" },
      { to: "/rebalancing", label: "Rebalancing" },
    ],
  },
  {
    label: "System",
    items: [
      { to: "/operations", label: "Health & Storage" },
      { to: "/operations/configuration", label: "Configuration" },
      { to: "/operations/reports", label: "Reports" },
      { to: "/audit", label: "Audit" },
      { to: "/operations/qualifications", label: "Readiness" },
    ],
  },
] as const;

export function navigationFor() {
  return navigationGroups;
}
