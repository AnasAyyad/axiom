import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { lazy, Suspense } from "react";
import {
  BrowserRouter,
  Navigate,
  Outlet,
  Route,
  Routes,
} from "react-router-dom";

import { sessionQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { AppShell } from "./AppShell";
import { LoginPage } from "./LoginPage";
import {
  BinancePage,
  CommandCenter,
  AuditPage,
  IncidentPage,
  IncidentDetailPage,
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
  StrategyCenter,
} from "./MultiExchangePages";

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

/** App composes the authenticated A11 console and authoritative server-state cache. */
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
              <Route path="exchanges" element={<ExchangesPage />} />
              <Route path="exchanges/binance" element={<BinancePage />} />
              <Route path="opportunities" element={<OpportunityScanner />} />
              <Route path="portfolios" element={<PortfolioPage />} />
              <Route path="portfolios/:id" element={<PortfolioPage />} />
              <Route path="risk" element={<RiskPage />} />
              <Route path="strategies/trend" element={<TrendPage />} />
              <Route path="strategies" element={<StrategyCenter />} />
              <Route
                path="inventory"
                element={<MultiExchangeInventoryPage />}
              />
              <Route path="rebalancing" element={<RebalancingCenter />} />
              <Route path="research/reports" element={<ResearchReports />} />
              <Route path="backtests" element={<BacktestLab />} />
              <Route path="backtests/:id" element={<BacktestLab />} />
              <Route path="replays" element={<ReplayLab />} />
              <Route path="replays/:id" element={<ReplayLab />} />
              <Route path="shadow" element={<ShadowCenter />} />
              <Route path="shadow/:id" element={<ShadowCenter />} />
              <Route path="sandbox" element={<SandboxOperationsPage />} />
              <Route path="incidents" element={<IncidentPage />} />
              <Route path="incidents/:id" element={<IncidentDetailPage />} />
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
