import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import axe from "axe-core";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SandboxOperationsPage } from "./SandboxOperationsPage";

const at = "2026-07-30T12:00:00Z";
const audit = "/api/v1/audit-events?event_type=sandbox_qualification";
const arm = {
  id: "arm-sandbox_qualification",
  session_id: "session-sandbox_qualification",
  account_ids: ["binance-sandbox_qualification"],
  state: "active",
  created_at: at,
  expires_at: "2099-07-30T12:15:00Z",
  revision: "1",
  audit_url: audit,
};
const account = {
  id: "binance-sandbox_qualification",
  exchange: "binance",
  environment: "spot_testnet",
  state: "ARMED",
  engine_ready: true,
  account_epoch: 3,
  credential_generation: 2,
  revision: "4",
  session_id: "session-sandbox_qualification",
  session_revision: "5",
  startup_cycle: 7,
  private_stream_healthy: true,
  reconciliation_clean: true,
  evidence_healthy: true,
  lease_held: true,
  observed_at: at,
  stale: false,
  active_arm: arm,
  cap_usage: {
    utc_day: "2026-07-30",
    per_order_limit: "10",
    daily_limit: "50",
    daily_reserved: "5",
    daily_remaining: "45",
    account_open: 1,
    account_open_limit: 1,
    global_open: 1,
    global_open_limit: 2,
  },
  audit_url: audit,
};
const order = {
  id: "order-sandbox_qualification",
  account_id: account.id,
  exchange: "binance",
  environment: "spot_testnet",
  state: "UNKNOWN",
  action: "ENTRY",
  instrument: "BTCUSDT",
  side: "buy",
  quantity: "0.0001",
  limit_price: "50000",
  notional: "5",
  style: "LIMIT_GTC",
  attempt: 1,
  recovery_status: "required",
  unknown_since: at,
  created_at: at,
  updated_at: at,
  revision: "6",
  fills: [],
  audit_url: audit,
};
const reconciliation = {
  id: "reconciliation-sandbox_qualification",
  account_id: account.id,
  exchange: "binance",
  account_epoch: 3,
  state: "clean",
  reconciled_at: at,
  differences: [],
  suspense_count: 0,
  quarantine_count: 0,
  audit_url: audit,
};
const qualification = {
  state: "SMOKE_PASSED",
  mode: "smoke",
  required_duration_seconds: 2,
  observed_duration_seconds: 2,
  profitability_evidence: false,
  qualified: false,
  failures: [],
  recovery_incidents: [
    {
      account_id: "bybit-qualification",
      exchange: "bybit",
      environment: "demo",
      state: "recovered",
      incident_source: "private_stream",
      reason_category: "transient_outage",
      cause_code: "private_stream_receive_failed",
      deadline_at: "2026-07-30T12:02:00Z",
      clean_check_count: 2,
      detected_at: at,
      recovery_timestamp: "2026-07-30T12:00:31Z",
      evidence_hash: "a".repeat(64),
    },
  ],
  chaos: {
    status: "passed",
    passed: 14,
    failed: 0,
    last_observed_at: at,
  },
  slo: {
    samples: 3,
    critical_alert_latency_ms: 100,
    recovery_duration_ms: 200,
    duplicate_creates: 0,
    lost_fills: 0,
    double_posted_fills: 0,
    unknown_orders: 0,
    reconciliation_mismatches: 0,
    suspense_items: 0,
    reconnects: 1,
    restarts: 0,
    resident_memory_delta_bytes: 1024,
    positive_memory_leak_trend: false,
    passing: true,
  },
  formal_soak_pending: true,
  audit_url: audit,
};
const strategySession = {
  id: "strategy-session-sandbox_qualification",
  display_name: "Trend Following · Binance Spot Testnet · BTCUSDT",
  strategy_name: "Trend Following",
  exchanges: ["binance"],
  instrument: "BTCUSDT",
  state: "prepared",
  waiting_reason:
    "This session is prepared. Complete owner reauthentication and arm its selected account before the strategy can begin evaluating.",
  created_at: at,
  revision: "1",
  audit_url: audit,
};

afterEach(() => vi.unstubAllGlobals());

describe("sandbox qualification sandbox console", () => {
  it("shows authoritative boundaries and accessible recovery controls", async () => {
    vi.stubGlobal("fetch", vi.fn(sandboxQualificationFetchFixture));
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    const view = render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <SandboxOperationsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(
      await screen.findByRole("heading", { name: "Sandbox Operations" }),
    ).toBeInTheDocument();
    expect(screen.getByText("BINANCE SPOT TESTNET")).toBeInTheDocument();
    expect(screen.getByText("BYBIT DEMO")).toBeInTheDocument();
    expect(
      screen.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
    ).toBeInTheDocument();
    expect(screen.getByText("UNKNOWN", { exact: true })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Strategy sessions" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /complete owner reauthentication and arm its selected account/i,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("list", { name: "Strategy session steps" }),
    ).toHaveTextContent(
      /Use the account controls above to arm every selected/i,
    );
    expect(
      await screen.findByRole("heading", { name: "Virtual/test actions only" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: "Cancel order-sandbox_qualification",
      }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Query order-sandbox_qualification" }),
    ).toBeEnabled();
    expect(
      screen.queryByText(/production environment/i),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/never a production order/i)).toBeInTheDocument();
    expect(
      screen.getByText(/bybit-qualification.*private_stream/i),
    ).toBeInTheDocument();
    const result = await axe.run(view.container);
    expect(result.violations).toHaveLength(0);
  });

  it("requires owner reauthentication before starting a prepared strategy session", async () => {
    vi.stubGlobal("fetch", vi.fn(sandboxQualificationFetchFixture));
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <SandboxOperationsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Start after reauthentication",
      }),
    );
    expect(screen.getByLabelText("Owner password")).toHaveAttribute(
      "type",
      "password",
    );
    expect(screen.getByLabelText("One-time code")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Reauthenticate and start" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /Starting evaluates the strategy; it does not create an order by itself/i,
      ),
    ).toBeInTheDocument();
  });
});

function sandboxQualificationFetchFixture(input: RequestInfo | URL) {
  const path = String(input);
  let body: unknown;
  if (path.includes("/session/me")) {
    body = {
      user: {
        id: "owner-sandbox_qualification",
        email: "owner@example.test",
      },
      session_id: "browser-sandbox_qualification",
      session_revision: "1",
      reauthenticated_at: at,
    };
  } else if (path.includes("/sandbox/overview")) {
    body = {
      environment_label: "BINANCE SPOT TESTNET + BYBIT DEMO / VIRTUAL",
      real_trading_enabled: false,
      observed_at: at,
      stale: false,
      accounts: [account],
      active_arms: [arm],
      strategy_sessions: [strategySession],
      orders: [order],
      reconciliations: [reconciliation],
      reset_incidents: [],
      risk_state: "PAUSED",
      qualification,
      audit_url: audit,
    };
  } else if (path.includes("/sandbox/orders")) {
    body = { items: [order], revision: "6", has_more: false };
  } else if (path.includes("/sandbox/reconciliations")) {
    body = {
      items: [reconciliation],
      reset_incidents: [],
      revision: "7",
      has_more: false,
    };
  } else {
    body = qualification;
  }
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
}
