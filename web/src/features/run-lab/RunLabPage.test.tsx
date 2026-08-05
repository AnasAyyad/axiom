import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import { RunLabPage } from "./RunLabPage";

afterEach(() => vi.unstubAllGlobals());

describe("RunLabPage", () => {
  it("uses server-approved semantic choices and explains advisory rebalancing", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            JSON.stringify({
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
            }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          ),
      ),
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
    expect(await screen.findByText("Trend Following")).toBeInTheDocument();
    expect(
      screen.getByText(/Advisory recommendations only/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Continue with this run")).toHaveLength(2);
  });
});
