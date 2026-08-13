import { describe, expect, it } from "vitest";

import { parseAPIResponse } from "./validation";

const shadowSession = {
  id: "shadow-validation-test",
  state: "RUNNING",
  label: "PUBLIC-LIVE SHADOW / VIRTUAL",
  public_only: true,
  simulation_only: true,
  entries_enabled: true,
  revision: "3",
  configuration_id: "configuration-test",
  strategy_version: "trend-following@1.0.0",
  decision_dataset_id: "dataset-test",
  model_namespace_id: "production-public-models",
  accepted_decisions: 0,
  rejected_decisions: 0,
  journal_transactions: 0,
  activity_state: "waiting",
  waiting_reason_code: "waiting_for_finalized_4h_candle",
  waiting_reason: "Waiting for the next finalized four-hour candle.",
  trigger_condition: "Evaluate after the next finalized four-hour candle.",
  input_health: [],
  created_at: "2026-08-09T10:00:00Z",
};

describe("legacy shadow response validation", () => {
  it("accepts complete owner activity evidence", () => {
    const parsed = parseAPIResponse(
      "GET /api/v1/shadow-sessions/shadow-validation-test",
      shadowSession,
    );

    expect(parsed?.success).toBe(true);
  });

  it("rejects a response that cannot explain current activity", () => {
    const { activity_state: _activityState, ...incomplete } = shadowSession;
    const parsed = parseAPIResponse(
      "GET /api/v1/shadow-sessions/shadow-validation-test",
      incomplete,
    );

    expect(parsed?.success).toBe(false);
  });
});
