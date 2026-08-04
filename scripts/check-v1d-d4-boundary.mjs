import { readFileSync, readdirSync } from "node:fs";

const read = (path) => readFileSync(path, "utf8");
const requireTokens = (path, tokens) => {
  const source = read(path);
  for (const token of tokens) {
    if (!source.includes(token)) throw new Error(`${path} omits ${token}`);
  }
  return source;
};

requireTokens("crypto_bot_v1_codex_spec.md", [
  "### Phase D4: Reports, incidents, audit, and alert delivery",
  "durable, deduplicated",
  "immutable hash-linked timeline",
  "explicit verification verdict",
]);

requireTokens("api/openapi.yaml", [
  "operationId: createIncident",
  "operationId: updateIncident",
  "operationId: createIncidentEvidenceBundle",
  "operationId: verifyAuditChain",
  "operationId: getAlert",
  "operationId: escalateAlert",
  "operationId: testAlertRoute",
  "operationId: createReport",
  "operationId: getReport",
  "operationId: createReportSchedule",
  "AuditVerification:",
  "ReportProvenance:",
  "EvidenceBundleRequest:",
]);

requireTokens(
  "internal/storage/postgres/migrations/000026_v1d_d4_ops_evidence.sql",
  [
    "v1d_incident_events",
    "v1d_incident_replay_inputs",
    "v1d_report_schedules",
    "v1d_alert_delivery_attempts",
    "v1d_audit_chain",
    "validate_v1d_artifact_hold_reference",
    "('webhook','webhook',false",
  ],
);
requireTokens("internal/storage/postgres/d4_report_worker.go", [
  "FOR UPDATE OF job,report SKIP LOCKED",
  "report_generation_failed",
  "SAVEPOINT d4_report_content",
]);
requireTokens("internal/storage/postgres/d4_incident_commands.go", [
  "manifest.state='qualified'",
  "manifest.dataset_kind='decision_inputs'",
  "v1d_incident_replay_inputs",
]);
requireTokens("internal/storage/postgres/d4_audit_read.go", [
  "link.previous != prior",
  "link.stored != link.authoritativeHash",
  "audit_chain_link_invalid",
]);
requireTokens("internal/storage/postgres/d4_report_content.go", [
  "Historical, replay, shadow, Testnet, and Demo results do not prove profitability.",
  '"real_trading_enabled": false',
  '"strategy_viability"',
  '"platform_readiness"',
]);

for (const path of readdirSync("internal/storage/postgres")
  .filter(
    (name) =>
      name.startsWith("d4_") &&
      name.endsWith(".go") &&
      !name.endsWith("_test.go"),
  )
  .map((name) => `internal/storage/postgres/${name}`)) {
  const source = read(path);
  for (const forbidden of [
    '"net/http"',
    '"axiom/internal/exchange',
    "http.Client",
    "Authorization:",
    "request_signature",
    "private_payload",
  ]) {
    if (source.includes(forbidden)) {
      throw new Error(
        `${path} crosses the D4 outbound or secret boundary with ${forbidden}`,
      );
    }
  }
}

requireTokens("web/src/features/operations/ReportCenterPage.tsx", [
  "On-demand and scheduled evidence",
  "Create an on-demand report",
  "do not prove profitability",
]);
requireTokens("web/src/features/operations/ReportSchedulePanel.tsx", [
  "UTC schedules",
  "UTC",
]);
requireTokens("web/src/features/operations/IncidentCenterPage.tsx", [
  "Incident Center",
  "Open incident workspace",
]);
requireTokens("web/src/features/operations/IncidentEvidenceSummary.tsx", [
  "Deterministic replay input",
  "Evidence holds",
]);
requireTokens("web/src/features/operations/IncidentControls.tsx", [
  "Resolution evidence",
  "link_replay",
]);
requireTokens("web/src/features/operations/AlertDetailPage.tsx", [
  "Delivery attempts",
  "Escalations",
]);
requireTokens("web/src/app/AuditPage.tsx", [
  "Audit chain integrity",
  "Integrity verification failed",
]);
requireTokens("internal/storage/postgres/d4_integration_test.go", [
  "TestV1DD4PostgresOperationalEvidenceQualification",
  "TestV1DD4PostgresD1ToD4UpgradeQualification",
  "dataset-d4-replay",
  "report_generation_failed",
  "D4 immutable alert delivery attempt accepted mutation",
]);
requireTokens("web/tests/e2e/a11-workflow.spec.ts", [
  "D4 operational evidence workflows are responsive, redacted, and actionable",
]);
requireTokens("docs/requirements/v1d-d4-traceability.md", [
  "AX-V1D-D04-001",
  "AX-V1D-D04-009",
]);
requireTokens("docs/requirements/v1d-d4-source-coverage.md", [
  "B2, C6,",
  "D5 remain separate",
]);
requireTokens("docs/adr/0024-v1d-d4-operational-evidence.md", [
  "D4 owns no exchange client",
  "webhook route is",
]);

console.log(
  "V1D D4 operational-evidence, redaction, and safety boundary passed",
);
