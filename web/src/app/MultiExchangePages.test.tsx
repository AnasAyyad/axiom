import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import axe from "axe-core";
import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";

import { getAPI } from "../api/client";
import { OpportunityScanner } from "./MultiExchangePages";

vi.mock("../api/client", () => ({
  getAPI: vi.fn(),
  postAPI: vi.fn(),
  newIdempotencyKey: vi.fn(() => "test-idempotency-key"),
}));

const quality = {
  tier: "local_tier_b",
  confidence: "high",
  freshness: "fresh",
  source: "immutable_simulation_evidence",
  observed_at: "2026-07-24T12:00:00Z",
  provenance_complete: true,
} as const;

const summary = {
  id: "decision-b8",
  kind: "cross_exchange",
  label: "buy_binance_sell_bybit",
  buy_exchange: "binance",
  sell_exchange: "bybit",
  instrument: "BTCUSDT",
  gross_metric: "0.01",
  net_metric: "0.006",
  expected_profit: "0.60",
  worst_case_profit: "0.20",
  maximum_size: "100",
  tested_size: "50",
  status: "simulated",
  simulation_only: true,
  strategy_version: "cross-v1",
  quality,
  recorded_at: "2026-07-24T12:00:00Z",
  revision: "3",
} as const;

beforeEach(() => {
  vi.mocked(getAPI).mockImplementation(async (path) => {
    if (path.startsWith("/api/v1/opportunities?")) {
      return {
        items: [summary],
        has_more: false,
        revision: "4",
        snapshot_revision: "4",
      } as never;
    }
    return {
      summary,
      legs: [
        {
          index: 0,
          exchange: "binance",
          instrument: "BTCUSDT",
          side: "buy",
          input_quantity: "50",
          trade_quantity: "0.001",
          gross_output: "0.001",
          net_output: "0.00099",
          fee_asset: "BTC",
          fee_quantity: "0.00001",
          fee_quote_equivalent: "0.50",
          vwap: "50000",
          depth_cost: "0.05",
          state: "FILLED",
          revision: "4",
        },
      ],
      inventory: [],
      recovery: {
        attempted: false,
        succeeded: false,
        quarantined: false,
        disposition: "both_filled",
        explanation: "Closed-cycle simulation completed.",
        recovery_loss: "0",
      },
      cost_attribution: { buy_fee: "0.05" },
      timeline: [
        {
          index: 0,
          event_type: "cross_exchange.simulation",
          label: "Simulation outcome recorded",
          occurred_at: "2026-07-24T12:00:00Z",
          correlation_id: "decision-b8",
          revision: "4",
        },
      ],
      raw_evidence_available: true,
    } as never;
  });
});

it("is accessible and exposes opportunity evidence from a keyboard-native row control", async () => {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <OpportunityScanner />
    </QueryClientProvider>,
  );
  const row = await screen.findByRole("button", {
    name: /cross exchange.*decision-b8/i,
  });
  expect(row).toHaveAttribute("aria-expanded", "false");
  fireEvent.click(row);
  expect(await screen.findByText("Simulation outcome recorded")).toBeVisible();
  expect(row).toHaveAttribute("aria-expanded", "true");
  const result = await axe.run(view.container);
  expect(result.violations).toEqual([]);
});
