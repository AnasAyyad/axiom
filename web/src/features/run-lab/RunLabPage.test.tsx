import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RunLabPage } from "./RunLabPage";

afterEach(() => vi.unstubAllGlobals());

describe("RunLabPage", () => {
  it("keeps run history visible when the optional catalogue fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        if (String(input) === "/api/v1/run-catalog") {
          return new Response(
            JSON.stringify({
              code: "RUN_CATALOGUE_UNAVAILABLE",
              correlation_id: "catalogue-correlation",
              message: "catalogue unavailable",
              summary: "The reviewed run catalogue is rebuilding.",
              suggested_action: "Wait for the data audit to complete.",
            }),
            { status: 503 },
          );
        }
        return new Response(JSON.stringify({ items: [] }), { status: 200 });
      }),
    );
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <RunLabPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(
      await screen.findByText(/The reviewed run catalogue is rebuilding/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/No durable runs have been created yet/),
    ).toBeInTheDocument();
  });

  it("uses server-approved semantic choices and explains advisory rebalancing", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const path = String(input);
        const body =
          path === "/api/v1/runs"
            ? { items: [] }
            : {
                choices: [
                  {
                    strategy_id: "trend-following",
                    strategy_name: "Trend Following",
                    strategy_version: "trend-following@1.0.0",
                    mode: "shadow",
                    exchanges: ["binance"],
                    instrument: "BTC/USDT",
                    cadence: "After each finalized 4-hour candle",
                    warmup: "Required candle history",
                    order_capable: true,
                  },
                  {
                    strategy_id: "mean-reversion",
                    strategy_name: "Mean Reversion",
                    strategy_version: "mean-reversion@1.0.0",
                    mode: "shadow",
                    exchanges: ["bybit"],
                    instrument: "ETH/USDT",
                    cadence:
                      "After each finalized 1-hour candle with 4-hour context",
                    warmup: "Required 1-hour and 4-hour history",
                    order_capable: true,
                  },
                  {
                    strategy_id: "trend-following",
                    strategy_name: "Trend Following",
                    strategy_version: "trend-following@1.0.0",
                    mode: "shadow",
                    exchanges: ["bybit"],
                    instrument: "BTC/USDT",
                    cadence: "After each finalized 4-hour candle",
                    warmup: "Required candle history",
                    order_capable: true,
                  },
                  {
                    strategy_id: "inventory-rebalancing",
                    strategy_name: "Inventory Rebalancing",
                    strategy_version: "inventory-rebalancing@1.0.0",
                    mode: "shadow",
                    exchanges: ["binance", "bybit"],
                    instrument: "BTC/USDT",
                    cadence: "When inventory changes",
                    warmup: "Current inventory",
                    order_capable: false,
                  },
                ],
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
        <MemoryRouter>
          <RunLabPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    const guidedProof = await screen.findByRole("button", {
      name: /Guided proof/i,
    });
    expect(guidedProof).not.toBeDisabled();
    fireEvent.click(guidedProof);
    expect(
      screen.getByRole("link", { name: "Open guided demonstrations" }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: /Live public-data shadow/i }),
    );
    expect(await screen.findByText("Trend Following")).toBeInTheDocument();
    expect(screen.getByText("Mean Reversion")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Trend Following/i }));
    expect(
      screen.getByRole("button", { name: /BTC\/USDT on binance/i }),
    ).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: /BTC\/USDT on bybit/i }),
    );
    expect(screen.getByText("BTC/USDT · bybit")).toBeInTheDocument();
    fireEvent.click(
      screen.getByRole("button", { name: /Inventory Rebalancing/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /BTC\/USDT on binance and bybit/i }),
    );
    expect(
      screen.getByText(/Advisory only\. It cannot submit a transfer or order/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /Exchange sandbox/i }),
    ).toBeDisabled();
  });

  it("starts a recorded-data run with the reviewed semantic choice", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/api/v1/runs" && init?.method === "POST") {
          return new Response(
            JSON.stringify({
              id: "run-triangular-1",
              friendly_name: "Triangular Arbitrage backtest",
              strategy_id: "triangular-arbitrage",
              strategy_version: "triangular-arbitrage@1.0.0",
              mode: "backtest",
              environment: "recorded_data",
              state: "accepted",
              order_capable: true,
              available_actions: [],
              revision: "1",
              created_at: "2026-08-05T12:00:00Z",
            }),
            { status: 202, headers: { "Content-Type": "application/json" } },
          );
        }
        return new Response(
          JSON.stringify(
            path === "/api/v1/runs"
              ? { items: [] }
              : {
                  choices: [
                    {
                      strategy_id: "triangular-arbitrage",
                      strategy_name: "Triangular Arbitrage",
                      strategy_version: "triangular-arbitrage@1.0.0",
                      mode: "backtest",
                      exchanges: ["binance"],
                      instrument: "BTC/USDT",
                      cadence: "For every recorded event",
                      warmup: "Qualified recorded inputs",
                      order_capable: true,
                    },
                  ],
                },
          ),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <RunLabPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    fireEvent.click(
      await screen.findByRole("button", { name: /Historical test/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /Triangular Arbitrage/i }),
    );
    fireEvent.click(
      screen.getByRole("button", { name: /BTC\/USDT on binance/i }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Start reviewed run" }));

    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith(
        "/api/v1/runs",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            strategy_id: "triangular-arbitrage",
            strategy_version: "triangular-arbitrage@1.0.0",
            mode: "backtest",
            exchanges: ["binance"],
            instrument: "BTC/USDT",
            preset: "latest-qualified-inputs",
          }),
        }),
      ),
    );
  });
});
