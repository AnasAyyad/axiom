import type { APIModel } from "../../api/client";

export function compareShadowSessions(
  left: APIModel<"ShadowSessionResource">,
  right: APIModel<"ShadowSessionResource">,
) {
  const fields: ReadonlyArray<readonly [string, unknown, unknown]> = [
    ["State", left.state, right.state],
    ["Configuration", left.configuration_id, right.configuration_id],
    ["Strategy", left.strategy_version, right.strategy_version],
    ["Model namespace", left.model_namespace_id, right.model_namespace_id],
    ["Decision dataset", left.decision_dataset_id, right.decision_dataset_id],
    ["Accepted decisions", left.accepted_decisions, right.accepted_decisions],
    ["Rejected decisions", left.rejected_decisions, right.rejected_decisions],
    [
      "Journal transactions",
      left.journal_transactions,
      right.journal_transactions,
    ],
    [
      "Realized P&L",
      left.pnl_attribution?.realized_pnl,
      right.pnl_attribution?.realized_pnl,
    ],
    [
      "Fee expense",
      left.pnl_attribution?.fee_expense,
      right.pnl_attribution?.fee_expense,
    ],
    [
      "Slippage attribution",
      left.pnl_attribution?.slippage,
      right.pnl_attribution?.slippage,
    ],
    [
      "Latency attribution",
      left.pnl_attribution?.latency,
      right.pnl_attribution?.latency,
    ],
  ];
  return fields.map(([field, leftValue, rightValue]) => ({
    id: field,
    field,
    left: leftValue === undefined ? "Not available" : String(leftValue),
    right: rightValue === undefined ? "Not available" : String(rightValue),
    changed: leftValue !== rightValue,
  }));
}
