import { queryOptions } from "@tanstack/react-query";

import { getAPI } from "./client";

export const exchangesQuery = queryOptions({
  queryKey: ["exchanges"],
  queryFn: () => getAPI<"ExchangePage">("/api/v1/exchanges?page_size=50"),
});

export function opportunitiesQuery(kind = "") {
  const filter = kind === "" ? "" : `&kind=${encodeURIComponent(kind)}`;
  return queryOptions({
    queryKey: ["opportunities", kind],
    queryFn: () =>
      getAPI<"OpportunityPage">(`/api/v1/opportunities?page_size=50${filter}`),
  });
}

export const strategiesQuery = queryOptions({
  queryKey: ["strategies"],
  queryFn: () => getAPI<"StrategyPage">("/api/v1/strategies?page_size=50"),
});

export function inventoryQuery(filters: {
  exchange: string;
  asset: string;
  strategy: string;
  portfolio: string;
}) {
  const parameters = new URLSearchParams({ page_size: "50" });
  for (const [key, value] of Object.entries(filters)) {
    if (value !== "") parameters.set(key, value);
  }
  return queryOptions({
    queryKey: ["inventory", filters],
    queryFn: () =>
      getAPI<"InventoryPage">(`/api/v1/inventory?${parameters.toString()}`),
  });
}

export const rebalancingQuery = queryOptions({
  queryKey: ["rebalancing"],
  queryFn: () =>
    getAPI<"RebalancingPage">(
      "/api/v1/rebalancing/recommendations?page_size=50",
    ),
});

export const championChallengerQuery = queryOptions({
  queryKey: ["champion-challenger"],
  queryFn: () =>
    getAPI<"ChampionChallengerPage">(
      "/api/v1/research/champion-challenger?page_size=50",
    ),
});

export const sandboxOverviewQuery = queryOptions({
  queryKey: ["sandbox", "overview"],
  queryFn: () => getAPI<"SandboxOverview">("/api/v1/sandbox/overview"),
  refetchInterval: 5_000,
});

export const sandboxOrdersQuery = queryOptions({
  queryKey: ["sandbox", "orders"],
  queryFn: () =>
    getAPI<"SandboxOrderPage">("/api/v1/sandbox/orders?page_size=100"),
  refetchInterval: 5_000,
});

export const sandboxReconciliationsQuery = queryOptions({
  queryKey: ["sandbox", "reconciliations"],
  queryFn: () =>
    getAPI<"SandboxReconciliationPage">(
      "/api/v1/sandbox/reconciliations?page_size=100",
    ),
  refetchInterval: 5_000,
});

export const sandboxQualificationQuery = queryOptions({
  queryKey: ["sandbox", "qualification"],
  queryFn: () =>
    getAPI<"SandboxQualificationStatus">("/api/v1/sandbox/qualification"),
  refetchInterval: 5_000,
});

export function auditQueryForType(eventType: string, includeDetail = false) {
  const eventFilter =
    eventType === "" ? "" : `&event_type=${encodeURIComponent(eventType)}`;
  const detailFilter = includeDetail ? "&include_detail=true" : "";
  return queryOptions({
    queryKey: ["audit", eventType, includeDetail],
    queryFn: () =>
      getAPI<"AuditEventPage">(
        `/api/v1/audit-events?page_size=50${eventFilter}${detailFilter}`,
      ),
  });
}

export type ActivityFilters = {
  readonly from: string;
  readonly to: string;
  readonly strategy: string;
  readonly instrument: string;
  readonly exchange: string;
  readonly side: string;
  readonly outcome: string;
  readonly reason: string;
  readonly mode: string;
  readonly correlation_id: string;
};

function filteredPath(
  path: string,
  filters: Readonly<Record<string, string>> = {},
) {
  const parameters = new URLSearchParams({ page_size: "50" });
  for (const [key, value] of Object.entries(filters)) {
    if (value.trim() !== "") parameters.set(key, value.trim());
  }
  return `${path}?${parameters.toString()}`;
}

export function activityQuery(
  view: "decisions_orders" | "system_events",
  filters: ActivityFilters,
) {
  const path = filteredPath("/api/v1/activity", { ...filters, view });
  return queryOptions({
    queryKey: ["activity", view, filters],
    queryFn: () => getAPI<"ActivityPage">(path),
  });
}

export function activityDetailQuery(id: string) {
  return queryOptions({
    queryKey: ["activity", "detail", id],
    queryFn: () =>
      getAPI<"ActivityResource">(`/api/v1/activity/${encodeURIComponent(id)}`),
    enabled: id !== "",
  });
}

export function strategyDetailQuery(id: string) {
  return queryOptions({
    queryKey: ["strategy", id],
    queryFn: () =>
      getAPI<"OwnerControlResource">(
        `/api/v1/strategies/${encodeURIComponent(id)}`,
      ),
    enabled: id !== "",
  });
}

export function strategyVersionsQuery(id: string) {
  return queryOptions({
    queryKey: ["strategy", id, "versions"],
    queryFn: () =>
      getAPI<"OwnerControlResourcePage">(
        `/api/v1/strategies/${encodeURIComponent(id)}/versions?page_size=50`,
      ),
    enabled: id !== "",
  });
}

export function ownerControlCollectionQuery(
  resource:
    | "assets"
    | "risk/controls"
    | "orders"
    | "fills"
    | "alerts"
    | "reports"
    | "configuration-revisions"
    | "lab-runs"
    | "qualifications"
    | "users",
  filters: Readonly<Record<string, string>> = {},
) {
  const path = filteredPath(`/api/v1/${resource}`, filters);
  return queryOptions({
    queryKey: ["owner_control", resource, filters],
    queryFn: () => getAPI<"OwnerControlResourcePage">(path),
  });
}
