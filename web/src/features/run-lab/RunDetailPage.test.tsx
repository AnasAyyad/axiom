import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
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
              waiting_reason: "No reducer-owned portfolio snapshot has been recorded for this run yet.",
            }
          : path.endsWith("/risk")
            ? { state: "not_recorded", summary: "No run-scoped risk projection is recorded." }
            : path.endsWith("/evidence")
              ? { state: "recorded", manifest_hash: "a".repeat(64), source_commit: "b".repeat(40), confidence_tier: "B" }
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
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter initialEntries={["/runs/run-a"]}>
          <Routes>
            <Route path="/runs/:id" element={<RunDetailPage />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(await screen.findByText("Trend Following backtest")).toBeInTheDocument();
    expect(screen.getByText(/No reducer-owned portfolio snapshot/i)).toBeInTheDocument();
    expect(screen.getAllByText("0 recorded item(s).")).toHaveLength(4);
    expect(screen.getByText("Advanced reproducibility identity")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Safe controls" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "resume" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "step" })).toBeInTheDocument();
  });
});
