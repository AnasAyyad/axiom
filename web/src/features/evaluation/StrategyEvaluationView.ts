import type { Member, Report } from "./StrategyEvaluationTypes";

export function summarizeMembers(members: readonly Member[]) {
  const terminal = members.filter((member) =>
    ["SUCCEEDED", "FAILED", "EXCLUDED", "CANCELED"].includes(member.state),
  ).length;
  return {
    total: members.length,
    terminal,
    succeeded: members.filter((m) => m.state === "SUCCEEDED").length,
    failed: members.filter((m) => m.state === "FAILED").length,
    running: members.filter((m) => ["QUEUED", "RUNNING"].includes(m.state))
      .length,
  };
}
function metricRecord(member: Member) {
  const metrics = member.metrics;
  if (!metrics) return undefined;
  return isRecord(metrics.selection_metrics)
    ? metrics.selection_metrics
    : metrics;
}
export function memberMetric(member: Member, key: string, suffix = "") {
  const summary = metricRecord(member);
  const ledger = ledgerMetricsRecord(member);
  const byStrategy = isRecord(ledger?.by_strategy)
    ? ledger.by_strategy
    : undefined;
  let value = summary?.[key];
  if (value === undefined && key === "net_result_micros") {
    value = byStrategy?.net_result;
    if (value !== undefined) return `${String(value)} USDT`;
  }
  if (value === undefined && key === "trade_count") value = ledger?.trades;
  if (value === undefined && key === "maximum_drawdown_bps") {
    const drawdown = ledger?.maximum_drawdown;
    if (drawdown !== undefined) return `${String(drawdown)} ratio`;
  }
  if (value === undefined || value === null || value === "") return "—";
  if (key.endsWith("_micros")) {
    return typeof value === "number"
      ? formatUSDT(value)
      : `${String(value)} micro-USDT`;
  }
  return `${String(value)}${suffix}`;
}
export function memberFunnel(member: Member) {
  const metrics = ledgerMetricsRecord(member);
  const byStrategy = isRecord(metrics?.by_strategy)
    ? metrics.by_strategy
    : undefined;
  if (!byStrategy) return "—";
  return [
    byStrategy.opportunities,
    byStrategy.accepted_decisions,
    byStrategy.simulated_orders,
    `${String(byStrategy.full_fills ?? "0")} full / ${String(byStrategy.partial_fills ?? "0")} partial`,
  ]
    .map((value) => String(value ?? "0"))
    .join(" → ");
}
export function reportMemberNet(member?: Record<string, unknown>) {
  if (!member || !isRecord(member.metrics)) return "—";
  const byStrategy = isRecord(member.metrics.by_strategy)
    ? member.metrics.by_strategy
    : undefined;
  if (byStrategy?.net_result !== undefined)
    return `${String(byStrategy.net_result)} USDT`;
  const value = member.metrics.total_net_return;
  if (value === undefined) return "—";
  return `${String(value)} return`;
}

function ledgerMetricsRecord(member: Member) {
  const metrics = member.metrics;
  if (!metrics) return undefined;
  return isRecord(metrics.final_test_metrics)
    ? metrics.final_test_metrics
    : metrics;
}
export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
export function formatLabel(value: string) {
  return value
    .replaceAll("_", " ")
    .replaceAll("-", " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
export function formatTime(value: string) {
  return new Date(value).toLocaleString();
}
export function formatDuration(value?: number) {
  if (value === undefined) return "Pending";
  const days = Math.floor(value / 86_400);
  const hours = Math.floor((value % 86_400) / 3_600);
  const minutes = Math.floor((value % 3_600) / 60);
  return days > 0
    ? `${days}d ${hours}h`
    : hours > 0
      ? `${hours}h ${minutes}m`
      : `${minutes}m`;
}
export function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let size = value;
  let index = -1;
  do {
    size /= 1024;
    index++;
  } while (size >= 1024 && index < units.length - 1);
  return `${size.toFixed(size >= 10 ? 1 : 2)} ${units[index]}`;
}
export function formatUSDT(micros: number) {
  return `${(micros / 1_000_000).toLocaleString(undefined, { maximumFractionDigits: 2 })} USDT`;
}

export function downloadReport(report: Report, format: "json" | "html") {
  const payload = report.content ?? report;
  const json = JSON.stringify(payload, null, 2);
  const content =
    format === "json"
      ? json
      : `<!doctype html><html lang="en"><meta charset="utf-8"><title>Axiom strategy evaluation report</title><style>body{font:16px/1.5 system-ui;max-width:80rem;margin:auto;padding:2rem;color:#17211f}pre{white-space:pre-wrap;overflow-wrap:anywhere;background:#f3f6f5;padding:1rem;border-radius:.5rem}</style><h1>Axiom strategy evaluation report</h1><p>Simulation-only, spot-only research evidence.</p><pre>${escapeHTML(json)}</pre></html>`;
  const url = URL.createObjectURL(
    new Blob([content], {
      type: format === "json" ? "application/json" : "text/html",
    }),
  );
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = `axiom-strategy-evaluation.${format}`;
  anchor.click();
  URL.revokeObjectURL(url);
}
function escapeHTML(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}
