import { render, screen, waitFor } from "@testing-library/react";
import axe from "axe-core";
import { afterEach, describe, expect, it, vi } from "vitest";

import { HealthPage } from "./HealthPage";

const responses: Record<string, unknown> = {
  "/health/ready": { status: "ready", role: "api", phase: "A1" },
  "/api/v1/system/build": {
    version: "0.1.0",
    commit: "test",
    built_at: "test",
    go_version: "go1.26.5",
    dirty: false,
  },
  "/api/v1/system/status": {
    release: "V1A",
    phase: "A1",
    role: "api",
    lifecycle_state: "READY_PAUSED",
    strategy_activation: "unavailable",
    real_trading_enabled: false,
  },
};

afterEach(() => vi.unstubAllGlobals());

describe("HealthPage", () => {
  it("shows the hard lock and backend status accessibly", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        return Promise.resolve(
          new Response(JSON.stringify(responses[path]), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          }),
        );
      }),
    );

    const view = render(<HealthPage />);
    expect(screen.getByText("REAL TRADING DISABLED")).toBeInTheDocument();
    await screen.findByText("READY_PAUSED");
    await waitFor(async () => {
      const result = await axe.run(view.container);
      expect(result.violations).toHaveLength(0);
    });
  });

  it("fails closed when the status claims real trading", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const path = String(input);
        const body =
          path === "/api/v1/system/status"
            ? { ...(responses[path] as object), real_trading_enabled: true }
            : responses[path];
        return Promise.resolve(
          new Response(JSON.stringify(body), { status: 200 }),
        );
      }),
    );

    render(<HealthPage />);
    expect(await screen.findByText("Health unavailable")).toBeInTheDocument();
    expect(screen.queryByText("READY_PAUSED")).not.toBeInTheDocument();
  });
});
