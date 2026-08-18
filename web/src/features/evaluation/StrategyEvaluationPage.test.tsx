import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { StrategyEvaluationPage } from "./StrategyEvaluationPage";

afterEach(() => vi.unstubAllGlobals());

const campaign = {
  id: "evaluation-campaign-1",
  preset: "balanced_full_v1",
  state: "PAUSED_RECOVERABLE",
  current_stage: "RECORDER_QUALIFICATION",
  completed_stages: [
    "HISTORICAL_IMPORT",
    "EXISTING_DATA_AUDIT",
    "RECORDER_ROTATION",
  ],
  valid_recording_seconds: 3_600,
  valid_shadow_seconds: 0,
  wall_time_seconds: 7_200,
  estimated_remaining_seconds: 255_600,
  recorded_bytes: 1_073_741_824,
  recording_limit_bytes: 214_748_364_800,
  reason_code: "FEED_UNHEALTHY",
  suggested_action:
    "Restore both public feeds; valid time resumes automatically.",
  stages: [
    {
      stage: "HISTORICAL_IMPORT",
      state: "COMPLETED",
      attempt: 1,
      recoverable_failures: 0,
      completed_at: "2026-08-11T01:00:00Z",
      updated_at: "2026-08-11T01:00:00Z",
    },
    {
      stage: "RECORDER_QUALIFICATION",
      state: "PAUSED_RECOVERABLE",
      attempt: 1,
      recoverable_failures: 1,
      reason_code: "FEED_UNHEALTHY",
      next_retry_at: "2026-08-11T02:01:00Z",
      attempts: [
        {
          attempt: 1,
          outcome: "PAUSED_RECOVERABLE",
          reason_code: "FEED_UNHEALTHY",
          summary: "The feed will retry from the same checkpoint.",
          started_at: "2026-08-11T01:00:00Z",
          finished_at: "2026-08-11T02:00:00Z",
          retry_at: "2026-08-11T02:01:00Z",
        },
      ],
      started_at: "2026-08-11T01:00:00Z",
      updated_at: "2026-08-11T02:00:00Z",
    },
  ],
  historical_imports: [],
  coverage: [],
  matrix: [],
  feed_health: [
    {
      exchange: "binance",
      instrument: "BTCUSDT",
      eligible: false,
      book_fresh: false,
      clock_eligible: true,
      latest_event_at: "2026-08-11T02:00:00Z",
      message_count: 100,
      queue_drop_count: 0,
      gap_count: 1,
      decoder_error_count: 0,
    },
  ],
  revision: "4",
  created_at: "2026-08-11T00:00:00Z",
  updated_at: "2026-08-11T02:00:00Z",
};

function renderPage(path = "/strategy-evaluation/evaluation-campaign-1") {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="strategy-evaluation"
            element={<StrategyEvaluationPage />}
          />
          <Route
            path="strategy-evaluation/:id"
            element={<StrategyEvaluationPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("StrategyEvaluationPage", () => {
  it("keeps authoritative progress visible when the optional event query fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/v1/evaluation-campaigns")
          return new Response(JSON.stringify({ items: [campaign] }), {
            status: 200,
          });
        if (path.endsWith("/events"))
          return new Response(
            JSON.stringify({
              code: "TIMELINE_REFRESH_FAILED",
              correlation_id: "timeline-correlation",
              message: "timeline unavailable",
              summary: "The durable timeline is temporarily unavailable.",
            }),
            { status: 503 },
          );
        if (path.endsWith("/report"))
          return new Response(
            JSON.stringify({
              state: "not_ready",
              generated_at: "2026-08-11T02:00:00Z",
            }),
            { status: 200 },
          );
        return new Response(JSON.stringify(campaign), { status: 200 });
      }),
    );
    renderPage();
    expect(await screen.findByText("Current campaign")).toBeInTheDocument();
    expect(screen.getByText(/1.00 GiB of 200.0 GiB/)).toBeInTheDocument();
    expect(screen.getByText(/Restore both public feeds/)).toBeInTheDocument();
    expect(await screen.findByText(/Automatic retry/)).toBeInTheDocument();
    expect(screen.getByText(/Previous attempt preserved/)).toBeInTheDocument();
    expect(
      await screen.findByText(/durable timeline is temporarily unavailable/i),
    ).toBeInTheDocument();
    expect(screen.getByText("binance · BTCUSDT")).toBeInTheDocument();
  });

  it("starts by sending only the server-owned preset", async () => {
    const created = { ...campaign, state: "PENDING", current_stage: undefined };
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/v1/evaluation-campaigns" && init?.method === "POST")
          return new Response(JSON.stringify(created), { status: 202 });
        if (path === "/api/v1/evaluation-campaigns")
          return new Response(JSON.stringify({ items: [] }), { status: 200 });
        return new Response(JSON.stringify(created), { status: 200 });
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    renderPage("/strategy-evaluation");
    const button = await screen.findByRole("button", {
      name: "Start Full Evaluation",
    });
    await waitFor(() => expect(button).not.toBeDisabled());
    fireEvent.click(button);
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/v1/evaluation-campaigns",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({ preset: "balanced_full_v1" }),
        }),
      ),
    );
  });

  it("renders preserved run metrics and the locked backtest/replay/shadow comparison", async () => {
    const rawMetrics = (net: string) => ({
      total_net_return: "0.01",
      maximum_drawdown: "0.0125",
      trades: 25,
      by_strategy: {
        net_result: net,
        opportunities: "50",
        accepted_decisions: "10",
        simulated_orders: "12",
        full_fills: "8",
        partial_fills: "1",
      },
    });
    const member = (
      id: string,
      mode: "backtest" | "replay" | "shadow",
      repeat: number,
      net: string,
    ) => ({
      id,
      strategy: "trend-following",
      configuration: "trend-balanced-02",
      mode,
      capital_micros: 2_000_000_000,
      repeat_ordinal: repeat,
      cost_stress_bps: 10_000,
      state: "SUCCEEDED",
      verdict: "CONTINUE",
      metrics: rawMetrics(net),
    });
    const backtest = member("backtest", "backtest", 0, "3.5");
    const replay = member("replay", "replay", 2, "4.25");
    const shadow = member("shadow", "shadow", 0, "12.5");
    const complete = {
      ...campaign,
      state: "COMPLETED",
      current_stage: undefined,
      reason_code: undefined,
      suggested_action: undefined,
      matrix: [backtest, replay],
      shadow: {
        state: "COMPLETED",
        valid_seconds: 604_800,
        start_ordinal: 1,
        last_processed_ordinal: 100,
        shared_capital_micros: 10_000_000_000,
        protected_reserve_micros: 2_000_000_000,
        member_ceiling_micros: 2_000_000_000,
        members: [shadow],
      },
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        if (path === "/api/v1/evaluation-campaigns")
          return new Response(JSON.stringify({ items: [complete] }), {
            status: 200,
          });
        if (path.endsWith("/events"))
          return new Response(JSON.stringify({ items: [] }), { status: 200 });
        if (path.endsWith("/report"))
          return new Response(
            JSON.stringify({
              state: "final",
              verdict: "CONTINUE",
              summary: "Complete",
              report_hash: "a".repeat(64),
              generated_at: "2026-08-20T00:00:00Z",
              content: {
                candidate_locks: [
                  {
                    strategy: "trend-following",
                    state: "SELECTED",
                    configuration_key: "trend-balanced-02",
                  },
                ],
                members: [backtest, replay, shadow],
              },
            }),
            { status: 200 },
          );
        return new Response(JSON.stringify(complete), { status: 200 });
      }),
    );
    renderPage();
    expect(
      await screen.findByText("50 → 10 → 12 → 8 full / 1 partial"),
    ).toBeInTheDocument();
    expect(screen.getAllByText("3.5 USDT").length).toBeGreaterThan(0);
    expect(screen.getAllByText("4.25 USDT").length).toBeGreaterThan(0);
    expect(screen.getAllByText("12.5 USDT").length).toBeGreaterThan(0);
    expect(screen.getByText("0.0125 ratio")).toBeInTheDocument();
  });
});
