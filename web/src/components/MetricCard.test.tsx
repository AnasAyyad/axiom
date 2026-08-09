import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { MetricCard } from "./MetricCard";

describe("MetricCard", () => {
  it("explains a metric through the shared accessible help control", () => {
    render(<MetricCard label="Virtual equity" value="100 USDT" />);

    const help = screen.getByRole("button", { name: "About Virtual equity" });
    fireEvent.focus(help);

    expect(
      screen.getByText(/most recently retrieved server-authoritative value/i),
    ).toBeVisible();
    expect(
      screen.getByText(/does not prove strategy profitability/i),
    ).toBeVisible();
  });
});
