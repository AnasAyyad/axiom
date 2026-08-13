import type { APIModel } from "../../api/client";

export const reportTypes: ReadonlyArray<
  APIModel<"ReportRequest">["report_type"]
> = [
  "strategy_results",
  "decisions_orders",
  "portfolios",
  "inventory_pnl",
  "risk",
  "exchange_data_health",
  "lab_runs",
  "sandbox_qualifications",
  "platform_readiness",
];

export function reportLabel(value: string) {
  return value
    .replaceAll("_", " ")
    .replace(/\b\w/g, (character) => character.toUpperCase());
}
