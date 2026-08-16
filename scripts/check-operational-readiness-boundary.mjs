import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";

const read = (path) => readFileSync(path, "utf8");
const requireTokens = (path, tokens) => {
  const source = read(path);
  for (const token of tokens) {
    if (!source.includes(token)) throw new Error(`${path} omits ${token}`);
  }
  return source;
};

requireTokens("crypto_bot_v1_codex_spec.md", [
  "### Phase D5: Operational hardening and data lifecycle", // semantic-naming: historical-reference
  "initial critical free-space watermark is 5 GiB",
  "Ed25519-authenticated terminal",
  "B2 market-data and C6 sandbox verdicts remain independent", // semantic-naming: historical-reference
]);

const compose = requireTokens("docker-compose.yml", [
  "BACKUP_REMOTE_HOST_PATH",
  "BACKUP_DATABASE_FILESYSTEM: /verify/postgres",
  "BACKUP_MARKET_DATA_FILESYSTEM: /verify/market-data",
  "BACKUP_LOCAL_FILESYSTEM: /verify/local-backup",
  "BACKUP_RESTORE_MARKET_DATA_HOST_PATH",
  "BACKUP_REQUIRE_MARKET_RECOVERY",
  "RECORDER_CRITICAL_FREE_BYTES",
  "RECORDER_PRESSURE_SAMPLE_INTERVAL",
  "--storage.tsdb.retention.time=${PROMETHEUS_RETENTION_TIME:-15d}",
  "--storage.tsdb.retention.size=${PROMETHEUS_RETENTION_SIZE:-20GB}",
  "operational-readiness-observer:",
  "POSTGRES_OPERATIONAL_READINESS_USER",
  "AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE",
]);
if (compose.includes("backup_data:")) {
  throw new Error(
    "Compose-managed backup_data volume survived operational-readiness hardening",
  );
}

requireTokens(
  "internal/storage/postgres/migrations/000027_v1d_d5_operational_readiness.sql",
  [
    "v1d_storage_pressure_state", // semantic-naming: historical-reference
    "v1d_storage_pressure_events", // semantic-naming: historical-reference
    "'migration-bootstrap'",
    "storage.pressure.critical",
  ],
);
requireTokens("internal/bootstrap/recorder_role_runtime.go", [
  "observeStoragePressure",
  "pressureTicker",
]);
requireTokens("internal/bootstrap/recorder_pressure.go", [
  "flushPending(logger, true)",
  "recorder_storage_pressure_critical",
  "NewOperationalReadinessStoragePressureStore",
]);
requireTokens(
  "internal/storage/postgres/operational_readiness_pressure_gates.go",
  [
    "source_instance<>'migration-bootstrap'",
    "level='NORMAL'",
    "interval '2 minutes'",
  ],
);
requireTokens("internal/storage/postgres/owner_console_shadow.go", [
  "breaker_kind='disk_failure'",
  "level='NORMAL'",
  "source_instance<>'migration-bootstrap'",
]);
requireTokens("internal/bootstrap/offline_worker_pipeline.go", [
  "orderedOfflineWorkers{metricWorker, lifecycleWorker, reportWorker, worker, campaignWorker}",
]);
for (const path of [
  "internal/storage/postgres/owner_control_console_access.go",
  "internal/storage/postgres/owner_control_console_actions.go",
  "internal/storage/postgres/operational_evidence_report_worker.go",
]) {
  requireTokens(path, ["operationalReadinessHeavyWorkAllowed"]);
}
requireTokens("internal/backup/location.go", [
  "/proc/self/mountinfo",
  "backup_destination_filesystem_not_independent",
  "backup_destination_not_independent_mount",
]);
requireTokens("internal/backup/restore_evidence.go", [
  "axiom.backup.restore-evidence.v1",
  "O_EXCL",
  "AuthenticationTag",
]);
requireTokens("internal/backup/market_recovery.go", [
  "VerifyMarketRecovery",
  "market_recovery_checksum_failed",
  "market_recovery_path_ambiguous",
  "InventoryHash",
]);
requireTokens("internal/qualification/operationalreadiness/runner.go", [
  "validatePreflight",
  "sampleFailureReasons",
  "sample_chain_invalid",
  "fault_schedule_incomplete",
  "StateSmokePassed",
  "ed25519.Sign",
]);
requireTokens("internal/qualification/operationalreadiness/files.go", [
  "events[index].RunID != runID",
  "operational_readiness_fault_evidence_run_mismatch",
]);
requireTokens("internal/qualification/operationalreadiness/live_observer.go", [
  "DatabaseEvidenceHash",
  "RuntimeEvidenceHash",
  "DrillEvidenceHash",
  "WriteLiveSample",
]);
requireTokens(
  "internal/qualification/operationalreadiness/postgres_live_observer.go",
  [
    "AccessMode: pgx.ReadOnly",
    "AND path IN ('/api/v3/order','/v5/order/create')",
    "WHERE environment NOT IN ('testnet','demo')",
  ],
);
requireTokens("cmd/operational-readiness-observer/main.go", [
  "PostgresTelemetrySource",
  "HTTPRuntimeTelemetrySource",
  "postgres_operational_readiness_password",
]);
requireTokens("cmd/operational-readiness/main.go", [
  "AXIOM_OPERATIONAL_READINESS_ENABLED",
  "AXIOM_OPERATIONAL_READINESS_PREFLIGHT_CHECK",
  "AXIOM_OPERATIONAL_READINESS_PREFLIGHT_PROFILE",
  "PreflightProfileViennaRehearsal",
  "formal build identity mismatch",
  "AXIOM_OPERATIONAL_READINESS_TEST_MANIFEST_FILE",
]);
requireTokens("internal/qualification/operationalreadiness/checks.go", [
  "axiom.operational_readiness.preflight-report.v2",
  'PreflightProfileStrict PreflightProfile = "strict"',
  'PreflightProfileViennaRehearsal PreflightProfile = "vienna_rehearsal"',
  '"route_clock_threshold_exceeded"',
  "FormalClockStarted:",
  "Qualified:",
  "MarketDataRecoveryPassed",
  "preflight_stale",
  "sampleFailureReasons",
]);

const schedule = read(
  "deploy/config/operational-readiness-fault-schedule-v1.json",
);
const scheduleContract = JSON.parse(schedule);
const scheduleHash = createHash("sha256").update(schedule).digest("hex");
const manifest = JSON.parse(
  read("deploy/config/operational-readiness-test-manifest-v1.json"),
);
const exactSet = (actual, expected) =>
  actual.length === expected.length &&
  expected.every((value) => actual.includes(value));
if (
  scheduleContract.schema_version !==
    "axiom.operational_readiness.fault-schedule.v1" ||
  manifest.schema_version !==
    "axiom.operational_readiness.test-manifest.v1" ||
  manifest.duration_seconds !== 604800 ||
  manifest.sample_interval_seconds !== 60 ||
  manifest.clock_offset_threshold_ms !== 100 ||
  manifest.fault_schedule_sha256 !== scheduleHash ||
  !exactSet(manifest.independent_verdicts_not_replaced, [
    "coherent market-data qualification",
    "sandbox order and reconciliation qualification",
  ])
) {
  throw new Error(
    "operational-readiness manifest duration or fault schedule identity drifted",
  );
}

for (const path of [
  "deploy/config/operational-readiness-run.example.json",
  "deploy/config/operational-readiness-preflight.example.json",
  "deploy/config/operational-readiness-sample.example.json",
  "deploy/config/operational-readiness-fault-evidence.example.json",
  "deploy/config/operational-readiness-drill-observation.example.json",
]) {
  const example = read(path);
  JSON.parse(example);
  const failingState =
    example.includes("false") || example.includes('"state": "failed"');
  const incompleteIdentity =
    example.includes("CHANGE_ME") || path.includes("sample");
  if (!failingState || !incompleteIdentity) {
    throw new Error(`${path} is not a fail-closed operator template`);
  }
}

const preflightReportExample = JSON.parse(
  read("deploy/config/operational-readiness-preflight-report.example.json"),
);
if (
  preflightReportExample.ready !== false ||
  preflightReportExample.formal_clock_started !== false ||
  preflightReportExample.qualified !== false ||
  preflightReportExample.profile !== "strict" ||
  !Array.isArray(preflightReportExample.warnings)
) {
  throw new Error("operational-readiness preflight report example can qualify");
}

for (const path of [
  "internal/qualification/operationalreadiness/model.go",
  "internal/qualification/operationalreadiness/runner.go",
  "internal/qualification/operationalreadiness/checks.go",
  "cmd/operational-readiness/main.go",
  "internal/qualification/operationalreadiness/live_observer.go",
  "internal/qualification/operationalreadiness/postgres_live_observer.go",
  "internal/qualification/operationalreadiness/http_live_observer.go",
  "cmd/operational-readiness-observer/main.go",
]) {
  const source = read(path);
  for (const forbidden of [
    "axiom/internal/exchanges",
    "CreateOrder",
    "PlaceOrder",
    "SubmitOrder",
    "withdraw",
    "transfer",
  ]) {
    if (source.includes(forbidden)) {
      throw new Error(
        `${path} crosses the operational-readiness observation-only boundary with ${forbidden}`,
      );
    }
  }
}

requireTokens("docs/requirements/v1d-d5-traceability.md", [
  "AX-V1D-D05-001",
  "AX-V1D-D05-010",
]);
requireTokens("docs/operations/d5-readiness.md", [
  "make operational-readiness-preflight-check",
  "make operational-readiness-preflight-vienna-rehearsal",
  "Do not restart, resume, reset the clock",
  "formal_clock_started=false",
  "qualified=false",
]);

requireTokens("Makefile", [
  "operational-readiness-preflight-vienna-rehearsal",
  "AXIOM_OPERATIONAL_READINESS_PREFLIGHT_PROFILE=strict",
]);

console.log("Operational hardening, lifecycle, and readiness boundary passed");
