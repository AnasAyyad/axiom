import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  GettingStartedPage,
  GuidedDemonstrationsPage,
} from "./OwnerGuidancePages";

afterEach(() => vi.unstubAllGlobals());

describe("GettingStartedPage", () => {
  it("guides an owner through the required safe-first checklist", () => {
    render(
      <MemoryRouter>
        <GettingStartedPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole("heading", { name: "First-login checklist" })).toBeVisible();
    expect(screen.getByRole("link", { name: /Run a guided proof demonstration/ })).toHaveAttribute("href", "/guided-demonstrations");
    expect(screen.getByText(/does not predict returns or prove/i)).toBeInTheDocument();
    expect(screen.getByText(/only after the required strategy-session workflow is installed and armed/i)).toBeVisible();
  });
});

describe("GuidedDemonstrationsPage", () => {
  it("takes the owner through every installed walkthrough without creating a run", async () => {
    vi.stubGlobal("fetch", vi.fn(guidedDemonstrationFetch));
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <MemoryRouter>
          <GuidedDemonstrationsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    );

    expect(await screen.findByText("Trend Following basics")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "Start tour" }));
    expect(
      await screen.findByRole("heading", { name: "Tour step 1 of 2" }),
    ).toBeVisible();
    fireEvent.click(
      screen.getByRole("button", { name: "Continue to Inventory Rebalancing basics" }),
    );
    expect(
      await screen.findByRole("heading", { name: "Tour step 2 of 2" }),
    ).toBeVisible();
    expect(
      screen.getByText("Advisory recommendation evidence"),
    ).toBeVisible();
  });
});

function guidedDemonstrationFetch(input: RequestInfo | URL) {
  const path = String(input);
  if (path === "/api/v1/demonstrations") {
    return Promise.resolve(jsonResponse({
      items: [
        {
          id: "trend-following-basics",
          title: "Trend Following basics",
          description: "Synthetic trend proof.",
          strategy_id: "trend-following",
          strategy_version: "trend-following@1.0.0",
          synthetic: true,
          expected_outcomes: ["Accepted decision"],
        },
        {
          id: "inventory-rebalancing-basics",
          title: "Inventory Rebalancing basics",
          description: "Synthetic advisory proof.",
          strategy_id: "inventory-rebalancing",
          strategy_version: "inventory-rebalancing@1.0.0",
          synthetic: true,
          expected_outcomes: ["Advisory recommendation"],
        },
      ],
    }));
  }
  const advisory = path.endsWith("inventory-rebalancing-basics");
  return Promise.resolve(jsonResponse({
    id: advisory ? "inventory-rebalancing-basics" : "trend-following-basics",
    strategy_id: advisory ? "inventory-rebalancing" : "trend-following",
    strategy_version: advisory
      ? "inventory-rebalancing@1.0.0"
      : "trend-following@1.0.0",
    synthetic: true,
    advisory_only: advisory,
    ...(advisory ? { advisory_evidence: '{"recommendation":"read-only"}' } : {}),
    configuration_hash: "a".repeat(64),
    accepted: guidedEvent("accepted"),
    rejected: guidedEvent("rejected"),
    metrics: "{}",
    result_hash: "b".repeat(64),
  }));
}

function guidedEvent(outcome: string) {
  return {
    ordinal: 1,
    decision: JSON.stringify({ outcome }),
    orders: "[]",
    execution_events: "[]",
    balances: "{}",
  };
}

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
