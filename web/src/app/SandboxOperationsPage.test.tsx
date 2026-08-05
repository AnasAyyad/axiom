import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import axe from "axe-core";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SandboxOperationsPage } from "./SandboxOperationsPage";

const at = "2026-07-30T12:00:00Z";
const audit = "/api/v1/audit-events?event_type=c6";
const arm = {
  id: "arm-c6",
  session_id: "session-c6",
  account_ids: ["binance-c6"],
  state: "active",
  created_at: at,
  expires_at: "2099-07-30T12:15:00Z",
  revision: "1",
  audit_url: audit,
};
const account = {
  id: "binance-c6",
  exchange: "binance",
  environment: "spot_testnet",
  state: "ARMED",
  engine_ready: true,
  account_epoch: 3,
  credential_generation: 2,
  revision: "4",
  session_id: "session-c6",
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
  id: "order-c6",
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
  id: "reconciliation-c6",
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

afterEach(() => vi.unstubAllGlobals());

describe("C6 sandbox console", () => {
  it("shows authoritative boundaries and accessible recovery controls", async () => {
    vi.stubGlobal("fetch", vi.fn(c6FetchFixture));
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
    expect(screen.getByText("REAL TRADING DISABLED")).toBeInTheDocument();
    expect(screen.getByText("UNKNOWN", { exact: true })).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: "Virtual/test actions only" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Cancel order-c6" }),
    ).toBeEnabled();
    expect(
      screen.getByRole("button", { name: "Query order-c6" }),
    ).toBeEnabled();
    expect(
      screen.queryByText(/production environment/i),
    ).not.toBeInTheDocument();
    expect(screen.getByText(/never a production order/i)).toBeInTheDocument();
    const result = await axe.run(view.container);
    expect(result.violations).toHaveLength(0);
  });
});

function c6FetchFixture(input: RequestInfo | URL) {
  const path = String(input);
  let body: unknown;
  if (path.includes("/session/me")) {
    body = {
      user: {
        id: "owner-c6",
        email: "owner@example.test",
      },
      session_id: "browser-c6",
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
