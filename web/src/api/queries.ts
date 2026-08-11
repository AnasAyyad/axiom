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

export const guidedDemonstrationsQuery = queryOptions({
  queryKey: ["guided-demonstrations"],
  queryFn: () => getAPI<"GuidedDemonstrationPage">("/api/v1/demonstrations"),
  staleTime: 30_000,
});

export function guidedDemonstrationQuery(id: string) {
  return queryOptions({
    queryKey: ["guided-demonstration", id],
    queryFn: () =>
      getAPI<"GuidedDemonstrationResult">(
        `/api/v1/demonstrations/${encodeURIComponent(id)}`,
      ),
    enabled: id !== "",
    staleTime: Infinity,
  });
}

export const runsQuery = queryOptions({
  queryKey: ["runs"],
  queryFn: () => getAPI<"RunPage">("/api/v1/runs"),
  refetchInterval: 5_000,
});

export function runQuery(id: string) {
  return queryOptions({
    queryKey: ["run", id],
    queryFn: () =>
      getAPI<"RunResource">(`/api/v1/runs/${encodeURIComponent(id)}`),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function runOutputsQuery(
  id: string,
  view: "timeline" | "decisions" | "orders" | "fills",
) {
  return queryOptions({
    queryKey: ["run", id, view],
    queryFn: () =>
      getAPI<"RunOutputPage">(`/api/v1/runs/${encodeURIComponent(id)}/${view}`),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function runPortfolioProjectionQuery(id: string) {
  return queryOptions({
    queryKey: ["run", id, "portfolio"],
    queryFn: () =>
      getAPI<"RunPortfolioProjection">(
        `/api/v1/runs/${encodeURIComponent(id)}/portfolio`,
      ),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function runRiskProjectionQuery(id: string) {
  return queryOptions({
    queryKey: ["run", id, "risk"],
    queryFn: () =>
      getAPI<"RunRiskProjection">(
        `/api/v1/runs/${encodeURIComponent(id)}/risk`,
      ),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function runEvidenceQuery(id: string) {
  return queryOptions({
    queryKey: ["run", id, "evidence"],
    queryFn: () =>
      getAPI<"RunEvidence">(`/api/v1/runs/${encodeURIComponent(id)}/evidence`),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export const dataCatalogueQuery = queryOptions({
  queryKey: ["data-catalogue"],
  queryFn: () => getAPI<"DataCataloguePage">("/api/v1/data-catalogue"),
  staleTime: 30_000,
});

export const evaluationCampaignsQuery = queryOptions({
  queryKey: ["evaluation-campaigns"],
  queryFn: () =>
    getAPI<"EvaluationCampaignPage">("/api/v1/evaluation-campaigns"),
  refetchInterval: 5_000,
});

export function evaluationCampaignQuery(id: string) {
  return queryOptions({
    queryKey: ["evaluation-campaign", id],
    queryFn: () =>
      getAPI<"EvaluationCampaign">(
        `/api/v1/evaluation-campaigns/${encodeURIComponent(id)}`,
      ),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function evaluationCampaignEventsQuery(id: string) {
  return queryOptions({
    queryKey: ["evaluation-campaign", id, "events"],
    queryFn: () =>
      getAPI<"EvaluationCampaignEventPage">(
        `/api/v1/evaluation-campaigns/${encodeURIComponent(id)}/events`,
      ),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

export function evaluationCampaignReportQuery(id: string) {
  return queryOptions({
    queryKey: ["evaluation-campaign", id, "report"],
    queryFn: () =>
      getAPI<"EvaluationCampaignReport">(
        `/api/v1/evaluation-campaigns/${encodeURIComponent(id)}/report`,
      ),
    enabled: id !== "",
    refetchInterval: 5_000,
  });
}

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

export * from "./queriesConsole";
