import { queryOptions } from "@tanstack/react-query";

import { getAPI } from "./client";

export const sessionQuery = queryOptions({
  queryKey: ["session"],
  queryFn: () => getAPI<"SessionMe">("/api/v1/session/me"),
  retry: false,
  staleTime: 30_000,
});

export const runCatalogQuery = queryOptions({
  queryKey: ["run-catalog"],
  queryFn: () => getAPI<"RunCatalog">("/api/v1/run-catalog"),
  staleTime: 30_000,
});

export const runsQuery = queryOptions({
  queryKey: ["runs"],
  queryFn: () => getAPI<"RunPage">("/api/v1/runs"),
  refetchInterval: 5_000,
});

export const dataCatalogueQuery = queryOptions({
  queryKey: ["data-catalogue"],
  queryFn: () => getAPI<"DataCataloguePage">("/api/v1/data-catalogue"),
  staleTime: 30_000,
});

export const systemQuery = queryOptions({
  queryKey: ["system"],
  queryFn: () => getAPI<"SystemStatus">("/api/v1/system/status"),
});

export const binanceQuery = queryOptions({
  queryKey: ["binance-health"],
  queryFn: () => getAPI<"BinanceHealth">("/api/v1/exchanges/binance/health"),
});

export const portfolioQuery = queryOptions({
  queryKey: ["portfolios"],
  queryFn: () => getAPI<"PortfolioPage">("/api/v1/portfolios?page_size=50"),
});

export const riskQuery = queryOptions({
  queryKey: ["risk"],
  queryFn: () => getAPI<"RiskStatus">("/api/v1/risk/status"),
});

export const trendQuery = queryOptions({
  queryKey: ["trend"],
  queryFn: () => getAPI<"TrendStatus">("/api/v1/strategies/trend"),
});

export const decisionsQuery = queryOptions({
  queryKey: ["trend-decisions"],
  queryFn: () =>
    getAPI<"TrendDecisionPage">(
      "/api/v1/strategies/trend/decisions?page_size=50",
    ),
});

export const incidentsQuery = queryOptions({
  queryKey: ["incidents"],
  queryFn: () => getAPI<"IncidentPage">("/api/v1/incidents?page_size=50"),
});

export function incidentsQueryForState(state: string) {
  const filter = state === "" ? "" : `&state=${encodeURIComponent(state)}`;
  return queryOptions({
    queryKey: ["incidents", state],
    queryFn: () =>
      getAPI<"IncidentPage">(`/api/v1/incidents?page_size=50${filter}`),
  });
}

export const auditQuery = queryOptions({
  queryKey: ["audit"],
  queryFn: () => getAPI<"AuditEventPage">("/api/v1/audit-events?page_size=50"),
});

export const auditVerificationQuery = queryOptions({
  queryKey: ["audit", "verification"],
  queryFn: () => getAPI<"AuditVerification">("/api/v1/audit-verification"),
});

export const reportSchedulesQuery = queryOptions({
  queryKey: ["report-schedules"],
  queryFn: () =>
    getAPI<"ReportSchedulePage">("/api/v1/report-schedules?page_size=50"),
});

export function reportDetailQuery(id: string) {
  return queryOptions({
    queryKey: ["report", id],
    queryFn: () =>
      getAPI<"ReportResource">(`/api/v1/reports/${encodeURIComponent(id)}`),
    enabled: id !== "",
  });
}

export const alertRoutesQuery = queryOptions({
  queryKey: ["alert-routes"],
  queryFn: () => getAPI<"AlertRoutePage">("/api/v1/alert-routes"),
});

export function alertDetailQuery(id: string) {
  return queryOptions({
    queryKey: ["alert", id],
    queryFn: () =>
      getAPI<"AlertDetail">(`/api/v1/alerts/${encodeURIComponent(id)}`),
    enabled: id !== "",
  });
}

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

export const c6QualificationQuery = queryOptions({
  queryKey: ["sandbox", "qualification"],
  queryFn: () =>
    getAPI<"C6QualificationStatus">("/api/v1/sandbox/qualification"),
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
      getAPI<"D1Resource">(`/api/v1/strategies/${encodeURIComponent(id)}`),
    enabled: id !== "",
  });
}

export function strategyVersionsQuery(id: string) {
  return queryOptions({
    queryKey: ["strategy", id, "versions"],
    queryFn: () =>
      getAPI<"D1ResourcePage">(
        `/api/v1/strategies/${encodeURIComponent(id)}/versions?page_size=50`,
      ),
    enabled: id !== "",
  });
}

export function d1CollectionQuery(
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
    queryKey: ["d1", resource, filters],
    queryFn: () => getAPI<"D1ResourcePage">(path),
  });
}
