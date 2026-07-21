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
            release: "V1A",
            phase: "A11",
            role: "api",
            lifecycle_state: "RUNNING",
            strategy_activation: "trend.v1a.1",
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

it("accepts only versioned monotonic stream envelopes", () => {
  const valid = parseStreamEvent(
    JSON.stringify({
      id: "event-a11",
      stream: "risk",
      schema_version: "axiom.stream.v1",
      revision: "12",
      entity_revision: "2",
      occurred_at: "2026-07-16T12:00:00Z",
      correlation_id: "correlation-a11",
      causation_id: "command-a11",
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
            id: "replay-a11",
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
    getAPI<"JobResource">("/api/v1/replays/replay-a11"),
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
            id: "backtest-a11",
            kind: "backtest",
            state: "SUCCEEDED",
            mode_label: "BACKTEST",
            revision: "2",
            created_at: "2026-07-16T12:00:00Z",
            registered_report: {
              id: "report-a11",
              research_generation_id: "generation-a10-1",
              manifest_hash: "a".repeat(64),
              confidence_label: "local_tier_b",
              platform_correctness: "deterministic suite validated",
              strategy_evidence: "provisional local evidence",
              viability: "undetermined",
              disclaimer: "Research evidence only.",
              run_references: ["run-a11"],
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
    getAPI<"JobResource">("/api/v1/backtests/backtest-a11"),
  ).rejects.toEqual(
    expect.objectContaining<Partial<APIError>>({
      code: "invalid_server_response",
    }),
  );
});
