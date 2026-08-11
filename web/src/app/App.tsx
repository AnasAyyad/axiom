import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import { BrowserRouter, Navigate, Outlet, Route, Routes } from "react-router";

import { sessionQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { AppShell } from "./AppShell";
import { LoginPage } from "./LoginPage";
import {
  BinancePage,
  CommandCenter,
  AuditPage,
  PortfolioPage,
  RiskPage,
  TrendPage,
} from "./OperationalPages";
import {
  ExchangesPage,
  InventoryPage as MultiExchangeInventoryPage,
  OpportunityScanner,
  RebalancingCenter,
  ResearchReports,
} from "./MultiExchangePages";
import { ActivityPage } from "../features/activity/ActivityPage";
import { AlertCenterPage } from "../features/operations/AlertCenterPage";
import { AlertDetailPage } from "../features/operations/AlertDetailPage";
import { ConfigurationCenterPage } from "../features/operations/ConfigurationCenterPage";
import { OperationsHubPage } from "../features/operations/OperationsHubPage";
import { IncidentCenterPage } from "../features/operations/IncidentCenterPage";
import { IncidentWorkspacePage } from "../features/operations/IncidentWorkspacePage";
import { ReportCenterPage } from "../features/operations/ReportCenterPage";
import { ReportDetailPage } from "../features/operations/ReportDetailPage";
import { ResourceCollectionPage } from "../features/operations/ResourceCollectionPage";
import { QualificationCenterPage } from "../features/qualifications/QualificationCenterPage";
import { RiskControlsPage } from "../features/risk/RiskControlsPage";
import { RunLabPage } from "../features/run-lab/RunLabPage";
import { RunDetailPage } from "../features/run-lab/RunDetailPage";
import { DataCataloguePage } from "../features/run-lab/DataCataloguePage";
import {
  GettingStartedPage,
  GlossaryPage,
  GuidedDemonstrationsPage,
} from "../features/run-lab/OwnerGuidancePages";
import { StrategyCenterPage } from "../features/strategies/StrategyCenterPage";
import { StrategyEvaluationPage } from "../features/evaluation/StrategyEvaluationPage";

const BacktestLab = lazy(() =>
  import("./LabPages").then((module) => ({ default: module.BacktestLab })),
);
const ReplayLab = lazy(() =>
  import("./LabPages").then((module) => ({ default: module.ReplayLab })),
);
const ShadowCenter = lazy(() =>
  import("./LabPages").then((module) => ({ default: module.ShadowCenter })),
);
const SandboxOperationsPage = lazy(() =>
  import("./SandboxOperationsPage").then((module) => ({
    default: module.SandboxOperationsPage,
  })),
);

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 5_000, retry: 1, refetchOnWindowFocus: false },
  },
});

/** App composes the authenticated owner console and authoritative server-state cache. */
export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Suspense
          fallback={
            <main>
              <StatePanel state="loading" />
            </main>
          }
        >
          <Routes>
            <Route path="/login" element={<LoginPage />} />
            <Route element={<AuthenticatedShell />}>
              <Route index element={<CommandCenter />} />
              <Route path="getting-started" element={<GettingStartedPage />} />
              <Route path="glossary" element={<GlossaryPage />} />
              <Route path="exchanges" element={<ExchangesPage />} />
              <Route path="data-catalogue" element={<DataCataloguePage />} />
              <Route path="exchanges/binance" element={<BinancePage />} />
              <Route
                path="assets"
                element={
                  <ResourceCollectionPage
                    resource="assets"
                    title="Asset Universe"
                    eyebrow="Approved spot assets"
                    description="Review screening state and spot-only eligibility. Unsupported or unsafe assets remain excluded."
                  />
                }
              />
              <Route path="opportunities" element={<OpportunityScanner />} />
              <Route path="portfolios" element={<PortfolioPage />} />
              <Route path="portfolios/:id" element={<PortfolioPage />} />
              <Route path="risk" element={<RiskPage />} />
              <Route path="risk/controls" element={<RiskControlsPage />} />
              <Route path="strategies/trend" element={<TrendPage />} />
              <Route path="strategies" element={<StrategyCenterPage />} />
              <Route
                path="inventory"
                element={<MultiExchangeInventoryPage />}
              />
              <Route path="rebalancing" element={<RebalancingCenter />} />
              <Route path="research/reports" element={<ResearchReports />} />
              <Route
                path="activity/decisions-orders"
                element={<ActivityPage view="decisions_orders" />}
              />
              <Route
                path="activity/system-events"
                element={<ActivityPage view="system_events" />}
              />
              <Route path="run-lab" element={<RunLabPage />} />
              <Route
                path="strategy-evaluation"
                element={<StrategyEvaluationPage />}
              />
              <Route
                path="strategy-evaluation/:id"
                element={<StrategyEvaluationPage />}
              />
              <Route path="runs/:id" element={<RunDetailPage />} />
              <Route
                path="guided-demonstrations"
                element={<GuidedDemonstrationsPage />}
              />
              <Route path="backtests" element={<BacktestLab />} />
              <Route path="backtests/:id" element={<BacktestLab />} />
              <Route path="replays" element={<ReplayLab />} />
              <Route path="replays/:id" element={<ReplayLab />} />
              <Route path="shadow" element={<ShadowCenter />} />
              <Route path="shadow/:id" element={<ShadowCenter />} />
              <Route path="sandbox" element={<SandboxOperationsPage />} />
              <Route
                path="operations/sandbox"
                element={<SandboxOperationsPage />}
              />
              <Route path="operations" element={<OperationsHubPage />} />
              <Route
                path="operations/qualifications"
                element={<QualificationCenterPage />}
              />
              <Route path="operations/alerts" element={<AlertCenterPage />} />
              <Route
                path="operations/alerts/:id"
                element={<AlertDetailPage />}
              />
              <Route path="operations/reports" element={<ReportCenterPage />} />
              <Route
                path="operations/reports/:id"
                element={<ReportDetailPage />}
              />
              <Route
                path="operations/configuration"
                element={<ConfigurationCenterPage />}
              />
              <Route
                path="operations/orders"
                element={
                  <ResourceCollectionPage
                    resource="orders"
                    title="Orders"
                    eyebrow="Durable order lifecycle"
                    description="Inspect virtual, testnet, and demo order state without exposing private exchange payloads."
                  />
                }
              />
              <Route
                path="operations/fills"
                element={
                  <ResourceCollectionPage
                    resource="fills"
                    title="Fills"
                    eyebrow="Reconciled fill evidence"
                    description="Review quantity, price, fee, order linkage, accounting state, and correlation evidence."
                  />
                }
              />
              <Route path="incidents" element={<IncidentCenterPage />} />
              <Route path="incidents/:id" element={<IncidentWorkspacePage />} />
              <Route path="audit" element={<AuditPage />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

function AuthenticatedShell() {
  const session = useQuery(sessionQuery);
  if (session.isLoading)
    return (
      <main>
        <StatePanel state="loading" />
      </main>
    );
  if (session.isError || !session.data) return <Navigate to="/login" replace />;
  return (
    <AppShell user={session.data.user}>
      <Outlet />
    </AppShell>
  );
}
