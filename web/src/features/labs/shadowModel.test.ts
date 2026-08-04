import { expect, it } from "vitest";

import type { APIModel } from "../../api/client";
import { compareShadowSessions } from "./shadowModel";

function shadow(
  id: string,
  realized: string,
): APIModel<"ShadowSessionResource"> {
  return {
    id,
    state: "CANCELED",
    label: "PUBLIC-LIVE SHADOW / VIRTUAL",
    public_only: true,
    simulation_only: true,
    entries_enabled: false,
    revision: "2",
    created_at: "2026-08-03T10:00:00Z",
    configuration_id: "configuration-a",
    strategy_version: "trend.v1a.1",
    decision_dataset_id: "dataset-a",
    model_namespace_id: "models-a",
    accepted_decisions: 3,
    rejected_decisions: 1,
    journal_transactions: 2,
    pnl_attribution: {
      realized_pnl: realized,
      fee_expense: "-0.1",
      spread: "0",
      slippage: "-0.02",
      latency: "-0.01",
      valuation_basis: "sealed_ledger_functional_value",
    },
  };
}

it("compares shadow evidence as exact decimal strings", () => {
  const rows = compareShadowSessions(
    shadow("left", "1.01"),
    shadow("right", "1.010"),
  );
  expect(rows.find((row) => row.field === "Realized P&L")?.changed).toBe(true);
  expect(rows.find((row) => row.field === "Configuration")?.changed).toBe(
    false,
  );
});
