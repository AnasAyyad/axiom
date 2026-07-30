import { queryOptions } from "@tanstack/react-query";

import { getAPI } from "./client";

export const sessionQuery = queryOptions({
  queryKey: ["session"],
  queryFn: () => getAPI<"SessionMe">("/api/v1/session/me"),
  retry: false,
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
