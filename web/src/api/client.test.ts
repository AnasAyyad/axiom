import { afterEach, expect, it, vi } from "vitest";

import { APIError, getAPI, parseStreamEvent } from "./client";

afterEach(() => vi.unstubAllGlobals());

it("rejects an unsafe or structurally invalid system response", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            application_version: "test",
            build_commit: "test-commit",
            configuration_identity: "test-configuration",
            readiness_state: "ready",
            lifecycle_state: "RUNNING",
            strategy_activation: "trend-following@1.0.0",
            real_trading_enabled: true,
          }),
          { status: 200 },
        ),
      ),
    ),
  );
  await expect(getAPI<"SystemStatus">("/api/v1/system/status")).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});

it("keeps an owner-actionable workflow blocker without exposing its code to the UI", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            code: "PUBLIC_SHADOW_EXCHANGE_UNAVAILABLE",
            correlation_id: "correlation-run-1",
            message: "Public shadow is not available on this exchange.",
            summary: "Public shadow is currently available on Binance only.",
            detail:
              "The active worker has a Binance public-data boundary and does not substitute another exchange.",
            impact: "No shadow session was created.",
            suggested_action: "Choose Binance or wait for the Bybit worker.",
            blocking_prerequisites: ["Bybit worker installed"],
          }),
          { status: 412 },
        ),
      ),
    ),
  );
  await expect(getAPI<"RunResource">("/api/v1/runs/run-1")).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "PUBLIC_SHADOW_EXCHANGE_UNAVAILABLE",
      details: expect.objectContaining({
        summary: "Public shadow is currently available on Binance only.",
        suggestedAction: "Choose Binance or wait for the Bybit worker.",
      }),
    }),
  );
});

it("accepts only versioned monotonic stream envelopes", () => {
  const valid = parseStreamEvent(
    JSON.stringify({
      id: "event-owner_console",
      stream: "risk",
      schema_version: "axiom.stream.v1",
      revision: "12",
      entity_revision: "2",
      occurred_at: "2026-07-16T12:00:00Z",
      correlation_id: "correlation-owner_console",
      causation_id: "command-owner_console",
      event_type: "resume",
      payload: { state: "NORMAL" },
    }),
  );
  expect(valid.success).toBe(true);
  expect(
    parseStreamEvent(JSON.stringify({ revision: 12, payload: "unsafe" }))
      .success,
  ).toBe(false);
});

it("rejects malformed canonical replay evidence", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            id: "replay-owner_console",
            kind: "replay",
            state: "PAUSED",
            mode_label: "REPLAY",
            revision: "2",
            created_at: "2026-07-16T12:00:00Z",
            replay_inspection: {
              event_count: "1",
              ordinal: "1",
              event_hash: "a".repeat(64),
              canonical_event: "not-json",
              canonical_decision: "{}",
              canonical_orders: "[]",
              canonical_execution_events: "[]",
              canonical_balances: "{}",
            },
          }),
          { status: 200 },
        ),
      ),
    ),
  );
  await expect(
    getAPI<"JobResource">("/api/v1/replays/replay-owner_console"),
  ).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});

it("rejects malformed registered research evidence", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            id: "backtest-owner_console",
            kind: "backtest",
            state: "SUCCEEDED",
            mode_label: "BACKTEST",
            revision: "2",
            created_at: "2026-07-16T12:00:00Z",
            registered_report: {
              id: "report-owner_console",
              research_generation_id: "generation-research_registry-1",
              manifest_hash: "a".repeat(64),
              confidence_label: "local_tier_b",
              platform_correctness: "deterministic suite validated",
              strategy_evidence: "provisional local evidence",
              viability: "undetermined",
              disclaimer: "Research evidence only.",
              run_references: ["run-owner_console"],
              benchmarks: [],
              stress: [],
              capacity: [],
              canonical_manifest: "not-json",
              created_at: "2026-07-16T12:00:00Z",
            },
          }),
          { status: 200 },
        ),
      ),
    ),
  );
  await expect(
    getAPI<"JobResource">("/api/v1/backtests/backtest-owner_console"),
  ).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});

it("accepts isolated multi-exchange console inventory and rejects a combined balance", async () => {
  const response = {
    items: [
      {
        id: "decision-1:buy_venue",
        exchange: "bybit",
        asset: "BTC",
        strategy_version: "cross.v1",
        experiment_id: "run-1",
        portfolio_id: "portfolio-1",
        before: "1",
        after: "0.9",
        available: "0.9",
        reserved: "0",
        status: "normal",
        virtual: true,
        quality: {
          tier: "local_tier_b",
          confidence: "high",
          freshness: "fresh",
          source: "cross_exchange_inventory_snapshots",
          observed_at: "2026-07-24T12:00:00Z",
          provenance_complete: true,
        },
        updated_at: "2026-07-24T12:00:00Z",
        revision: "1",
      },
    ],
    revision: "4",
    snapshot_revision: "4",
    has_more: false,
    combined_balance: false,
    isolation_notice:
      "Inventory remains isolated by every ownership dimension.",
  };
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(JSON.stringify(response), { status: 200 }),
    )
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ ...response, combined_balance: true }), {
        status: 200,
      }),
    );
  vi.stubGlobal("fetch", fetchMock);
  await expect(
    getAPI<"InventoryPage">("/api/v1/inventory?page_size=50"),
  ).resolves.toEqual(response);
  await expect(
    getAPI<"InventoryPage">("/api/v1/inventory?page_size=50"),
  ).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});

it("validates detailed evaluation progress and rejects a reset storage cap", async () => {
  const campaign = {
    id: "evaluation-1",
    preset: "balanced_full_v1",
    state: "RUNNING",
    current_stage: "RECORDER_QUALIFICATION",
    completed_stages: [
      "HISTORICAL_IMPORT",
      "EXISTING_DATA_AUDIT",
      "RECORDER_ROTATION",
    ],
    valid_recording_seconds: 3600,
    valid_shadow_seconds: 0,
    wall_time_seconds: 7200,
    recorded_bytes: 1024,
    recording_limit_bytes: 214_748_364_800,
    stages: [],
    historical_imports: [],
    coverage: [],
    matrix: [],
    feed_health: [],
    revision: "4",
    created_at: "2026-08-11T00:00:00Z",
    updated_at: "2026-08-11T02:00:00Z",
  };
  const fetchMock = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(JSON.stringify(campaign), { status: 200 }),
    )
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({ ...campaign, recording_limit_bytes: 1_000 }),
        { status: 200 },
      ),
    );
  vi.stubGlobal("fetch", fetchMock);
  await expect(
    getAPI<"EvaluationCampaign">("/api/v1/evaluation-campaigns/evaluation-1"),
  ).resolves.toEqual(campaign);
  await expect(
    getAPI<"EvaluationCampaign">("/api/v1/evaluation-campaigns/evaluation-1"),
  ).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});
