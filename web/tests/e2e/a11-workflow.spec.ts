import { expect, test, type Page, type Route } from "@playwright/test";

const now = "2026-07-16T12:00:00Z";
const user = {
  id: "owner-a11",
  email: "owner@example.test",
  roles: ["owner"],
  permissions: [
    "operations.read",
    "commands.write",
    "incident.raw",
    "audit.raw",
  ],
};
function pageEnvelope<T>(items: T[]) {
  return {
    items,
    revision: "12",
    has_more: false,
  };
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    class DeterministicEventSource extends EventTarget {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSED = 2;
      CONNECTING = 0;
      OPEN = 1;
      CLOSED = 2;
      readyState = 0;
      withCredentials = false;
      onopen: ((event: Event) => void) | null = null;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onerror: ((event: Event) => void) | null = null;
      constructor(readonly url: string | URL) {
        super();
        setTimeout(() => {
          this.readyState = 1;
          this.onopen?.(new Event("open"));
        }, 20);
        (
          window as unknown as { axiomStream?: DeterministicEventSource }
        ).axiomStream = this;
      }
      close() {
        this.readyState = 2;
      }
    }
    Object.defineProperty(window, "EventSource", {
      value: DeterministicEventSource,
    });
  });
  await page.route("**/api/v1/**", routeAPI);
});

test("authenticated research workflow remains virtual and recovers state", async ({
  page,
  isMobile,
}) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();
  await expect(page.getByText("REAL TRADING DISABLED")).toBeVisible();
  await expect(
    page.getByRole("status").getByText("SHADOW · VIRTUAL"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Binance" }).click();
  await expect(
    page.getByRole("heading", { name: "Binance Connection" }),
  ).toBeVisible();
  await expect(page.getByText("Production-public only")).toBeVisible();

  await page.getByRole("link", { name: "Backtest Lab" }).click();
  await fillRun(page);
  await page.getByRole("button", { name: "Launch backtest" }).click();
  await expect(page.getByText("SUCCEEDED")).toBeVisible();
  await expect(page.getByText("locally reproducible")).toBeVisible();

  await page.getByRole("link", { name: "Replay Lab" }).click();
  await fillRun(page);
  await page.getByRole("button", { name: "Create replay" }).click();
  for (const action of ["pause", "step", "resume"] as const) {
    await page.getByRole("button", { name: action, exact: true }).click();
    await expect(page.getByRole("alertdialog")).toBeVisible();
    await page
      .getByRole("button", { name: action, exact: true })
      .last()
      .click();
  }

  await page.getByRole("link", { name: "Shadow Center" }).click();
  await page.getByLabel("Configuration ID").fill("configuration-a10");
  await page.getByLabel("Portfolio ID").fill("portfolio-a11");
  await page.getByRole("button", { name: "Start virtual shadow" }).click();
  await expect(
    page.getByText(/Public-live · virtual execution/i),
  ).toBeVisible();
  await expect(page.getByText("yes").first()).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Simulated orders and fills" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Trend" }).click();
  await expect(page.getByText("local_tier_b")).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Decision and rejection evidence" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Portfolio" }).click();
  await expect(
    page.getByRole("table", { name: "Virtual balances" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Immutable journal lines" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Incidents" }).click();
  await page
    .getByRole("link", { name: "Open latest incident evidence" })
    .click();
  await expect(page.getByText("dataset-a11")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Prepare incident replay" }),
  ).toBeVisible();

  await page.evaluate(() =>
    (
      window as unknown as {
        axiomStream: { onerror: ((event: Event) => void) | null };
      }
    ).axiomStream.onerror?.(new Event("error")),
  );
  await expect(page.getByText("reconnecting")).toBeVisible();

  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  if (!isMobile) {
    await page.keyboard.press("Tab");
    expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
      "BODY",
    );
  }

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(
    page.getByRole("heading", { name: "Owner access" }),
  ).toBeVisible();
});

async function fillRun(page: Page) {
  await page.getByLabel("Configuration ID").fill("configuration-a10");
  await page.getByLabel("Dataset ID").fill("dataset-a11");
  await page.getByLabel("Root seed hash").fill("8".repeat(64));
}

async function routeAPI(route: Route) {
  const request = route.request();
  const url = new URL(request.url());
  const path = url.pathname;
  const method = request.method();
  let body: unknown;
  if (method === "POST" && path === "/api/v1/session/login")
    body = { user, csrf_token: "csrf-" + "c".repeat(40), expires_at: now };
  else if (method === "POST" && path === "/api/v1/session/logout")
    return route.fulfill({ status: 204 });
  else if (path === "/api/v1/session/me")
    body = {
      user,
      session_id: "session-a11",
      session_revision: "1",
      reauthenticated_at: now,
    };
  else if (path === "/api/v1/system/status")
    body = {
      release: "V1A",
      phase: "A11",
      role: "api",
      lifecycle_state: "RUNNING",
      strategy_activation: "trend.v1a.1",
      real_trading_enabled: false,
      environment: "production_public",
      execution_mode: "shadow",
      engine_state: "RUNNING",
      binance_state: "healthy",
      risk_state: "RESUMED",
      active_resource_id: "shadow-a11",
      critical_incidents: 1,
      server_time: now,
      revision: "12",
    };
  else if (path === "/api/v1/exchanges/binance/health")
    body = {
      environment: "production_public",
      public_only: true,
      websocket_state: "healthy",
      book_state: "healthy",
      recorder_state: "healthy",
      observed_at: now,
      revision: "12",
      capabilities: ["public_metadata", "public_order_book"],
    };
  else if (path === "/api/v1/exchanges/binance/instruments")
    body = pageEnvelope([
      {
        id: "instrument-a11",
        symbol: "BTCUSDT",
        product: "spot",
        price_tick: "0.01",
        quantity_step: "0.00001",
        minimum_quantity: "0.00001",
        minimum_notional: "10",
        metadata_version: "1",
      },
    ]);
  else if (path === "/api/v1/portfolios")
    body = pageEnvelope([
      {
        id: "portfolio-a11",
        label: "VIRTUAL",
        mode: "shadow",
        equity: "1000",
        available: "900",
        reserved: "100",
        revision: "4",
      },
    ]);
  else if (path === "/api/v1/portfolios/portfolio-a11")
    body = {
      id: "portfolio-a11",
      label: "VIRTUAL",
      mode: "shadow",
      equity: "1000",
      available: "900",
      reserved: "100",
      balances: [{ asset: "USDT", available: "900", reserved: "100" }],
      positions: [],
      revision: "4",
      updated_at: now,
    };
  else if (path.endsWith("/journal"))
    body = {
      ...pageEnvelope([
        {
          id: "journal-a11:1",
          transaction_id: "journal-a11",
          asset: "USDT",
          direction: "debit",
          quantity: "10",
          occurred_at: now,
          correlation_id: "correlation-a11",
        },
      ]),
      virtual: true,
    };
  else if (path === "/api/v1/risk/status")
    body = {
      state: "NORMAL",
      policy_version: "1",
      recovery_ready: false,
      contributors: [],
      revision: "2",
      updated_at: now,
      unresolved_critical: 0,
    };
  else if (path === "/api/v1/strategies/trend")
    body = {
      version: "trend.v1a.1",
      timeframe: "4h",
      health: "healthy",
      evidence_maturity: "local_tier_b",
      viability: "undetermined",
      parameters: Array.from({ length: 16 }, (_, index) => ({
        id: `parameter-${index}`,
        value: "1",
        unit: "count",
        cadence: "4h",
        mutability: "immutable_per_run",
      })),
      revision: "1",
    };
  else if (path.endsWith("/decisions"))
    body = pageEnvelope([
      {
        id: "decision-a11",
        outcome: "accepted",
        reason_code: "entry_accepted",
        explanation: "Strict completed-candle breakout",
        candle_view_id: "candle-a11",
        market_view_id: "market-a11",
        occurred_at: now,
        revision: "1",
      },
    ]);
  else if (
    method === "POST" &&
    (path === "/api/v1/backtests" || path === "/api/v1/replays")
  )
    body = job(path.includes("backtests") ? "backtest" : "replay");
  else if (method === "GET" && /^\/api\/v1\/(backtests|replays)\//.test(path))
    body = job(path.includes("backtests") ? "backtest" : "replay");
  else if (method === "POST" && /^\/api\/v1\/replays\/[^/]+\//.test(path))
    body = command("replay-a11");
  else if (method === "POST" && path === "/api/v1/shadow-sessions")
    body = shadow();
  else if (method === "GET" && path.startsWith("/api/v1/shadow-sessions/"))
    body = shadow();
  else if (method === "POST" && path.endsWith("/stop"))
    body = command("shadow-a11");
  else if (path === "/api/v1/incidents")
    body = pageEnvelope([
      {
        id: "incident-a11",
        severity: "critical",
        state: "resolved",
        reason_code: "public_feed_gap",
        opened_at: now,
        revision: "1",
      },
    ]);
  else if (path === "/api/v1/incidents/incident-a11")
    body = {
      id: "incident-a11",
      severity: "critical",
      state: "resolved",
      reason_code: "public_feed_gap",
      opened_at: now,
      revision: "1",
      timeline: [
        {
          id: "event-a11",
          event_type: "gap",
          occurred_at: now,
          correlation_id: "correlation-a11",
          redacted: false,
        },
      ],
      replay_window: {
        dataset_id: "dataset-a11",
        first_ordinal: "1",
        last_ordinal: "20",
      },
    };
  else if (path === "/api/v1/audit-events") body = pageEnvelope([]);
  else
    return route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({
        code: "not_found",
        message: "not found",
        correlation_id: "test",
      }),
    });
  const status =
    path === "/api/v1/session/login" ? 201 : method === "POST" ? 202 : 200;
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

function job(kind: "backtest" | "replay") {
  return {
    id: `${kind}-a11`,
    kind,
    state: "SUCCEEDED",
    mode_label: kind.toUpperCase(),
    revision: "4",
    progress: "1",
    created_at: now,
    updated_at: now,
    result: {
      result_hash: "a".repeat(64),
      platform_correctness: "locally reproducible",
      strategy_evidence: "Tier B local only",
      viability: "undetermined",
      reproducibility: "byte-identical",
      metrics: { net_return: "0.01" },
    },
  };
}
function shadow() {
  return {
    id: "shadow-a11",
    state: "RUNNING",
    label: "PUBLIC-LIVE SHADOW / VIRTUAL",
    public_only: true,
    simulation_only: true,
    entries_enabled: true,
    revision: "3",
    risk_state: "RESUMED",
    created_at: now,
    started_at: now,
    orders: [
      {
        id: "order-a11",
        instrument: "BTCUSDT",
        side: "buy",
        state: "filled",
        quantity: "0.001",
        limit_price: "60000",
        filled_quantity: "0.001",
        latency_ms: "25",
        simulated: true,
      },
    ],
  };
}
function command(target: string) {
  return {
    id: "command-a11",
    state: "applied",
    target_id: target,
    revision: "2",
    correlation_id: "correlation-a11",
    created_at: now,
  };
}
