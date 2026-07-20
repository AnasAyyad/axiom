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
