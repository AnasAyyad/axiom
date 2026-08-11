import { expect, test, type Page, type Route } from "@playwright/test";
import axe from "axe-core";

const now = "2026-07-16T12:00:00Z";
const user = {
  id: "owner-owner_console",
  email: "owner@example.test",
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
    id: "decision-multi_exchange_console",
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

function runChoiceFixture() {
  return {
    strategy_id: "triangular-arbitrage",
    strategy_name: "Triangular Arbitrage",
    strategy_version: "triangular-arbitrage@1.0.0",
    mode: "backtest",
    exchanges: ["binance"],
    instrument: "BTC/USDT",
    cadence: "For every synchronized recorded book generation",
    warmup: "Three qualified synchronized spot books",
    order_capable: true,
  };
}

function sandboxRunChoiceFixture() {
  return {
    strategy_id: "cross-exchange-arbitrage",
    strategy_name: "Cross-Exchange Arbitrage",
    strategy_version: "cross-exchange-arbitrage@1.0.0",
    mode: "sandbox",
    exchanges: ["binance", "bybit"],
    instrument: "BTC/USDT",
    cadence: "When coherent Binance and Bybit books are available",
    warmup: "Paired synchronized spot books and inventory evidence",
    order_capable: true,
  };
}

function runResourceFixture() {
  return {
    id: "run-triangular-owner",
    friendly_name: "Triangular Arbitrage backtest",
    strategy_id: "triangular-arbitrage",
    strategy_version: "triangular-arbitrage@1.0.0",
    exchanges: ["binance"],
    instrument: "BTC/USDT",
    mode: "backtest",
    environment: "recorded_data",
    state: "accepted",
    order_capable: true,
    available_actions: [],
    revision: "1",
    created_at: now,
  };
}

function evaluationCampaignFixture(completed = false) {
  const feeds = ["binance", "bybit"].flatMap((exchange) =>
    ["BTCUSDT", "ETHUSDT", "ETHBTC"].map((instrument) => ({
      exchange,
      instrument,
      eligible: true,
      book_fresh: true,
      clock_eligible: true,
      latest_event_at: now,
      message_count: 864_000,
      queue_drop_count: 0,
      gap_count: 0,
      decoder_error_count: 0,
    })),
  );
  return {
    id: "evaluation-campaign-owner_console",
    preset: "balanced_full_v1",
    state: completed ? "COMPLETED" : "RUNNING",
    current_stage: completed ? "FINAL_REPORT" : "COMBINED_SHADOW",
    completed_stages: completed
      ? [
          "HISTORICAL_IMPORT",
          "EXISTING_DATA_AUDIT",
          "RECORDER_ROTATION",
          "RECORDER_QUALIFICATION",
          "BACKTEST_MATRIX",
          "REPLAY_MATRIX",
          "CANDIDATE_SELECTION",
          "COMBINED_SHADOW",
          "FINAL_REPORT",
        ]
      : [
          "HISTORICAL_IMPORT",
          "EXISTING_DATA_AUDIT",
          "RECORDER_ROTATION",
          "RECORDER_QUALIFICATION",
          "BACKTEST_MATRIX",
          "REPLAY_MATRIX",
          "CANDIDATE_SELECTION",
        ],
    valid_recording_seconds: 259_200,
    valid_shadow_seconds: 86_400,
    wall_time_seconds: 432_000,
    estimated_remaining_seconds: 518_400,
    recorded_bytes: 32_212_254_720,
    recording_limit_bytes: 214_748_364_800,
    measured_bytes_per_hour: 447_392_426,
    shadow_reserved_bytes: 75_161_927_568,
    recording_last_valid_at: now,
    shadow_last_valid_at: now,
    stages: [
      {
        stage: "HISTORICAL_IMPORT",
        state: "COMPLETED",
        attempt: 1,
        started_at: "2026-07-11T12:00:00Z",
        completed_at: "2026-07-11T13:00:00Z",
        updated_at: "2026-07-11T13:00:00Z",
      },
      {
        stage: "COMBINED_SHADOW",
        state: completed ? "COMPLETED" : "RUNNING",
        attempt: 1,
        started_at: "2026-07-15T12:00:00Z",
        ...(completed ? { completed_at: now } : {}),
        updated_at: now,
      },
      {
        stage: "FINAL_REPORT",
        state: completed ? "COMPLETED" : "PENDING",
        attempt: completed ? 1 : 0,
        ...(completed ? { started_at: now, completed_at: now } : {}),
        updated_at: now,
      },
    ],
    historical_imports: [
      {
        exchange: "binance",
        instrument: "BTC/USDT",
        interval: "15m",
        state: "COMPLETED",
        window_start: "2023-08-01T00:00:00Z",
        window_end: "2026-08-01T00:00:00Z",
        checkpoint_time: "2026-08-01T00:00:00Z",
        row_count: 105_216,
        byte_count: 52_428_800,
        gap_count: 0,
      },
    ],
    coverage: [
      {
        dataset_id: "dataset-owner_console",
        exchange: "binance",
        instrument: "BTCUSDT",
        eligibility: "eligible",
        reason_code: "AUDIT_PASSED",
        segment_count: 864,
        byte_count: 31_138_512_896,
        gap_count: 0,
        duplicate_count: 0,
      },
    ],
    matrix: [
      {
        id: "evaluation-member-trend-backtest",
        strategy: "trend-following",
        configuration: "trend-balanced-1",
        mode: "backtest",
        capital_micros: 2_000_000_000,
        repeat_ordinal: 0,
        cost_stress_bps: 10_000,
        state: "SUCCEEDED",
        verdict: "CONTINUE",
        result_hash: "a".repeat(64),
        metrics: {
          selection_metrics: {
            net_result_micros: 42_000_000,
            trade_count: 128,
            maximum_drawdown_bps: 95,
          },
        },
      },
    ],
    feed_health: feeds,
    shadow: {
      state: completed ? "COMPLETED" : "RUNNING",
      valid_seconds: completed ? 604_800 : 86_400,
      start_ordinal: 1,
      last_processed_ordinal: 86_400,
      shared_capital_micros: 10_000_000_000,
      protected_reserve_micros: 2_000_000_000,
      member_ceiling_micros: 2_000_000_000,
      members: [
        {
          id: "evaluation-member-trend-shadow",
          strategy: "trend-following",
          configuration: "trend-balanced-1",
          mode: "shadow",
          capital_micros: 2_000_000_000,
          repeat_ordinal: 0,
          cost_stress_bps: 10_000,
          state: "RUNNING",
          verdict: "CONTINUE",
          metrics: {
            net_result_micros: 8_000_000,
            trade_count: 21,
            maximum_drawdown_bps: 24,
            opportunities: 80,
            accepted_decisions: 31,
            simulated_orders: 31,
            filled_orders: 21,
          },
        },
      ],
    },
    revision: "8",
    created_at: "2026-07-11T12:00:00Z",
    updated_at: now,
  };
}

function ownerExperienceResourceFixture(
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

function ownerExperienceActivityFixture(
  view: "decisions_orders" | "system_events",
) {
  return {
    id: "activity-owner_experience",
    activity_revision: "7",
    view,
    source_type: view === "system_events" ? "alerts" : "decisions",
    source_id:
      view === "system_events"
        ? "alert-owner_experience"
        : "decision-owner_experience",
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
    correlation_id: "correlation-owner_experience",
    occurred_at: now,
    details: { risk_evaluation_id: "risk-owner_experience" },
    links: { self: "/api/v1/activity/activity-owner_experience" },
  };
}

test.beforeEach(async ({ page }) => {
  const state: FixtureState = {
    replayState: "RUNNING",
    replayRevision: 1,
    evaluationCreated: false,
  };
  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, "onLine", {
      configurable: true,
      get: () => true,
    });
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

test("Unified runs preserve immutable identity and virtual execution", async ({
  page,
  isMobile,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();
  await expect(
    page.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
  ).toBeVisible();
  await expect(
    page.getByLabel("Persistent safety status").getByText("SHADOW · VIRTUAL"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Exchanges", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Exchange Operations" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Binance" })).toBeVisible();

  await page.getByRole("link", { name: "New Run" }).click();
  await expect(page.getByRole("heading", { name: "New Run" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: /Exchange sandbox/i }),
  ).toBeEnabled();
  await page.getByRole("button", { name: /Historical test/i }).click();
  await page.getByRole("button", { name: /Triangular Arbitrage/i }).click();
  await page.getByRole("button", { name: /BTC\/USDT on binance/i }).click();
  await expect(
    page.getByText(/never need to copy a dataset, configuration, portfolio/i),
  ).toBeVisible();
  await page.getByRole("button", { name: "Start reviewed run" }).click();
  await expect(
    page.getByRole("heading", { name: "Triangular Arbitrage backtest" }),
  ).toBeVisible();
  for (const tab of [
    "Overview",
    "Timeline",
    "Decisions",
    "Orders & Fills",
    "Portfolio & P&L",
    "Risk",
    "Data & Models",
    "Evidence",
  ]) {
    await expect(page.getByRole("tab", { name: tab })).toBeVisible();
  }
  await page.getByRole("tab", { name: "Data & Models" }).click();
  await expect(page.getByText("recorded data")).toBeVisible();

  await page.getByRole("link", { name: "Live Shadow" }).click();
  await expect(
    page.getByRole("heading", { name: "Shadow Trading Center" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Choose a reviewed run" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Strategies" }).click();
  await expect(
    page.getByRole("heading", { name: "Strategy Center" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Portfolio" }).click();
  await expect(
    page.getByRole("table", { name: "Virtual balances" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: "Immutable journal lines" }),
  ).toBeVisible();

  await page.goto("/incidents");
  await page.getByRole("link", { name: "Open incident workspace" }).click();
  await expect(
    page.getByText("dataset-owner_console", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Show authorized evidence hashes" })
    .dispatchEvent("click");
  await expect(
    page
      .getByRole("table", { name: "Immutable incident timeline" })
      .getByRole("cell", { name: "d".repeat(64), exact: true }),
  ).toBeVisible();
  const incidentReplay = page.getByRole("link", {
    name: "Prepare incident replay",
  });
  await expect(incidentReplay).toBeVisible();
  const incidentReplayHref = await incidentReplay.getAttribute("href");
  expect(incidentReplayHref).toContain("dataset=dataset-owner_console");
  await page.goto(incidentReplayHref!);
  await expect(page.getByRole("heading", { name: "Replay Lab" })).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Choose a reviewed run" }),
  ).toBeVisible();
  await expect(
    page.getByLabel(/configuration|dataset|generation|seed/i),
  ).toHaveCount(0);

  await expect(
    page
      .getByLabel("Persistent safety status")
      .getByText("live", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    const stream = (
      window as unknown as {
        axiomStream?: { onerror: ((event: Event) => void) | null };
      }
    ).axiomStream;
    if (stream === undefined) throw new Error("deterministic_stream_missing");
    stream.onerror?.(new Event("error"));
  });
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

  await page.goto("/shadow/shadow-owner_console");
  expect(await seriousAxeViolations(page)).toEqual([]);
  await page.getByRole("button", { name: "Stop shadow session" }).click();
  await expect(page.getByRole("alertdialog")).toBeVisible();
  await page.getByRole("button", { name: "Stop session" }).click();

  await page.getByRole("button", { name: "Log out" }).click();
  await expect(
    page.getByRole("heading", { name: "Owner access" }),
  ).toBeVisible();
});

test("Multi-exchange workflows remain simulation-only and keyboard reachable", async ({
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
    .getByRole("button", {
      name: /cross exchange.*decision-multi_exchange_console/i,
    })
    .click();
  await expect(
    page.getByRole("heading", { name: "Leg evidence" }),
  ).toBeVisible();
  await expect(page.getByText("Simulation outcome recorded")).toBeVisible();

  await page.getByRole("link", { name: "Strategies" }).click();
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
  await expect(
    page.getByRole("heading", { name: "Report Center" }),
  ).toBeVisible();
  await expect(page.getByText(/do not prove profitability/i)).toBeVisible();

  await expect(
    page.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
  ).toBeVisible();
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

test("Exchange sandbox workflows remain test/demo-only, responsive, and recoverable", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  await page.getByRole("link", { name: "Exchange Sandbox" }).click();
  await expect(
    page.getByRole("heading", { name: "Sandbox Operations" }),
  ).toBeVisible();
  const boundary = page.getByRole("region", { name: "Execution boundary" });
  await expect(boundary.getByText("BINANCE SPOT TESTNET")).toBeVisible();
  await expect(boundary.getByText("BYBIT DEMO")).toBeVisible();
  await expect(
    boundary.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
  ).toBeVisible();
  await expect(page.getByText("UNKNOWN", { exact: true })).toBeVisible();
  await expect(page.getByText("NOT QUALIFIED")).toBeVisible();
  await expect(
    page.getByText(/smoke pass is never a 72-hour pass/i),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Strategy sessions" }),
  ).toBeVisible();
  await expect(page.getByText("Triangular Arbitrage on Binance")).toBeVisible();
  await expect(page.getByText("Running", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Cancel order-sandbox_qualification" }),
  ).toBeEnabled();
  await expect(
    page.getByRole("button", { name: "Query order-sandbox_qualification" }),
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

test("Owner command center is understandable and evidence-linked", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  for (const group of [
    "Overview",
    "Explore",
    "Run",
    "Monitor",
    "Portfolio & Risk",
    "System",
  ]) {
    await expect(
      page.getByRole("heading", { name: group, exact: true }),
    ).toBeVisible();
  }
  await expect(
    page.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
  ).toBeVisible();
  await expect(
    page.getByLabel("Persistent safety status").getByText("SHADOW · VIRTUAL"),
  ).toBeVisible();

  await page.getByRole("link", { name: "Decisions", exact: true }).click();
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

  await page.getByRole("link", { name: "Strategies" }).click();
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

  await page.getByRole("link", { name: "Readiness" }).click();
  await expect(
    page.getByRole("heading", { name: "Qualification Center" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Sandbox order and reconciliation" }),
  ).toBeVisible();
  await expect(
    page.getByText(/smoke pass cannot become a formal pass/i),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Start fail-closed preflight" }),
  ).toBeDisabled();

  await page.getByRole("link", { name: "Run History" }).click();
  await expect(
    page.getByRole("main").getByRole("heading", { name: "New Run", level: 1 }),
  ).toBeVisible();
  await expect(
    page.getByLabel(/configuration|dataset|generation|seed|command|test name/i),
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

test("Operational evidence workflows are responsive, redacted, and actionable", async ({
  page,
}) => {
  test.slow();
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  await page.getByRole("link", { name: "Reports" }).click();
  await expect(
    page.getByRole("heading", { name: "Report Center" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "UTC schedules" }),
  ).toBeVisible();
  await expect(page.getByText(/do not prove profitability/i)).toBeVisible();
  await page.getByRole("link", { name: "Open report evidence" }).click();
  await expect(
    page.getByRole("heading", { name: "Risk", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("operational", { exact: true }).first(),
  ).toBeVisible();
  const reportDownload = page.waitForEvent("download");
  await page
    .getByRole("button", { name: "Download JSON", exact: true })
    .click();
  expect((await reportDownload).suggestedFilename()).toContain(
    "axiom-report-report-operational_evidence",
  );

  await page.getByRole("link", { name: "Alerts & Incidents" }).click();
  await expect(
    page.getByRole("heading", { name: "Delivery routes" }),
  ).toBeVisible();
  await expect(page.getByText(/credentials.*never exposed/i)).toBeVisible();
  await page
    .getByRole("link", { name: "Open delivery and escalation evidence" })
    .click();
  await expect(
    page.getByRole("table", { name: "Immutable sanitized delivery attempts" }),
  ).toBeVisible();
  await expect(
    page.getByText("sink_unavailable", { exact: true }),
  ).toBeVisible();

  await page.goto("/incidents");
  await page.getByRole("link", { name: "Open incident workspace" }).click();
  await expect(
    page.getByRole("heading", { name: "Hash-linked timeline" }),
  ).toBeVisible();
  await expect(
    page.getByText("Verified the public feed recovery.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Evidence holds" }),
  ).toBeVisible();

  await page.getByRole("link", { name: "Audit" }).click();
  await expect(
    page.getByRole("heading", { name: "Audit chain integrity" }),
  ).toBeVisible();
  await expect(
    page.getByText(/authoritative events have a valid immutable chain/i),
  ).toBeVisible();
  await page.addScriptTag({ content: axe.source });
  expect(await seriousAxeViolations(page)).toEqual([]);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
});

test("Strategy evaluation starts one server-owned workflow and remains responsive", async ({
  page,
}) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill("owner@example.test");
  await page.getByLabel("Password").fill("qualification-password");
  await page.getByRole("button", { name: "Enter console" }).click();

  await page.getByRole("link", { name: "Strategy Evaluation" }).click();
  await expect(
    page.getByRole("heading", { name: "Strategy Evaluation" }),
  ).toBeVisible();
  await expect(
    page.getByText("Spot only · simulated orders only"),
  ).toBeVisible();
  await expect(
    page.getByText(/No evaluation campaign has been started/i),
  ).toBeVisible();

  const startRequest = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/api/v1/evaluation-campaigns",
  );
  await page.getByRole("button", { name: "Start Full Evaluation" }).click();
  expect((await startRequest).postDataJSON()).toEqual({
    preset: "balanced_full_v1",
  });

  await expect(page.getByText("Current campaign")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Automatic stages" }),
  ).toBeVisible();
  await expect(
    page.getByRole("table", { name: /One strategy failure/i }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Combined seven-valid-day shadow" }),
  ).toBeVisible();
  await expect(page.getByText("10,000 USDT")).toBeVisible();
  await expect(
    page.getByText(/Only healthy, recorded intervals count/),
  ).toBeVisible();
  await expect(
    page.getByText(/All eligible strategies completed/i),
  ).toBeVisible();
  const reportDownload = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download JSON" }).click();
  expect((await reportDownload).suggestedFilename()).toBe(
    "axiom-strategy-evaluation.json",
  );

  expect(await seriousAxeViolations(page)).toEqual([]);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth,
    ),
  ).toBe(true);
  await page.keyboard.press("Tab");
  expect(await page.evaluate(() => document.activeElement?.tagName)).not.toBe(
    "BODY",
  );
});

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
  evaluationCreated: boolean;
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
      session_id: "session-owner_console",
      session_revision: "1",
      reauthenticated_at: now,
    };
  else if (path === "/api/v1/run-catalog")
    body = { choices: [runChoiceFixture(), sandboxRunChoiceFixture()] };
  else if (path === "/api/v1/evaluation-campaigns" && method === "POST") {
    state.evaluationCreated = true;
    body = evaluationCampaignFixture();
  } else if (path === "/api/v1/evaluation-campaigns")
    body = {
      items: state.evaluationCreated ? [evaluationCampaignFixture(true)] : [],
    };
  else if (
    path ===
    "/api/v1/evaluation-campaigns/evaluation-campaign-owner_console/events"
  )
    body = {
      items: [
        {
          ordinal: "8",
          event_type: "stage_started",
          stage: "COMBINED_SHADOW",
          summary: "Combined simulation-only shadow started.",
          occurred_at: now,
        },
      ],
    };
  else if (
    path ===
    "/api/v1/evaluation-campaigns/evaluation-campaign-owner_console/report"
  )
    body = state.evaluationCreated
      ? {
          state: "final",
          verdict: "CONTINUE",
          summary:
            "All eligible strategies completed offline and combined shadow evaluation.",
          report_hash: "f".repeat(64),
          generated_at: now,
          content: {
            members: [
              {
                strategy: "trend-following",
                mode: "backtest",
                verdict: "CONTINUE",
                metrics: { net_result_micros: 42_000_000 },
              },
              {
                strategy: "trend-following",
                mode: "replay",
                verdict: "CONTINUE",
                metrics: { net_result_micros: 39_000_000 },
              },
              {
                strategy: "trend-following",
                mode: "shadow",
                verdict: "CONTINUE",
                metrics: { net_result_micros: 18_000_000 },
              },
            ],
          },
        }
      : { state: "not_ready", generated_at: now };
  else if (
    path === "/api/v1/evaluation-campaigns/evaluation-campaign-owner_console"
  )
    body = evaluationCampaignFixture(true);
  else if (path === "/api/v1/runs" && method === "POST")
    body = runResourceFixture();
  else if (path === "/api/v1/runs") body = { items: [runResourceFixture()] };
  else if (path === "/api/v1/runs/run-triangular-owner")
    body = runResourceFixture();
  else if (
    path === "/api/v1/runs/run-triangular-owner/timeline" ||
    path === "/api/v1/runs/run-triangular-owner/decisions" ||
    path === "/api/v1/runs/run-triangular-owner/orders" ||
    path === "/api/v1/runs/run-triangular-owner/fills"
  )
    body = { items: [] };
  else if (path === "/api/v1/runs/run-triangular-owner/portfolio")
    body = {
      state: "not_recorded",
      waiting_reason: "The worker has not recorded a portfolio snapshot yet.",
    };
  else if (path === "/api/v1/runs/run-triangular-owner/risk")
    body = {
      state: "not_recorded",
      summary: "No run-scoped risk decision has been recorded yet.",
    };
  else if (path === "/api/v1/runs/run-triangular-owner/evidence")
    body = { state: "not_recorded" };
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
      application_version: "test",
      build_commit: "test-commit",
      configuration_identity: "test-configuration",
      readiness_state: "ready",
      lifecycle_state: "RUNNING",
      strategy_activation: "trend-following@1.0.0",
      real_trading_enabled: false,
      environment: "production_public",
      execution_mode: "shadow",
      engine_state: "RUNNING",
      binance_state: "healthy",
      risk_state: "RESUMED",
      active_resource_id: "shadow-owner_console",
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
    body = snapshotEnvelope([ownerExperienceActivityFixture(view)]);
  } else if (path === "/api/v1/activity/activity-owner_experience")
    body = ownerExperienceActivityFixture("decisions_orders");
  else if (path === "/api/v1/opportunities/decision-multi_exchange_console")
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
          correlation_id: "decision-multi_exchange_console",
          revision: "3",
        },
        {
          index: 1,
          event_type: "cross_exchange.simulation",
          label: "Simulation outcome recorded",
          occurred_at: now,
          correlation_id: "decision-multi_exchange_console",
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
    body = ownerExperienceResourceFixture("cross-v1", "strategy", "blocked", {
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
      ownerExperienceResourceFixture(
        "cross-v1-v1",
        "strategy_version",
        "registered",
        {
          strategy_id: "cross-v1",
          implementation_hash: "a".repeat(64),
        },
      ),
    ]);
  else if (path === "/api/v1/assets")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture("BTC", "asset", "approved", {
        symbol: "BTC",
        spot_only: true,
      }),
    ]);
  else if (path === "/api/v1/risk/controls")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture("global:all", "risk_control", "normal", {
        scope: "global",
        scope_id: "all",
        reason_code: "manual_normal",
      }),
    ]);
  else if (path === "/api/v1/alerts")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture(
        "alert-owner_experience",
        "alert",
        "open",
        {
          alert_type: "public_feed_gap",
        },
      ),
    ]);
  else if (method === "GET" && path === "/api/v1/alerts/alert-owner_experience")
    body = {
      id: "alert-owner_experience",
      severity: "warning",
      reason_code: "alert_delivery",
      component: "public-feed",
      state: "open",
      occurrences: 2,
      revision: "2",
      correlation_id: "correlation-alert-owner_experience",
      created_at: now,
      last_seen_at: now,
      deliveries: [
        {
          id: "attempt-owner_experience",
          sink_name: "webhook",
          attempt: 1,
          state: "failed",
          reason_code: "sink_unavailable",
          started_at: now,
          completed_at: now,
          latency_ms: 425,
        },
      ],
      escalations: [
        {
          id: "escalation-owner_experience",
          actor_user_id: "owner-owner_console",
          reason: "Escalated after delivery review",
          revision: "2",
          escalated_at: now,
        },
      ],
    };
  else if (method === "GET" && path === "/api/v1/alert-routes")
    body = {
      items: [
        {
          id: "in-app",
          sink_name: "in_app",
          enabled: true,
          minimum_severity: "info",
          target_label: "Axiom in-app Alert Center",
          last_test_state: "delivered",
          last_tested_at: now,
          revision: "1",
        },
        {
          id: "webhook",
          sink_name: "webhook",
          enabled: true,
          minimum_severity: "warning",
          target_label: "Allowlisted HTTPS webhook",
          last_test_state: "failed",
          last_tested_at: now,
          revision: "1",
        },
      ],
      revision: "1",
    };
  else if (method === "GET" && path === "/api/v1/reports")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture(
        "job-report-operational_evidence",
        "report",
        "SUCCEEDED",
        {
          job_type: "report:risk",
          report_id: "report-operational_evidence",
          confidence_tier: "operational",
        },
      ),
    ]);
  else if (
    method === "GET" &&
    path === "/api/v1/reports/report-operational_evidence"
  )
    body = {
      id: "report-operational_evidence",
      job_id: "job-report-operational_evidence",
      report_type: "risk",
      state: "SUCCEEDED",
      provenance: {
        mode: "operational",
        confidence_tier: "operational",
        valuation_basis: "not applicable",
        model_provenance: {
          report_schema: "axiom.report.owner_console.operational_evidence",
        },
        maturity: "operational",
        source_identity: "a".repeat(64),
        source_revision: "12",
      },
      generated_at: now,
      content_hash: "b".repeat(64),
      created_at: now,
      revision: "3",
    };
  else if (method === "GET" && path === "/api/v1/report-schedules")
    body = pageEnvelope([
      {
        id: "schedule-operational_evidence",
        report_type: "platform_readiness",
        frequency: "daily",
        minute_utc: 0,
        hour_utc: 6,
        state: "active",
        next_run_at: "2026-07-17T06:00:00Z",
        last_run_at: now,
        revision: "2",
        created_at: now,
        updated_at: now,
      },
    ]);
  else if (method === "GET" && path === "/api/v1/configuration-revisions")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture(
        "configuration-research_registry",
        "configuration_revision",
        "active",
        {
          configuration_hash: "b".repeat(64),
          actor: "owner-owner_console",
        },
      ),
    ]);
  else if (method === "GET" && path === "/api/v1/qualifications")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture(
        "sandbox_qualification-sandbox",
        "qualification",
        "AVAILABLE",
        {
          name: "Sandbox order and reconciliation",
          kind: "sandbox",
          duration_seconds: 259200,
          owner_start_required: true,
          latest_run_id: null,
        },
      ),
    ]);
  else if (method === "GET" && path === "/api/v1/lab-runs")
    body = snapshotEnvelope([
      ownerExperienceResourceFixture(
        "backtest-owner_console",
        "lab_run",
        "SUCCEEDED",
        {
          job_type: "backtest",
          run_id: "backtest-owner_console",
        },
      ),
      ownerExperienceResourceFixture(
        "replay-owner_console",
        "lab_run",
        state.replayState,
        {
          job_type: "replay",
          run_id: "replay-owner_console",
        },
      ),
    ]);
  else if (path === "/api/v1/orders") body = snapshotEnvelope([]);
  else if (path === "/api/v1/fills") body = snapshotEnvelope([]);
  else if (path === "/api/v1/inventory")
    body = {
      ...snapshotEnvelope([
        {
          id: "decision-multi_exchange_console:buy_venue",
          exchange: "binance",
          asset: "BTC",
          strategy_version: "cross-v1",
          experiment_id: "run-multi_exchange_console",
          portfolio_id: "portfolio-multi_exchange_console-binance",
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
          id: "inventory_rebalancing-aaaaaaaaaaaaaaaaaaaaaaaa",
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
    path ===
    "/api/v1/rebalancing/recommendations/inventory_rebalancing-aaaaaaaaaaaaaaaaaaaaaaaa"
  )
    body = {
      summary: {
        id: "inventory_rebalancing-aaaaaaaaaaaaaaaaaaaaaaaa",
        advisory_only: true,
      },
      route: [
        {
          index: 0,
          role: "transfer",
          fact_id: "fact-multi_exchange_console",
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
        id: "comparison-multi_exchange_console",
        champion_strategy_version: "trend-following-1-0-0",
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
    path === "/api/v1/reports/comparison-multi_exchange_console/exports"
  )
    body = {
      id: "multi_exchange_console-export-aaaaaaaaaaaaaaaaaaaaaaaa",
      report_id: "comparison-multi_exchange_console",
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
      id: "export-owner_experience",
      command_id: "command-owner_experience",
      job_id: "job-owner_experience",
      resource_type: "activity",
      resource_id: "activity-owner_experience",
      format: "json",
      content_type: "application/json",
      content: '{"real_trading_enabled":false}\n',
      content_hash: "c".repeat(64),
      size_bytes: "31",
      redaction_version: "owner_console.redaction.v1",
      created_at: now,
      expires_at: "2026-07-23T12:00:00Z",
      held: false,
      deleted: false,
      revision: "1",
    };
  else if (
    method === "POST" &&
    /^\/api\/v1\/incidents\/[^/]+\/evidence-bundles$/.test(path)
  )
    body = {
      id: "export-incident-operational_evidence",
      command_id: "command-incident-operational_evidence",
      job_id: "job-incident-operational_evidence",
      resource_type: "incident",
      resource_id: "incident-owner_console",
      format: "json",
      content_type: "application/json",
      content: '{"real_trading_enabled":false,"timeline_head_hash":"sealed"}\n',
      content_hash: "d".repeat(64),
      size_bytes: "64",
      redaction_version: "owner_console.redaction.v1",
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
      /^\/api\/v1\/alerts\/[^/]+\/(acknowledge|escalate)$/.test(path) ||
      /^\/api\/v1\/alert-routes\/[^/]+\/test$/.test(path) ||
      path === "/api/v1/reports" ||
      path === "/api/v1/report-schedules" ||
      /^\/api\/v1\/report-schedules\/[^/]+\/transitions$/.test(path) ||
      path === "/api/v1/incidents" ||
      /^\/api\/v1\/incidents\/[^/]+\/(updates|transitions)$/.test(path) ||
      path === "/api/v1/configuration-revisions" ||
      path === "/api/v1/qualifications" ||
      /^\/api\/v1\/lab-runs\/[^/]+\/(pause|resume|cancel|reproduce)$/.test(
        path,
      ) ||
      /^\/api\/v1\/qualifications\/[^/]+\/abort$/.test(path))
  )
    body = command("owner_experience-target");
  else if (path === "/api/v1/exchanges/binance/instruments")
    body = pageEnvelope([
      {
        id: "instrument-owner_console",
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
        id: "portfolio-owner_console",
        label: "VIRTUAL",
        mode: "shadow",
        equity: "1000",
        available: "900",
        reserved: "100",
        revision: "4",
      },
    ]);
  else if (path === "/api/v1/portfolios/portfolio-owner_console")
    body = {
      id: "portfolio-owner_console",
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
          id: "journal-owner_console:1",
          transaction_id: "journal-owner_console",
          asset: "USDT",
          direction: "debit",
          quantity: "10",
          occurred_at: now,
          correlation_id: "correlation-owner_console",
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
      version: "trend-following@1.0.0",
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
        id: "decision-owner_console",
        outcome: "accepted",
        reason_code: "entry_accepted",
        explanation: "Strict completed-candle breakout",
        candle_view_id: "candle-owner_console",
        market_view_id: "market-owner_console",
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
    body = command("replay-owner_console");
  } else if (method === "POST" && path === "/api/v1/shadow-sessions")
    body = shadow();
  else if (method === "GET" && path === "/api/v1/shadow-sessions")
    body = pageEnvelope([
      {
        id: "shadow-owner_console",
        state: "RUNNING",
        revision: "3",
        configuration_id: "configuration-research_registry",
        strategy_version: "trend-following@1.0.0",
        public_only: true,
        simulation_only: true,
        created_at: now,
      },
    ]);
  else if (method === "GET" && path.startsWith("/api/v1/shadow-sessions/"))
    body = shadow();
  else if (method === "POST" && path.endsWith("/stop"))
    body = command("shadow-owner_console");
  else if (path === "/api/v1/incidents")
    body = pageEnvelope([
      {
        id: "incident-owner_console",
        severity: "critical",
        state: "resolved",
        reason_code: "public_feed_gap",
        owner_user_id: "owner-owner_console",
        opened_at: now,
        updated_at: now,
        resolved_at: now,
        revision: "4",
      },
    ]);
  else if (path === "/api/v1/incidents/incident-owner_console")
    body = {
      id: "incident-owner_console",
      severity: "critical",
      state: "resolved",
      reason_code: "public_feed_gap",
      owner_user_id: "owner-owner_console",
      opened_at: now,
      updated_at: now,
      resolved_at: now,
      revision: "4",
      timeline: [
        {
          id: "event-owner_console",
          event_type: "gap",
          occurred_at: now,
          correlation_id: "correlation-owner_console",
          actor: "operator-owner_console",
          reason: "investigate public feed gap",
          event_hash: "d".repeat(64),
          redacted: url.searchParams.get("include_raw") !== "true",
          ...(url.searchParams.get("include_raw") === "true"
            ? { safe_detail: `{"event_hash":"${"d".repeat(64)}"}` }
            : {}),
        },
      ],
      replay_window: {
        dataset_id: "dataset-owner_console",
        first_ordinal: "1",
        last_ordinal: "20",
        source_identity: "qualified-dataset-window",
      },
      related_alert_ids: ["alert-owner_experience"],
      related_activity_ids: ["activity-owner_experience"],
      evidence_holds: [
        {
          id: "hold-operational_evidence",
          artifact_id: "export-incident-held",
          hold_type: "incident",
          created_at: now,
        },
      ],
      remediation_notes: ["Verified the public feed recovery."],
      resolution_evidence: "Verified the public feed recovery.",
    };
  else if (path === "/api/v1/audit-events") body = pageEnvelope([]);
  else if (path === "/api/v1/audit-verification")
    body = {
      verdict: "valid",
      checked_events: 22,
      head_hash: "e".repeat(64),
      verified_at: now,
    };
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
    id: "binance-sandbox_qualification",
    exchange: "binance",
    environment: "spot_testnet",
    state: "ARMED",
    engine_ready: true,
    account_epoch: 3,
    credential_generation: 2,
    revision: "4",
    session_id: "sandbox-session-sandbox_qualification",
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
    id: "arm-sandbox_qualification",
    session_id: "sandbox-session-sandbox_qualification",
    account_ids: ["binance-sandbox_qualification"],
    state: "active",
    created_at: now,
    expires_at: "2099-07-30T12:15:00Z",
    revision: "1",
    audit_url: "/api/v1/audit-events?event_type=sandbox_arm",
  };
}

function sandboxOrderFixture() {
  return {
    id: "order-sandbox_qualification",
    account_id: "binance-sandbox_qualification",
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
    audit_url: "/api/v1/audit-events?event_type=sandbox_runtime_order",
  };
}

function sandboxReconciliationFixture() {
  return {
    id: "reconciliation-sandbox_qualification",
    account_id: "binance-sandbox_qualification",
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
    audit_url: "/api/v1/audit-events?event_type=sandbox_qualification",
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
    strategy_sessions: [
      {
        id: "strategy-session-sandbox_qualification",
        display_name: "Triangular Arbitrage on Binance",
        strategy_name: "Triangular Arbitrage",
        exchanges: ["binance"],
        instrument: "BTCUSDT",
        state: "running",
        waiting_reason: "Evaluating fresh synchronized market facts.",
        revision: "3",
        created_at: now,
        started_at: now,
        audit_url:
          "/api/v1/audit-events?resource_id=strategy-session-sandbox_qualification",
      },
    ],
    risk_state: "PAUSED",
    qualification: sandboxQualificationFixture(),
    audit_url: "/api/v1/audit-events?event_type=sandbox_qualification",
  };
}

function job(kind: "backtest" | "replay", state: FixtureState) {
  return {
    id: `${kind}-owner_console`,
    kind,
    state: kind === "backtest" ? "SUCCEEDED" : state.replayState,
    mode_label: kind.toUpperCase(),
    revision: kind === "backtest" ? "4" : state.replayRevision.toString(10),
    progress: "1",
    created_at: now,
    updated_at: now,
    input_manifest: {
      configuration_id: "configuration-research_registry",
      dataset_id: "dataset-owner_console",
      research_generation_id: "generation-research_registry-1",
      strategy_version: "trend-following@1.0.0",
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
      run_id: `${kind}-owner_console`,
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
      model_namespace_id: "production-public-trend-following",
      starting_balance_hash: "7".repeat(64),
      confidence_tier: "B",
      canonical_manifest: JSON.stringify({ run_id: `${kind}-owner_console` }),
    },
    result: {
      result_hash: "a".repeat(64),
      platform_correctness: "locally reproducible",
      strategy_evidence: "Tier B local only",
      viability: "undetermined",
      reproducibility: "byte-identical",
      report_id: `report-${kind}-owner_console`,
      report_hash: "b".repeat(64),
      confidence_label: "insufficient",
      research_coverage: "single_run_incomplete",
      disclaimer:
        "Backtest, replay, paper, and shadow results are research evidence only and are not evidence or a guarantee of production profitability.",
      metrics: { net_return: "0.01" },
    },
    registered_report: {
      id: "registered-report-owner_console",
      research_generation_id: "generation-research_registry-1",
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
        research_generation_id: "generation-research_registry-1",
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
              model_namespace_id: "production-public-trend-following",
              created_at: now,
            },
          ],
        }
      : {}),
  };
}
function shadow() {
  return {
    id: "shadow-owner_console",
    state: "RUNNING",
    label: "PUBLIC-LIVE SHADOW / VIRTUAL",
    public_only: true,
    simulation_only: true,
    entries_enabled: true,
    revision: "3",
    configuration_id: "configuration-research_registry",
    strategy_version: "trend-following@1.0.0",
    decision_dataset_id: "dataset-owner_console",
    model_namespace_id: "production-public-trend-following",
    accepted_decisions: 1,
    rejected_decisions: 1,
    journal_transactions: 2,
    activity_state: "waiting",
    waiting_reason_code: "waiting_for_finalized_4h_candle",
    waiting_reason:
      "No Trend decision is due until the next finalized four-hour candle.",
    next_evaluation_at: "2026-07-16T16:00:02Z",
    trigger_condition:
      "After the next finalized four-hour candle and its configured finalization delay.",
    input_health: [
      {
        exchange: "binance",
        instrument: "BTC/USDT",
        state: "HEALTHY",
        reason:
          "The production-public order book and clock evidence are healthy and fresh.",
        fresh: true,
        book_version: "9",
        age_milliseconds: "18",
        observed_at: now,
      },
    ],
    risk_state: "RESUMED",
    created_at: now,
    started_at: now,
    portfolio_id: "portfolio-owner_console",
    run_id: "shadow-owner_console",
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
        id: "decision-shadow-owner_console",
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
        id: "order-owner_console",
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
    id: "command-owner_console",
    state: "applied",
    target_id: target,
    revision: "2",
    correlation_id: "correlation-owner_console",
    created_at: now,
  };
}
