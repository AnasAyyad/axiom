import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { APIModel } from "../../api/client";
import { ShadowSessionEvidence } from "./ShadowSessionEvidence";
import { shadowEvaluationCountdown } from "./shadowActivityModel";

const session = {
  id: "shadow-owner-test",
  state: "RUNNING",
  label: "PUBLIC-LIVE SHADOW / VIRTUAL",
  public_only: true,
  simulation_only: true,
  entries_enabled: true,
  revision: "3",
  created_at: "2026-08-09T10:00:00Z",
  configuration_id: "configuration-owner",
  strategy_version: "trend-following@1.0.0",
  decision_dataset_id: "",
  model_namespace_id: "production-public-models",
  accepted_decisions: 0,
  rejected_decisions: 0,
  journal_transactions: 0,
  activity_state: "waiting",
  waiting_reason_code: "waiting_for_finalized_4h_candle",
  waiting_reason:
    "No Trend decision is due yet. The next eligible four-hour candle becomes usable at 2026-08-09T12:00:02Z.",
  next_evaluation_at: "2026-08-09T12:00:02Z",
  trigger_condition:
    "After the next finalized four-hour candle and its configured finalization delay.",
  input_health: [
    {
      exchange: "bybit",
      instrument: "BTC/USDT",
      state: "HEALTHY",
      reason:
        "The production-public order book and clock evidence are healthy and fresh.",
      fresh: true,
      book_version: "9",
      age_milliseconds: "18",
      observed_at: "2026-08-09T10:00:00Z",
    },
  ],
} as APIModel<"ShadowSessionResource">;

describe("ShadowSessionEvidence", () => {
  it("shows exact waiting cadence and every required input", () => {
    render(
      <ShadowSessionEvidence
        session={session}
        canControl={false}
        stopPending={false}
        onStop={() => undefined}
      />,
    );

    expect(screen.getByText("Why is nothing happening?")).toBeInTheDocument();
    expect(screen.getAllByText(/next eligible four-hour candle/i)).toHaveLength(
      2,
    );
    expect(screen.getByText("Bybit production public")).toBeVisible();
    expect(screen.getByText("BTC/USDT")).toBeVisible();
    expect(
      screen.getByText("Freshness for every required strategy input"),
    ).toBeVisible();
  });

  it("formats the countdown without going negative", () => {
    expect(
      shadowEvaluationCountdown(
        Date.parse("2026-08-09T11:59:00Z"),
        "2026-08-09T12:00:02Z",
      ),
    ).toBe("0h 1m 2s remaining");
    expect(
      shadowEvaluationCountdown(
        Date.parse("2026-08-09T12:01:00Z"),
        "2026-08-09T12:00:02Z",
      ),
    ).toMatch(/Due now/);
  });
});
