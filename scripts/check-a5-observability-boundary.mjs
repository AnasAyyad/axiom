import fs from "node:fs";

const failures = [];
function fail(message) {
  failures.push(message);
  process.stderr.write(`ERROR [a5-observability-boundary] ${message}\n`);
}

const metricsSource = fs.readFileSync(
  "internal/observability/metrics.go",
  "utf8",
);
const metricsDocs = fs.readFileSync(
  "docs/operations/metrics-alerts.md",
  "utf8",
);
const rules = fs.readFileSync("monitoring/alerts.yml", "utf8");
const prometheus = fs.readFileSync("monitoring/prometheus.yml", "utf8");
const compose = fs.readFileSync("docker-compose.yml", "utf8");
const service = fs.readFileSync("internal/alerting/service.go", "utf8");
const support = fs.readFileSync("internal/observability/support.go", "utf8");
const migration = fs
  .readdirSync("internal/storage/postgres/migrations")
  .filter((name) => name.endsWith(".sql"))
  .sort()
  .map((name) =>
    fs.readFileSync(`internal/storage/postgres/migrations/${name}`, "utf8"),
  )
  .join("\n");

const requiredMetrics = [
  "axiom_websocket_messages_total",
  "axiom_websocket_events_total",
  "axiom_order_book_age_seconds",
  "axiom_event_queue_depth",
  "axiom_event_queue_dropped_total",
  "axiom_strategy_evaluations_total",
  "axiom_strategy_candidates",
  "axiom_strategy_rejections_total",
  "axiom_risk_check_duration_seconds",
  "axiom_execution_simulation_duration_seconds",
  "axiom_exchange_rest_duration_seconds",
  "axiom_exchange_rest_failures_total",
  "axiom_websocket_lag_seconds",
  "axiom_shadow_fills_total",
  "axiom_reconciliation_mismatches_total",
  "axiom_journal_failures_total",
  "axiom_virtual_pnl_reporting_units",
  "axiom_virtual_drawdown_ratio",
  "axiom_database_operation_duration_seconds",
  "axiom_database_failures_total",
  "axiom_alerts_open",
  "axiom_dependency_ready",
  "axiom_disk_free_bytes",
];
for (const metric of requiredMetrics) {
  if (!metricsSource.includes(`"${metric}"`))
    fail(`missing collector ${metric}`);
  if (!metricsDocs.includes(`\`${metric}\``))
    fail(`undocumented metric ${metric}`);
}

for (const forbidden of [
  '"order_id"',
  '"decision_id"',
  '"client_id"',
  '"user_id"',
  '"path"',
  '"url"',
  '"error"',
]) {
  if (metricsSource.includes(forbidden))
    fail(`unbounded metric label ${forbidden}`);
}

const dashboard = JSON.parse(
  fs.readFileSync(
    "monitoring/grafana/dashboards/axiom-operations.json",
    "utf8",
  ),
);
if (!Array.isArray(dashboard.panels) || dashboard.panels.length < 8)
  fail("operational dashboard has fewer than eight panels");
for (const panel of dashboard.panels ?? []) {
  if (!panel.title || !panel.fieldConfig?.defaults?.unit)
    fail(`panel ${panel.id ?? "unknown"} lacks a title or unit`);
}

const monitoringText = rules + JSON.stringify(dashboard);
const referenced = new Set(monitoringText.match(/axiom_[a-z0-9_]+/g) ?? []);
for (let metric of referenced) {
  metric = metric.replace(/_(bucket|sum|count)$/, "");
  if (!metricsDocs.includes(`\`${metric}\``))
    fail(`monitoring references undocumented ${metric}`);
  if (!metricsSource.includes(`"${metric}"`))
    fail(`monitoring references absent ${metric}`);
}

const requiredAlerts = [
  "AxiomServiceUnavailable",
  "AxiomDatabaseUnavailable",
  "AxiomFencingLeaseLost",
  "AxiomClockUnsafe",
  "AxiomBooksUnhealthy",
  "AxiomQueueDrops",
  "AxiomDiskLow",
  "AxiomDiskCritical",
  "AxiomReconciliationMismatch",
  "AxiomJournalFailure",
];
for (const alert of requiredAlerts) {
  if (!rules.includes(`alert: ${alert}`)) fail(`missing rule ${alert}`);
  if (!metricsDocs.includes(`\`${alert}\``)) fail(`undocumented rule ${alert}`);
}

for (const invariant of ["rule_files:", "/etc/prometheus/alerts.yml"]) {
  if (!prometheus.includes(invariant) && !compose.includes(invariant))
    fail(`missing Prometheus invariant ${invariant}`);
}
for (const invariant of [
  "alert_deliveries",
  "deduplication_key",
  "alert_acknowledgements",
]) {
  if (!migration.includes(invariant))
    fail(`missing durable alert invariant ${invariant}`);
}
for (const reason of [
  "ReasonPersistenceFailure",
  "ReasonFencingLeaseLost",
  "ReasonDiskCritical",
  "ReasonClockDrift",
  "ReasonQueueSaturated",
  "ReasonBookUnhealthy",
  "ReasonStaleData",
  "ReasonReconciliationMismatch",
  "ReasonAccountingInvariant",
]) {
  if (!service.includes(reason)) fail(`missing fail-closed reason ${reason}`);
}
if (
  !service.includes("service.gate.Lock") ||
  !service.includes("service.store.Upsert")
)
  fail("critical lock must precede durable alert insertion");
for (const forbidden of ["Environment", "Headers", "Payload", "URL", "Path"]) {
  if (support.includes(`${forbidden} `))
    fail(`support bundle exposes forbidden field ${forbidden}`);
}
if (
  !support.includes("RealTradingEnabled") ||
  !support.includes("filterSecrets")
)
  fail("support bundle lacks hard safety/redaction fields");

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `A5 observability boundary validated (${requiredMetrics.length} metrics, ${requiredAlerts.length} alerts)\n`,
);
