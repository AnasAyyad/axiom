import { describe, expect, it } from "vitest";

import { parseAPIResponse } from "./validation";

const reason = {
  code: "risk.entry_blocked",
  summary: "Entry blocked",
  explanation: "Central risk policy rejected this candidate.",
  suggested_action: "Review the scoped risk state before retrying.",
  severity: "warning",
  unknown: false,
  version: "1",
};

describe("owner control browser contract validation", () => {
  it("accepts a reasoned immutable activity snapshot", () => {
    const parsed = parseAPIResponse(
      "GET /api/v1/activity?page_size=50&view=decisions_orders",
      {
        items: [
          {
            id: "activity-1",
            activity_revision: "2",
            view: "decisions_orders",
            source_type: "decisions",
            source_id: "decision-1",
            source_revision: "1",
            outcome: "rejected",
            reason,
            correlation_id: "correlation-1",
            occurred_at: "2026-08-03T12:00:00Z",
            details: {},
            links: { self: "/api/v1/activity/activity-1" },
          },
        ],
        revision: "2",
        snapshot_revision: "2",
        has_more: false,
      },
    );
    expect(parsed?.success).toBe(true);
  });

  it("rejects activity without a plain explanation or suggested action", () => {
    const parsed = parseAPIResponse("GET /api/v1/activity/activity-1", {
      id: "activity-1",
      activity_revision: "2",
      view: "system_events",
      source_type: "alerts",
      source_id: "alert-1",
      source_revision: "1",
      outcome: "recorded",
      reason: { ...reason, explanation: "", suggested_action: "" },
      correlation_id: "correlation-1",
      occurred_at: "2026-08-03T12:00:00Z",
      details: {},
      links: {},
    });
    expect(parsed?.success).toBe(false);
  });

  it("validates exact-purpose high-risk grants", () => {
    const parsed = parseAPIResponse("POST /api/v1/authorizations", {
      token: "a".repeat(32),
      purpose: "qualification_start",
      target_revision: "4",
      expires_at: "2026-08-03T12:05:00Z",
    });
    expect(parsed?.success).toBe(true);
  });

  it("preserves the exact legacy trend route ahead of the owner control strategy parameter", () => {
    const parsed = parseAPIResponse("GET /api/v1/strategies/trend", {
      version: "trend-following@1.0.0",
      timeframe: "4h",
      health: "healthy",
      evidence_maturity: "local_tier_b",
      parameters: Array.from({ length: 16 }, (_, index) => ({
        id: `parameter-${index}`,
        value: "1",
        unit: "count",
        cadence: "4h",
        mutability: "immutable_per_run",
      })),
      revision: "1",
    });
    expect(parsed?.success).toBe(true);
  });
});
