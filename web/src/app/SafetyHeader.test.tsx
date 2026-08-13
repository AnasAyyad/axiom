import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { APIModel } from "../api/client";
import { SafetyHeader } from "./SafetyHeader";

const system = {
  execution_mode: "shadow",
  environment: "production_public",
} as APIModel<"SystemStatus">;
const binance = {
  websocket_state: "healthy",
  book_state: "healthy",
  recorder_state: "healthy",
} as APIModel<"BinanceHealth">;
const exchanges = {
  items: [
    { id: "binance", websocket_state: "healthy" },
    { id: "bybit", websocket_state: "stale" },
  ],
} as APIModel<"ExchangePage">;
const risk = { state: "NORMAL" } as APIModel<"RiskStatus">;

describe("SafetyHeader", () => {
  it("represents Binance and Bybit public-data state independently", () => {
    render(
      <SafetyHeader
        system={system}
        binance={binance}
        exchanges={exchanges}
        risk={risk}
        criticalAlerts={2}
        streamState="live"
      />,
    );

    expect(
      screen.getByText("REAL-MONEY TRADING IS NOT AVAILABLE"),
    ).toBeInTheDocument();
    expect(screen.getByText("Binance data")).toBeInTheDocument();
    expect(screen.getByText("Bybit data")).toBeInTheDocument();
    expect(screen.getByText("Critical alerts")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
    expect(screen.getByText("stale")).toBeInTheDocument();
    expect(screen.getByText("attention required")).toBeInTheDocument();
  });

  it("fails closed when Bybit recorder evidence is absent", () => {
    render(<SafetyHeader streamState="live" />);

    expect(screen.getAllByText("unavailable").length).toBeGreaterThanOrEqual(2);
    expect(screen.getByText("attention required")).toBeInTheDocument();
  });
});
