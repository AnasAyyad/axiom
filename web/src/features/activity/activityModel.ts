import type { ActivityFilters } from "../../api/queries";

export const emptyActivityFilters: ActivityFilters = {
  from: "",
  to: "",
  strategy: "",
  instrument: "",
  exchange: "",
  side: "",
  outcome: "",
  reason: "",
  mode: "",
  correlation_id: "",
};

export function localDateTimeToUTC(value: string) {
  if (value === "") return "";
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : date.toISOString();
}

export function safeDownloadName(
  view: "decisions_orders" | "system_events",
  format: string,
) {
  return `axiom-${view.replace("_", "-")}-redacted.${format}`;
}
