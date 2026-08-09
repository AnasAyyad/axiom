import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RunDetailPage } from "./RunDetailPage";

afterEach(() => vi.unstubAllGlobals());

describe("RunDetailPage", () => {
  it("keeps empty evidence explicit and exposes only advanced reproducibility identifiers", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        const body = path.endsWith("/portfolio")
          ? {
              state: "not_recorded",
              waiting_reason:
                "No reducer-owned portfolio snapshot has been recorded for this run yet.",
            }
          : path.endsWith("/risk")
            ? {
                state: "not_recorded",
                summary: "No run-scoped risk projection is recorded.",
              }
            : path.endsWith("/evidence")
              ? {
                  state: "recorded",
                  manifest_hash: "a".repeat(64),
                  source_commit: "b".repeat(40),
                  confidence_tier: "B",
                }
              : /\/(timeline|decisions|orders|fills)$/.test(path)
                ? { items: [] }
                : {
                    id: "run-a",
                    friendly_name: "Trend Following backtest",
                    strategy_id: "trend-following",
                    strategy_version: "trend-following@1.0.0",
                    mode: "replay",
                    environment: "recorded_data",
                    state: "PAUSED",
                    order_capable: true,
                    available_actions: ["resume", "step"],
                    revision: "1",
                    created_at: "2026-08-05T00:00:00Z",
                  };
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/runs/run-a"]}>
          <Routes>
            <Route path="/runs/:id" element={<RunDetailPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText("Trend Following backtest"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Safe controls" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "resume" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "step" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Portfolio & P&L" }));
    expect(
      await screen.findByText(/No reducer-owned portfolio snapshot/i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    expect(await screen.findByText("0 recorded item(s).")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Orders & Fills" }));
    expect(screen.getAllByText("0 recorded item(s).")).toHaveLength(2);
    fireEvent.click(screen.getByRole("tab", { name: "Evidence" }));
    expect(
      await screen.findByText("Advanced reproducibility identity"),
    ).toBeInTheDocument();
  });

  it("shows run-scoped sandbox timeline, P&L, and central-risk evidence", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        const body = path.endsWith("/portfolio")
          ? {
              state: "recorded",
              summary:
                "Latest immutable accounting and central-risk valuation recorded for 1 exchange account(s).",
              ordinal: "3",
              realized_pnl: "1.25",
              unrealized_pnl: "0.50",
              total_pnl: "1.75",
              account_drawdown: "0.01",
              positions: [
                {
                  exchange: "binance",
                  instrument: "BTCUSDT",
                  quantity: "0.001",
                  total_cost: "10",
                  weighted_average_cost: "10000",
                  realized_pnl: "1.25",
                  valuation_state: "complete",
                  updated_at: "2026-08-05T00:00:00Z",
                },
              ],
              fees: [],
            }
          : path.endsWith("/risk")
            ? {
                state: "recorded",
                status: "normal",
                summary:
                  "Latest immutable central-risk inputs recorded for 1 exchange account(s).",
                blockers: [],
                observations: [
                  {
                    exchange: "binance",
                    instrument: "BTCUSDT",
                    policy_version: "1",
                    account_drawdown: "0.01",
                    utc_day_loss: "0",
                    rolling_24_hour_loss: "0",
                    strategy_loss: "0",
                    asset_exposure: "10",
                    combined_exposure: "10",
                    exchange_exposure: "10",
                    reserve: "90",
                    reserved_capital: "0",
                    spread: "0.001",
                    slippage: "0",
                    open_orders: 0,
                    quality_score: 100,
                    health_blockers: [],
                    observed_at: "2026-08-05T00:00:00Z",
                    evidence_hash: "c".repeat(64),
                  },
                ],
              }
            : path.endsWith("/evidence")
              ? { state: "not_recorded" }
              : path.endsWith("/timeline")
                ? {
                    items: [
                      {
                        ordinal: "1",
                        kind: "event",
                        content_hash: "d".repeat(64),
                        canonical_payload: JSON.stringify({
                          event_type: "strategy_evaluation",
                          exchange: "binance",
                          state: "waiting",
                          reason: "waiting_for_finalized_candle",
                        }),
                      },
                    ],
                  }
                : /\/(decisions|orders|fills)$/.test(path)
                  ? { items: [] }
                  : {
                      id: "sandbox-run",
                      friendly_name:
                        "Trend Following · Binance Spot Testnet · BTC/USDT",
                      strategy_id: "trend-following",
                      strategy_version: "trend-following@1.0.0",
                      mode: "sandbox",
                      environment: "binance_spot_testnet",
                      state: "running",
                      order_capable: true,
                      available_actions: ["stop"],
                      revision: "2",
                      created_at: "2026-08-05T00:00:00Z",
                    };
        return new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/runs/sandbox-run"]}>
          <Routes>
            <Route path="/runs/:id" element={<RunDetailPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText(
        "Trend Following · Binance Spot Testnet · BTC/USDT",
      ),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Timeline" }));
    expect(
      await screen.findByText(/waiting for finalized candle/i),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Portfolio & P&L" }));
    expect(await screen.findByText("1.75")).toBeInTheDocument();
    expect(screen.getByText("Binance Spot Testnet")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Risk" }));
    expect(
      await screen.findByText(/Current run risk status: normal/i),
    ).toBeInTheDocument();
    expect(screen.getByText("100/100")).toBeInTheDocument();
  });
});
