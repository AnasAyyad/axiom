import type { APIModel } from "../api/client";
import { hasAccess } from "../features/shared/access";

export interface NavigationItem {
  readonly to: string;
  readonly label: string;
  readonly permissions?: readonly string[];
}

export interface NavigationGroup {
  readonly label:
    | "Home"
    | "Activity"
    | "Strategies"
    | "Run Lab"
    | "Risk & Controls"
    | "Operations";
  readonly items: readonly NavigationItem[];
}

export const navigationGroups: readonly NavigationGroup[] = [
  {
    label: "Home",
    items: [
      { to: "/", label: "Overview" },
      {
        to: "/exchanges",
        label: "Exchanges",
        permissions: ["operations.read"],
      },
      {
        to: "/exchanges/binance",
        label: "Binance",
        permissions: ["operations.read"],
      },
      { to: "/assets", label: "Assets", permissions: ["operations.read"] },
      {
        to: "/opportunities",
        label: "Opportunities",
        permissions: ["operations.read"],
      },
      {
        to: "/portfolios",
        label: "Portfolio",
        permissions: ["operations.read"],
      },
      {
        to: "/inventory",
        label: "Inventory",
        permissions: ["operations.read"],
      },
      {
        to: "/rebalancing",
        label: "Rebalancing",
        permissions: ["operations.read"],
      },
    ],
  },
  {
    label: "Activity",
    items: [
      {
        to: "/activity/decisions-orders",
        label: "Decisions & Orders",
        permissions: ["activity.read"],
      },
      {
        to: "/activity/system-events",
        label: "System Events",
        permissions: ["activity.read"],
      },
      {
        to: "/operations/orders",
        label: "Orders",
        permissions: ["activity.read"],
      },
      {
        to: "/operations/fills",
        label: "Fills",
        permissions: ["activity.read"],
      },
    ],
  },
  {
    label: "Strategies",
    items: [
      {
        to: "/strategies",
        label: "Strategy Center",
        permissions: ["operations.read"],
      },
      {
        to: "/strategies/trend",
        label: "Trend",
        permissions: ["operations.read"],
      },
    ],
  },
  {
    label: "Run Lab",
    items: [
      {
        to: "/run-lab",
        label: "Approved runs",
        permissions: ["operations.read", "research.control"],
      },
      {
        to: "/backtests",
        label: "Backtest Lab",
        permissions: ["research.control"],
      },
      {
        to: "/replays",
        label: "Replay Lab",
        permissions: ["research.control"],
      },
      {
        to: "/shadow",
        label: "Shadow Center",
        permissions: ["operations.read", "research.control"],
      },
      {
        to: "/research/reports",
        label: "Reports",
        permissions: ["operations.read"],
      },
    ],
  },
  {
    label: "Risk & Controls",
    items: [
      { to: "/risk", label: "Risk Center", permissions: ["operations.read"] },
      {
        to: "/risk/controls",
        label: "Scoped controls",
        permissions: ["operations.read"],
      },
    ],
  },
  {
    label: "Operations",
    items: [
      {
        to: "/operations",
        label: "Operations home",
        permissions: ["operations.read"],
      },
      {
        to: "/operations/qualifications",
        label: "Qualifications",
        permissions: ["operations.read"],
      },
      {
        to: "/operations/sandbox",
        label: "Sandbox Operations",
        permissions: ["sandbox.read"],
      },
      {
        to: "/operations/alerts",
        label: "Alerts",
        permissions: ["operations.read"],
      },
      {
        to: "/incidents",
        label: "Incidents",
        permissions: ["operations.read"],
      },
      {
        to: "/audit",
        label: "Audit",
        permissions: ["audit.read", "audit.raw"],
      },
      {
        to: "/operations/reports",
        label: "Report jobs",
        permissions: ["operations.read"],
      },
      {
        to: "/operations/configuration",
        label: "Configuration",
        permissions: ["operations.read"],
      },
      {
        to: "/operations/users",
        label: "User access",
        permissions: ["roles.admin"],
      },
    ],
  },
] as const;

export function navigationFor(user: APIModel<"SessionUser">) {
  return navigationGroups
    .map((group) => ({
      ...group,
      items: group.items.filter(
        (item) =>
          item.permissions === undefined || hasAccess(user, item.permissions),
      ),
    }))
    .filter((group) => group.items.length > 0);
}
