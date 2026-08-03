import { expect, test, type Page, type Route } from "@playwright/test";
import axe from "axe-core";

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
    "sandbox.read",
    "sandbox.arm",
    "sandbox.cancel",
    "sandbox.admin",
  ],
};
function pageEnvelope<T>(items: T[]) {
  return {
    items,
    revision: "12",
    has_more: false,
  };
}
function snapshotEnvelope<T>(items: T[]) {
  return { ...pageEnvelope(items), snapshot_revision: "12" };
}
function qualityFixture() {
  return {
    tier: "local_tier_b",
    confidence: "high",
    freshness: "fresh",
    source: "immutable_simulation_evidence",
    observed_at: now,
    provenance_complete: true,
  };
}
function exchangeFixture(id: string, name: string) {
  return {
    id,
    name,
    environment: "production_public",
    public_only: true,
    websocket_state: "healthy",
    book_state: "healthy",
    recorder_state: "healthy",
    capabilities: ["public_metadata", "public_order_book"],
    instruments: 2,
    quality: qualityFixture(),
    revision: "2",
  };
}
function opportunityFixture() {
  return {
    id: "decision-b8",
    kind: "cross_exchange",
    label: "buy_binance_sell_bybit",
    buy_exchange: "binance",
    sell_exchange: "bybit",
    instrument: "BTCUSDT",
    gross_metric: "0.01",
    net_metric: "0.006",
    expected_profit: "0.60",
    worst_case_profit: "0.20",
    maximum_size: "100",
    tested_size: "50",
    status: "simulated",
    simulation_only: true,
    strategy_version: "cross-v1",
    quality: qualityFixture(),
    recorded_at: now,
    revision: "3",
  };
}

function d2ResourceFixture(
  id: string,
  kind: string,
  state: string,
  attributes: Record<string, unknown>,
) {
  return {
    id,
    kind,
    state,
    revision: "1",
    correlation_id: `correlation-${id}`,
    occurred_at: now,
    attributes,
    links: {},
  };
}

function d2ActivityFixture(view: "decisions_orders" | "system_events") {
  return {
    id: "activity-d2",
    activity_revision: "7",
    view,
    source_type: view === "system_events" ? "alerts" : "decisions",
    source_id: view === "system_events" ? "alert-d2" : "decision-d2",
    source_revision: "1",
    outcome: view === "system_events" ? "recorded" : "rejected",
    strategy_id: view === "system_events" ? undefined : "cross-v1",
    instrument_id: view === "system_events" ? undefined : "BTCUSDT",
    exchange_id: "binance",
    mode: "shadow",
    reason: {
      code: view === "system_events" ? "public_feed_gap" : "risk.entry_blocked",
      summary:
        view === "system_events" ? "Public feed gap detected" : "Entry blocked",
      explanation:
        view === "system_events"
          ? "The public market-data sequence was incomplete and rebuilding began."
          : "Central risk policy rejected this candidate before virtual execution.",
      suggested_action:
        view === "system_events"
          ? "Wait for a healthy rebuilt book before resuming affected decisions."
          : "Review the scoped risk state and blocking prerequisites.",
      severity: "warning",
      unknown: false,
      version: "1",
    },
    correlation_id: "correlation-d2",
    occurred_at: now,
    details: { risk_evaluation_id: "risk-d2" },
    links: { self: "/api/v1/activity/activity-d2" },
  };
}

test.beforeEach(async ({ page }) => {
  const state: FixtureState = {
    replayState: "RUNNING",
    replayRevision: 1,
  };
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
  await page.route("**/api/v1/**", (route) => routeAPI(route, state));
});

test("D3 labs preserve immutable identity and virtual execution", async ({
  page,
  isMobile,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();
  await expect(page.getByText("REAL TRADING DISABLED")).toBeVisible();
  await expect(
    page.getByLabel("Persistent safety status").getByText("SHADOW · VIRTUAL"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Binance" }).click();
  await expect(
    page.getByRole("heading", { name: "Binance Connection" }),
  ).toBeVisible();
  await expect(page.getByText("Production-public only")).toBeVisible();

  await page.getByRole("link", { name: "Backtest Lab" }).click();
  await fillRun(page);
  await page.getByRole("button", { name: "Launch backtest" }).click();
  await expect(page.getByText("SUCCEEDED", { exact: true })).toBeVisible();
  await expect(page.getByText("locally reproducible")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Run progress and identity" }),
  ).toBeVisible();
  await expect(page.getByText("dataset-a11", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Registered benchmarks" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Registered stress scenarios" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Registered capacity curve" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Replay Lab" }).click();
  await fillRun(page);
  await page.getByRole("button", { name: "Create replay" }).click();
  for (const [action, expectedState] of [
    ["pause", "PAUSED"],
    ["step", "PAUSED"],
    ["resume", "RUNNING"],
  ] as const) {
    const trigger = page.getByRole("button", { name: action, exact: true });
    await expect(trigger).toBeEnabled();
    await trigger.click();
    await expect(page.getByRole("alertdialog")).toBeVisible();
    await page
      .getByRole("button", { name: action, exact: true })
      .last()
      .click();
    await expect(page.getByRole("alertdialog")).toBeHidden();
    await expect(
      page.getByRole("main").getByText(expectedState, { exact: true }),
    ).toBeVisible();
    await page.goto("/replays/replay-a11");
    await expect(
      page.getByRole("main").getByText(expectedState, { exact: true }),
    ).toBeVisible();
  }
  await expect(
    page.getByRole("heading", { name: "Exact event and decision inspection" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Durable replay checkpoints" }),
  ).toBeVisible();
  await page.getByText("Canonical decision", { exact: true }).click();
  await expect(
    page.getByText('{"reason_code":"entry_accepted"}', { exact: true }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Shadow Center" }).click();
  await page.getByLabel("Configuration ID").fill("configuration-a10");
  await page.getByLabel("Portfolio ID").fill("portfolio-a11");
  await page.getByRole("button", { name: "Start virtual shadow" }).click();
  await expect(
    page.getByText(/Public-live · virtual execution/i),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Simulated orders and fills" }),
  ).toBeVisible({ timeout: 15_000 });
  await expect(
    page.getByRole("table", { name: "Recent decisions and risk actions" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Owned virtual inventory" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Sealed-ledger P&L attribution" }),
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
  await page
    .getByRole("button", { name: "Show authorized evidence hashes" })
    .dispatchEvent("click");
  await expect(page.getByText(/event_hash.*[a-f0-9]{64}/)).toBeVisible();
  const incidentReplay = page.getByRole("link", {
    name: "Prepare incident replay",
  });
  await expect(incidentReplay).toBeVisible();
  const incidentReplayHref = await incidentReplay.getAttribute("href");
  expect(incidentReplayHref).toContain("dataset=dataset-a11");
  await page.goto(incidentReplayHref!);
  await expect(page.getByRole("heading", { name: "Replay Lab" })).toBeVisible();
  await expect(page.getByLabel("Approved dataset manifest ID")).toHaveValue(
    "dataset-a11",
  );
  await page.getByLabel("Configuration revision ID").fill("configuration-a10");
  await page.getByLabel("Research generation ID").fill("generation-a10-1");
  await page.getByLabel("Root seed SHA-256").fill("8".repeat(64));
  await page.getByRole("button", { name: "Create replay" }).click();
  await expect(page.getByText("single_run_incomplete")).toBeVisible();

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

  await page.goto("/shadow/shadow-a11");
  expect(await seriousAxeViolations(page)).toEqual([]);
  await page.getByRole("button", { name: "Stop shadow session" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Stop session" }).click();

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(
    page.getByRole("heading", { name: "Owner access" }),
  ).toBeVisible();
});

test("B8 multi-exchange workflows remain simulation-only and keyboard reachable", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  await page.getByRole("link", { name: "Exchanges", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Exchange Operations" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Bybit" })).toBeVisible();

  await page.getByRole("link", { name: "Opportunities" }).click();
  await page
    .getByRole("button", { name: /cross exchange.*decision-b8/i })
    .click();
  await expect(
    page.getByRole("heading", { name: "Leg evidence" }),
  ).toBeVisible();
  await expect(page.getByText("Simulation outcome recorded")).toBeVisible();

  await page.getByRole("link", { name: "Strategy Center" }).click();
  await expect(
    page.getByRole("heading", { name: "Cross venue" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Cross venue challenger/i }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Inventory" }).click();
  await expect(page.getByText("Combined balance:")).toBeVisible();
  await expect(
    page.getByText("DISABLED", { exact: true }).first(),
  ).toBeVisible();
  await expect(page.getByText(/never netted across exchanges/i)).toBeVisible();

  await page.getByRole("link", { name: "Rebalancing" }).click();
  await expect(page.getByText(/no transfer controls/i)).toBeVisible();
  await page
    .getByRole("button", { name: "Review route and checklist" })
    .click();
  await expect(
    page.getByText("Verify destination deposit availability"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Reports" }).click();
  await page.getByRole("button", { name: "Create JSON export" }).click();
  await expect(
    page.getByRole("heading", { name: "Immutable export" }),
  ).toBeVisible();
  await expect(page.getByText(/application\/json/)).toBeVisible();

  await expect(page.getByText("REAL TRADING DISABLED")).toBeVisible();
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
    "BODY",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("C6 sandbox workflows remain test/demo-only, responsive, and recoverable", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  await page.getByRole("link", { name: "Sandbox Operations" }).click();
  await expect(
    page.getByRole("heading", { name: "Sandbox Operations" }),
  ).toBeVisible();
  const boundary = page.getByRole("region", { name: "Execution boundary" });
  await expect(boundary.getByText("BINANCE SPOT TESTNET")).toBeVisible();
  await expect(boundary.getByText("BYBIT DEMO")).toBeVisible();
  await expect(boundary.getByText("REAL TRADING DISABLED")).toBeVisible();
  await expect(page.getByText("UNKNOWN", { exact: true })).toBeVisible();
  await expect(page.getByText("NOT QUALIFIED")).toBeVisible();
  await expect(
    page.getByText(/smoke pass is never a 72-hour pass/i),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Cancel order-c6" }),
  ).toBeEnabled();
  await expect(
    page.getByRole("button", { name: "Query order-c6" }),
  ).toBeEnabled();

  const orderButton = page.getByRole("button", {
    name: "Request capped test order",
  });
  await expect(orderButton).toBeDisabled();
  await orderButton.evaluate((button) => button.removeAttribute("disabled"));
  await orderButton.click();
  await expect(
    page.getByText("active_arm_confirmation_required"),
  ).toBeVisible();
  await expect(page.getByText(/production environment/i)).toHaveCount(0);

  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
    "BODY",
  );
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("D2 command center is understandable, role-aware, and evidence-linked", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  for (const group of [
    "Home",
    "Activity",
    "Strategies",
    "Run Lab",
    "Risk & Controls",
    "Operations",
  ]) {
    await expect(
      page.getByRole("heading", { name: group, exact: true }),
    ).toBeVisible();
  }
  await expect(page.getByText("REAL TRADING DISABLED")).toBeVisible();
  await expect(
    page.getByLabel("Persistent safety status").getByText("SHADOW · VIRTUAL"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Decisions & Orders" }).click();
  await expect(
    page.getByRole("heading", { name: "Decisions & Orders" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Entry blocked" }).click();
  await expect(
    page.getByRole("heading", { name: "Recommended action" }),
  ).toBeVisible();
  await expect(
    page.getByText("Review the scoped risk state and blocking prerequisites."),
  ).toBeVisible();
  const downloadPromise = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Download JSON", exact: true })
    .click();
  expect((await downloadPromise).suggestedFilename()).toContain(
    "decisions-orders-redacted.json",
  );

  await page
    .getByLabel("Activity views")
    .getByRole("link", { name: "System Events" })
    .click();
  await expect(
    page.getByRole("heading", { name: "System Events" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Public feed gap detected" }).click();
  await expect(page.getByText(/sanitized connectivity/i)).toBeVisible();

  await page.getByRole("link", { name: "Strategy Center" }).click();
  await expect(
    page.getByRole("heading", { name: "Strategy Center" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Cross venue" }),
  ).toBeVisible();
  await expect(
    page.getByLabel("Strategy controls").getByText("configuration disabled"),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Resume strategy" }),
  ).toBeDisabled();

  await page.getByRole("link", { name: "Qualifications" }).click();
  await expect(
    page.getByRole("heading", { name: "Qualification Center" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "C6 sandbox order and reconciliation" }),
  ).toBeVisible();
  await expect(
    page.getByText(/smoke pass cannot become a formal pass/i),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Start fail-closed preflight" }),
  ).toBeDisabled();

  await page.getByRole("link", { name: "Approved runs" }).click();
  await expect(
    page.getByRole("main").getByRole("heading", { name: "Run Lab", level: 1 }),
  ).toBeVisible();
  await expect(page.getByText(/cannot run arbitrary commands/i)).toBeVisible();
  await expect(
    page.getByRole("textbox", { name: /command|test name/i }),
  ).toHaveCount(0);

  await page.addScriptTag({ content: axe.source });
  const violations = await page.evaluate(async () => {
    const engine = (
      window as unknown as {
        axe: {
          run: (
            root: Document,
            options: unknown,
          ) => Promise<{
            violations: Array<{ id: string; impact: string | null }>;
          }>;
        };
      }
    ).axe;
    const result = await engine.run(document, {
      runOnly: {
        type: "tag",
        values: ["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"],
      },
    });
    return result.violations.filter((violation) =>
      ["critical", "serious"].includes(violation.impact ?? ""),
    );
  });
  expect(violations).toEqual([]);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

async function fillRun(page: Page) {
  await page.getByLabel("Configuration revision ID").fill("configuration-a10");
  await page.getByLabel("Approved dataset manifest ID").fill("dataset-a11");
  await page.getByLabel("Research generation ID").fill("generation-a10-1");
  await page.getByLabel("Root seed SHA-256").fill("8".repeat(64));
}

async function seriousAxeViolations(page: Page) {
  await page.addScriptTag({ content: axe.source });
  return page.evaluate(async () => {
    const engine = (
      window as unknown as {
        axe: {
          run: (
            root: Document,
            options: unknown,
          ) => Promise<{
            violations: Array<{ id: string; impact: string | null }>;
          }>;
        };
      }
    ).axe;
    const result = await engine.run(document, {
      runOnly: {
        type: "tag",
        values: ["wcag2a", "wcag2aa", "wcag21aa", "wcag22aa"],
      },
    });
    return result.violations.filter((violation) =>
      ["critical", "serious"].includes(violation.impact ?? ""),
    );
  });
}

interface FixtureState {
  replayState: "RUNNING" | "PAUSED";
  replayRevision: number;
}

async function routeAPI(route: Route, state: FixtureState) {
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
  else if (path === "/api/v1/sandbox/overview") body = sandboxOverviewFixture();
  else if (path === "/api/v1/sandbox/orders")
    body = pageEnvelope([sandboxOrderFixture()]);
  else if (path === "/api/v1/sandbox/reconciliations")
    body = {
      ...pageEnvelope([sandboxReconciliationFixture()]),
      reset_incidents: [],
    };
  else if (path === "/api/v1/sandbox/qualification")
    body = sandboxQualificationFixture();
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
  else if (path === "/api/v1/exchanges")
    body = snapshotEnvelope([
      exchangeFixture("binance", "Binance"),
      exchangeFixture("bybit", "Bybit"),
    ]);
  else if (path === "/api/v1/opportunities")
    body = snapshotEnvelope([opportunityFixture()]);
  else if (path === "/api/v1/activity") {
    const view =
      url.searchParams.get("view") === "system_events"
        ? "system_events"
        : "decisions_orders";
    body = snapshotEnvelope([d2ActivityFixture(view)]);
  } else if (path === "/api/v1/activity/activity-d2")
    body = d2ActivityFixture("decisions_orders");
  else if (path === "/api/v1/opportunities/decision-b8")
    body = {
      summary: opportunityFixture(),
      legs: [
        {
          index: 0,
          exchange: "binance",
          instrument: "BTCUSDT",
          side: "buy",
          input_quantity: "50",
          trade_quantity: "0.001",
          gross_output: "0.001",
          net_output: "0.00099",
          fee_asset: "BTC",
          fee_quantity: "0.00001",
          fee_quote_equivalent: "0.50",
          vwap: "50000",
          depth_cost: "0.05",
          state: "FILLED",
          revision: "4",
        },
      ],
      inventory: [],
      recovery: {
        attempted: false,
        succeeded: false,
        quarantined: false,
        disposition: "both_filled",
        explanation: "Closed-cycle simulation completed.",
        recovery_loss: "0",
      },
      cost_attribution: { buy_fee: "0.05", latency: "0.02" },
      timeline: [
        {
          index: 0,
          event_type: "cross_exchange.candidate",
          label: "Immutable candidate recorded",
          occurred_at: now,
          correlation_id: "decision-b8",
          revision: "3",
        },
        {
          index: 1,
          event_type: "cross_exchange.simulation",
          label: "Simulation outcome recorded",
          occurred_at: now,
          correlation_id: "decision-b8",
          revision: "4",
        },
      ],
      raw_evidence_available: true,
    };
  else if (path === "/api/v1/strategies")
    body = snapshotEnvelope([
      {
        id: "cross-v1",
        family: "cross_exchange",
        name: "Cross venue",
        version: "1",
        supported_modes: ["backtest", "replay", "shadow"],
        maturity: "EXPERIMENTAL",
        evidence_role: "challenger",
        confidence: "local_tier_b",
        viability: "viable_for_more_research",
        disclaimer: "No production profitability claim.",
        created_at: now,
        revision: "3",
      },
    ]);
  else if (path === "/api/v1/strategies/cross-v1")
    body = d2ResourceFixture("cross-v1", "strategy", "blocked", {
      name: "Cross venue",
      family: "cross_exchange",
      latest_version: "1",
      configured_state: "disabled",
      runtime_state: "blocked",
      blocking_prerequisites: ["configuration_disabled"],
      real_trading_enabled: false,
    });
  else if (path === "/api/v1/strategies/cross-v1/versions")
    body = snapshotEnvelope([
      d2ResourceFixture("cross-v1-v1", "strategy_version", "registered", {
        strategy_id: "cross-v1",
        implementation_hash: "a".repeat(64),
      }),
    ]);
  else if (path === "/api/v1/assets")
    body = snapshotEnvelope([
      d2ResourceFixture("BTC", "asset", "approved", {
        symbol: "BTC",
        spot_only: true,
      }),
    ]);
  else if (path === "/api/v1/risk/controls")
    body = snapshotEnvelope([
      d2ResourceFixture("global:all", "risk_control", "normal", {
        scope: "global",
        scope_id: "all",
        reason_code: "manual_normal",
      }),
    ]);
  else if (path === "/api/v1/alerts")
    body = snapshotEnvelope([
      d2ResourceFixture("alert-d2", "alert", "open", {
        alert_type: "public_feed_gap",
      }),
    ]);
  else if (method === "GET" && path === "/api/v1/reports")
    body = snapshotEnvelope([]);
  else if (method === "GET" && path === "/api/v1/configuration-revisions")
    body = snapshotEnvelope([
      d2ResourceFixture(
        "configuration-a10",
        "configuration_revision",
        "active",
        {
          configuration_hash: "b".repeat(64),
          actor: "owner-a11",
        },
      ),
    ]);
  else if (method === "GET" && path === "/api/v1/qualifications")
    body = snapshotEnvelope([
      d2ResourceFixture("c6-sandbox", "qualification", "AVAILABLE", {
        name: "C6 sandbox order and reconciliation",
        kind: "sandbox",
        duration_seconds: 259200,
        owner_start_required: true,
        latest_run_id: null,
      }),
    ]);
  else if (method === "GET" && path === "/api/v1/lab-runs")
    body = snapshotEnvelope([
      d2ResourceFixture("backtest-a11", "lab_run", "SUCCEEDED", {
        job_type: "backtest",
        run_id: "backtest-a11",
      }),
      d2ResourceFixture("replay-a11", "lab_run", state.replayState, {
        job_type: "replay",
        run_id: "replay-a11",
      }),
    ]);
  else if (method === "GET" && path === "/api/v1/users")
    body = snapshotEnvelope([
      d2ResourceFixture("owner-a11", "user", "active", {
        email: "owner@example.test",
        roles: ["owner"],
      }),
    ]);
  else if (path === "/api/v1/orders") body = snapshotEnvelope([]);
  else if (path === "/api/v1/fills") body = snapshotEnvelope([]);
  else if (path === "/api/v1/inventory")
    body = {
      ...snapshotEnvelope([
        {
          id: "decision-b8:buy_venue",
          exchange: "binance",
          asset: "BTC",
          strategy_version: "cross-v1",
          experiment_id: "run-b8",
          portfolio_id: "portfolio-b8-binance",
          before: "1",
          after: "0.999",
          available: "0.999",
          reserved: "0",
          status: "normal",
          virtual: true,
          quality: qualityFixture(),
          updated_at: now,
          revision: "1",
        },
      ]),
      combined_balance: false,
      isolation_notice:
        "Virtual inventory is isolated by exchange, strategy, experiment, and portfolio.",
    };
  else if (path === "/api/v1/rebalancing/recommendations")
    body = {
      ...snapshotEnvelope([
        {
          id: "b6-aaaaaaaaaaaaaaaaaaaaaaaa",
          method: "reviewed_graph_route",
          source_exchange: "binance",
          source_asset: "BTC",
          destination_exchange: "bybit",
          destination_asset: "BTC",
          quantity: "0.1",
          total_cost: "0.2",
          minimum_duration_nanos: "1000000",
          maximum_duration_nanos: "2000000",
          risk_score: "0.2",
          warnings: ["operator review required"],
          advisory_only: true,
          quality: qualityFixture(),
          recorded_at: now,
          revision: "5",
        },
      ]),
      execution_available: false,
    };
  else if (
    path === "/api/v1/rebalancing/recommendations/b6-aaaaaaaaaaaaaaaaaaaaaaaa"
  )
    body = {
      summary: {
        id: "b6-aaaaaaaaaaaaaaaaaaaaaaaa",
        advisory_only: true,
      },
      route: [
        {
          index: 0,
          role: "transfer",
          fact_id: "fact-b8",
          fact_version: "1",
          from_exchange: "binance",
          from_asset: "BTC",
          to_exchange: "bybit",
          to_asset: "BTC",
          confidence: "0.9",
          expected_cost: "0.2",
          minimum_duration_nanos: "1000000",
          maximum_duration_nanos: "2000000",
          warnings: [],
          approved: true,
          provenance_hash: "a".repeat(64),
        },
      ],
      checklist: [
        {
          index: 0,
          instruction: "Verify destination deposit availability",
          manual_only: true,
        },
      ],
      execution_available: false,
    };
  else if (path === "/api/v1/research/champion-challenger")
    body = snapshotEnvelope([
      {
        id: "comparison-b8",
        champion_strategy_version: "trend-v1a-1",
        challenger_strategy_version: "cross-v1",
        champion_suite_id: "suite-1",
        challenger_suite_id: "suite-2",
        confidence: "local_tier_b",
        viability: "viable_for_more_research",
        disposition: "retain_champion",
        disclaimer: "No production profitability claim.",
        manifest_hash: "b".repeat(64),
        created_at: now,
        revision: "6",
      },
    ]);
  else if (
    method === "POST" &&
    path === "/api/v1/reports/comparison-b8/exports"
  )
    body = {
      id: "b8-export-aaaaaaaaaaaaaaaaaaaaaaaa",
      report_id: "comparison-b8",
      format: "json",
      content_type: "application/json",
      content: '{"simulation_only":true}\n',
      payload_hash: "c".repeat(64),
      revision: "1",
      simulation_only: true,
      created_at: now,
    };
  else if (method === "POST" && path === "/api/v1/authorizations")
    body = {
      token: "authorization-" + "a".repeat(32),
      purpose: "qualification_start",
      target_revision: "1",
      expires_at: now,
    };
  else if (method === "POST" && path === "/api/v1/exports")
    body = {
      id: "export-d2",
      command_id: "command-d2",
      job_id: "job-d2",
      resource_type: "activity",
      resource_id: "activity-d2",
      format: "json",
      content_type: "application/json",
      content: '{"real_trading_enabled":false}\n',
      content_hash: "c".repeat(64),
      size_bytes: "31",
      redaction_version: "v1d.redaction.v1",
      created_at: now,
      expires_at: "2026-07-23T12:00:00Z",
      held: false,
      deleted: false,
      revision: "1",
    };
  else if (
    method === "POST" &&
    (/^\/api\/v1\/strategies\/[^/]+\/(configuration|runtime)$/.test(path) ||
      /^\/api\/v1\/risk\/controls\//.test(path) ||
      /^\/api\/v1\/alerts\/[^/]+\/acknowledge$/.test(path) ||
      path === "/api/v1/reports" ||
      path === "/api/v1/configuration-revisions" ||
      path === "/api/v1/qualifications" ||
      /^\/api\/v1\/lab-runs\/[^/]+\/(pause|resume|cancel|reproduce)$/.test(
        path,
      ) ||
      /^\/api\/v1\/qualifications\/[^/]+\/abort$/.test(path) ||
      /^\/api\/v1\/users\/[^/]+\/roles$/.test(path))
  )
    body = command("d2-target");
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
  ) {
    if (path === "/api/v1/replays") {
      state.replayState = "RUNNING";
      state.replayRevision += 1;
    }
    body = job(path.includes("backtests") ? "backtest" : "replay", state);
  } else if (method === "GET" && /^\/api\/v1\/(backtests|replays)\//.test(path))
    body = job(path.includes("backtests") ? "backtest" : "replay", state);
  else if (method === "POST" && /^\/api\/v1\/replays\/[^/]+\//.test(path)) {
    if (path.endsWith("/pause")) state.replayState = "PAUSED";
    if (path.endsWith("/resume")) state.replayState = "RUNNING";
    state.replayRevision += 1;
    body = command("replay-a11");
  } else if (method === "POST" && path === "/api/v1/shadow-sessions")
    body = shadow();
  else if (method === "GET" && path === "/api/v1/shadow-sessions")
    body = pageEnvelope([
      {
        id: "shadow-a11",
        state: "RUNNING",
        revision: "3",
        configuration_id: "configuration-a10",
        strategy_version: "trend.v1a.1",
        public_only: true,
        simulation_only: true,
        created_at: now,
      },
    ]);
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
          redacted: url.searchParams.get("include_raw") !== "true",
          ...(url.searchParams.get("include_raw") === "true"
            ? { safe_detail: `{"event_hash":"${"d".repeat(64)}"}` }
            : {}),
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

function sandboxAccountFixture() {
  return {
    id: "binance-c6",
    exchange: "binance",
    environment: "spot_testnet",
    state: "ARMED",
    engine_ready: true,
    account_epoch: 3,
    credential_generation: 2,
    revision: "4",
    session_id: "sandbox-session-c6",
    session_revision: "5",
    startup_cycle: 7,
    private_stream_healthy: true,
    reconciliation_clean: true,
    evidence_healthy: true,
    lease_held: true,
    observed_at: now,
    stale: false,
    active_arm: sandboxArmFixture(),
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
    audit_url: "/api/v1/audit-events?event_type=sandbox_account",
  };
}

function sandboxArmFixture() {
  return {
    id: "arm-c6",
    session_id: "sandbox-session-c6",
    account_ids: ["binance-c6"],
    state: "active",
    created_at: now,
    expires_at: "2099-07-30T12:15:00Z",
    revision: "1",
    audit_url: "/api/v1/audit-events?event_type=sandbox_arm",
  };
}

function sandboxOrderFixture() {
  return {
    id: "order-c6",
    account_id: "binance-c6",
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
    unknown_since: now,
    created_at: now,
    updated_at: now,
    revision: "6",
    fills: [],
    audit_url: "/api/v1/audit-events?event_type=v1c_order",
  };
}

function sandboxReconciliationFixture() {
  return {
    id: "reconciliation-c6",
    account_id: "binance-c6",
    exchange: "binance",
    account_epoch: 3,
    state: "clean",
    reconciled_at: now,
    differences: [],
    suspense_count: 0,
    quarantine_count: 0,
    audit_url: "/api/v1/audit-events?event_type=reconciliation",
  };
}

function sandboxQualificationFixture() {
  return {
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
      last_observed_at: now,
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
    audit_url: "/api/v1/audit-events?event_type=c6_qualification",
  };
}

function sandboxOverviewFixture() {
  return {
    environment_label: "BINANCE SPOT TESTNET + BYBIT DEMO / VIRTUAL",
    real_trading_enabled: false,
    observed_at: now,
    stale: false,
    accounts: [sandboxAccountFixture()],
    active_arms: [sandboxArmFixture()],
    orders: [sandboxOrderFixture()],
    reconciliations: [sandboxReconciliationFixture()],
    reset_incidents: [],
    risk_state: "PAUSED",
    qualification: sandboxQualificationFixture(),
    audit_url: "/api/v1/audit-events?event_type=c6",
  };
}

function job(kind: "backtest" | "replay", state: FixtureState) {
  return {
    id: `${kind}-a11`,
    kind,
    state: kind === "backtest" ? "SUCCEEDED" : state.replayState,
    mode_label: kind.toUpperCase(),
    revision: kind === "backtest" ? "4" : state.replayRevision.toString(10),
    progress: "1",
    created_at: now,
    updated_at: now,
    input_manifest: {
      configuration_id: "configuration-a10",
      dataset_id: "dataset-a11",
      research_generation_id: "generation-a10-1",
      strategy_version: "trend.v1a.1",
      root_seed_hash: "8".repeat(64),
      ...(kind === "replay" ? { speed: "maximum" } : {}),
    },
    lifecycle: {
      pause: kind === "replay" && state.replayState === "RUNNING",
      resume: kind === "replay" && state.replayState === "PAUSED",
      cancel: kind === "replay",
      reproduce: kind === "backtest",
      compare: true,
      export: true,
    },
    reproduction_bundle: {
      run_id: `${kind}-a11`,
      input_hash: "1".repeat(64),
      manifest_hash: "2".repeat(64),
      result_hash: "a".repeat(64),
      code_commit: "3".repeat(40),
      go_version: "go1.26.5",
      architecture: "amd64",
      operating_system: "linux",
      dataset_manifest_hash: "4".repeat(64),
      dataset_revision: "1",
      source_commit: "5".repeat(40),
      configuration_hash: "6".repeat(64),
      model_namespace_id: "production-public-v1a",
      starting_balance_hash: "7".repeat(64),
      confidence_tier: "B",
      canonical_manifest: JSON.stringify({ run_id: `${kind}-a11` }),
    },
    result: {
      result_hash: "a".repeat(64),
      platform_correctness: "locally reproducible",
      strategy_evidence: "Tier B local only",
      viability: "undetermined",
      reproducibility: "byte-identical",
      report_id: `report-${kind}-a11`,
      report_hash: "b".repeat(64),
      confidence_label: "insufficient",
      research_coverage: "single_run_incomplete",
      disclaimer:
        "Backtest, replay, paper, and shadow results are research evidence only and are not evidence or a guarantee of production profitability.",
      metrics: { net_return: "0.01" },
    },
    registered_report: {
      id: "registered-report-a11",
      research_generation_id: "generation-a10-1",
      manifest_hash: "e".repeat(64),
      confidence_label: "local_tier_b",
      platform_correctness: "deterministic registered suite validated",
      strategy_evidence: "registered local suite",
      viability: "viable_for_more_research",
      disclaimer:
        "Backtest, replay, paper, and shadow results are research evidence only and are not evidence or a guarantee of production profitability.",
      run_references: ["run-suite-1", "run-suite-2"],
      benchmarks: [
        {
          name: "cash",
          net_return: "0",
          max_drawdown: "0",
          trades: 0,
        },
        {
          name: "buy_and_hold",
          net_return: "0.02",
          max_drawdown: "0.03",
          trades: 1,
        },
      ],
      stress: [
        {
          name: "fee",
          net_return: "0.005",
          max_drawdown: "0.025",
          trades: 12,
        },
      ],
      capacity: [
        { notional: "1000", net_return: "0.01", fill_rate: "1" },
        { notional: "10000", net_return: "0.006", fill_rate: "0.8" },
      ],
      canonical_manifest: JSON.stringify({
        research_generation_id: "generation-a10-1",
        evidence: "registered suite",
      }),
      created_at: now,
    },
    ...(kind === "replay"
      ? {
          replay_inspection: {
            event_count: "20",
            ordinal: "20",
            event_hash: "c".repeat(64),
            canonical_event:
              '{"ordinal":20,"decision":{"reason_code":"entry_accepted"},"orders":[],"execution_events":[],"balances":{"USDT":"1000"}}',
            canonical_decision: '{"reason_code":"entry_accepted"}',
            canonical_orders: "[]",
            canonical_execution_events: "[]",
            canonical_balances: '{"USDT":"1000"}',
          },
          checkpoints: [
            {
              revision: "2",
              input_ordinal: "20",
              state_hash: "9".repeat(64),
              deterministic_state_hash: "8".repeat(64),
              model_namespace_id: "production-public-v1a",
              created_at: now,
            },
          ],
        }
      : {}),
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
    configuration_id: "configuration-a10",
    strategy_version: "trend.v1a.1",
    decision_dataset_id: "dataset-a11",
    model_namespace_id: "production-public-v1a",
    accepted_decisions: 1,
    rejected_decisions: 1,
    journal_transactions: 2,
    risk_state: "RESUMED",
    created_at: now,
    started_at: now,
    portfolio_id: "portfolio-a11",
    run_id: "shadow-a11",
    exchange_id: "binance",
    slippage_model_id: "slippage-v1",
    gap_model_id: "gap-v1",
    data_health: {
      exchange: "binance",
      state: "HEALTHY",
      reason: "public stream synchronized",
      observed_at: now,
      fresh: true,
    },
    pnl_attribution: {
      realized_pnl: "0.12",
      fee_expense: "-0.01",
      spread: "0.02",
      slippage: "-0.01",
      latency: "-0.005",
      valuation_basis: "sealed_ledger_functional_value",
    },
    decisions: [
      {
        id: "decision-shadow-a11",
        outcome: "accepted",
        reason_code: "entry_accepted",
        risk_outcome: "approved",
        risk_reason_code: "within_limits",
        occurred_at: now,
      },
    ],
    balances: [
      {
        asset: "USDT",
        available: "940",
        reserved: "0",
        revision: "2",
        updated_at: now,
      },
    ],
    positions: [
      {
        instrument: "BTCUSDT",
        quantity: "0.001",
        weighted_average_cost: "60000",
        realized_pnl: "0.12",
        revision: "2",
        updated_at: now,
      },
    ],
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
