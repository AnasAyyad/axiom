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
