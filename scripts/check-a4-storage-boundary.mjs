import fs from "node:fs";
import path from "node:path";

const migrationDirectory = "internal/storage/postgres/migrations";
const migrationNames = fs
  .readdirSync(migrationDirectory)
  .filter((name) => name.endsWith(".sql"))
  .sort();
const failures = [];

function fail(message) {
  failures.push(message);
  process.stderr.write(`ERROR [a4-storage-boundary] ${message}\n`);
}

if (migrationNames.length < 3)
  fail("expected at least three reviewed migrations");
for (let index = 0; index < migrationNames.length; index += 1) {
  const expected = String(index + 1).padStart(6, "0") + "_";
  if (!migrationNames[index].startsWith(expected)) {
    fail(`non-contiguous migration ${migrationNames[index]}`);
  }
}

const migrations = migrationNames
  .map((name) => fs.readFileSync(path.join(migrationDirectory, name), "utf8"))
  .join("\n")
  .toLowerCase();

for (const forbidden of [
  "double precision",
  "real ",
  "float4",
  "float8",
  "drop database",
  "redis",
]) {
  if (migrations.includes(forbidden))
    fail(`forbidden SQL token ${forbidden.trim()}`);
}

const requiredTables = [
  "users",
  "audit_events",
  "exchanges",
  "exchange_capabilities",
  "instruments",
  "instrument_metadata_versions",
  "assets",
  "asset_screening_versions",
  "market_data_segments",
  "dataset_manifests",
  "dataset_gaps",
  "data_quality_events",
  "strategy_definitions",
  "strategy_versions",
  "strategy_parameters",
  "portfolios",
  "strategy_portfolios",
  "virtual_accounts",
  "virtual_balances",
  "account_snapshots",
  "reservations",
  "opportunities",
  "decisions",
  "decision_inputs",
  "experiment_registrations",
  "risk_evaluations",
  "orders",
  "order_attempts",
  "order_events",
  "execution_plans",
  "execution_plan_legs",
  "recovery_attempts",
  "fills",
  "journal_transactions",
  "ledger_entries",
  "positions",
  "runs",
  "run_checkpoints",
  "model_versions",
  "incidents",
  "alerts",
  "configuration_versions",
  "command_requests",
  "inbox_events",
  "outbox_events",
  "execution_leases",
  "reconciliation_cases",
  "reconciliation_suspense",
  "jobs",
];
for (const table of requiredTables) {
  if (!migrations.includes(`create table ${table} `)) {
    fail(`required table ${table} is absent`);
  }
}

for (const invariant of [
  "financial_amount as numeric(38,18)",
  "enforce_journal_asset_balance",
  "enforce_journal_reversal",
  "reject_sealed_journal_line",
  "update journal_transactions set sealed = true",
  "security definer set search_path = pg_catalog, public",
  "enforce_order_transition",
  "immutable_order_identity",
  "enforce_reservation_transition",
  "immutable_reservation_identity",
  "enforce_fencing_increase",
  "invalid_run_transition",
  "enforce_job_transition",
  "protect_command_request",
  "protect_outbox_event",
  "enforce_consumer_cursor",
  "reject_immutable_mutation",
  "protect_strategy_version",
  "protect_model_version",
  "enforce_asset_screening_sequence",
  "protect_market_data_segment",
  "protect_dataset_manifest",
  "enforce_dataset_gap_nonoverlap",
]) {
  if (!migrations.includes(invariant)) fail(`missing invariant ${invariant}`);
}

const backupCommand = fs.readFileSync("cmd/storage-backup/main.go", "utf8");
for (const invariant of [
  'exec.CommandContext(ctx, "pg_restore", "--list")',
  "backup.QuarantineArtifact",
  "backup.PruneArtifacts",
  "targetCleanQuery",
  "restoredIntegrityQuery",
  "verifyRestoredDatabase",
]) {
  if (!backupCommand.includes(invariant))
    fail(`missing backup command invariant ${invariant}`);
}

const sqlc = fs.readFileSync("sqlc.yaml", "utf8");
if (
  !sqlc.includes("sql_package: pgx/v5") ||
  !sqlc.includes("internal/storage/postgres/generated")
) {
  fail("sqlc must target pgx/v5 and the generated storage package");
}

const queryFiles = fs
  .readdirSync("internal/storage/postgres/queries")
  .filter((name) => name.endsWith(".sql"));
for (const name of queryFiles) {
  const source = fs.readFileSync(
    path.join("internal/storage/postgres/queries", name),
    "utf8",
  );
  if (!source.includes("-- name:"))
    fail(`sqlc query file ${name} has no named query`);
}

const segmentSchema = fs.readFileSync(
  "internal/storage/segments/schema.go",
  "utf8",
);
for (const invariant of [
  'WireSchemaVersion = "market-wire.v1"',
  'CanonicalSchemaVersion = "market-canonical.v1"',
  'PhysicalType: "FIXED_LEN_BYTE_ARRAY(32)"',
  'Name: "recorded_logical_time"',
  "ValidateWireRow",
  "ValidateCanonicalRow",
]) {
  if (!segmentSchema.includes(invariant))
    fail(`missing segment schema invariant ${invariant}`);
}

const parquetCodec = [
  "content_hash.go",
  "parquet_writer.go",
  "parquet_reader.go",
]
  .map((file) => fs.readFileSync(`internal/storage/segments/${file}`, "utf8"))
  .join("\n");
for (const invariant of [
  "parquet.NewGenericWriter",
  "parquet.NewGenericReader",
  "parquet.OpenFile",
  "zstd.SpeedDefault",
  "Concurrency: 1",
  '"zstd-level-3"',
  "format.Zstd",
  "HashCanonicalRows",
  "segment_ordered_content_mismatch",
]) {
  if (!parquetCodec.includes(invariant))
    fail(`missing Parquet/Zstd codec invariant ${invariant}`);
}

const datasetReader = fs.readFileSync(
  "internal/storage/segments/reader.go",
  "utf8",
);
for (const invariant of [
  'dataset.OrderingVersion != "dataset-order.v1"',
  "record.RecordedLogicalTime < priorLogical",
  "seenOrdinals[record.IngestOrdinal]",
  "reader.parsers[manifest.Spec.ParserVersion]",
  "reader.normalizers[manifest.Spec.NormalizationVersion]",
  "ordinalInGap(record.IngestOrdinal, dataset.Gaps)",
]) {
  if (!datasetReader.includes(invariant))
    fail(`missing deterministic dataset reader invariant ${invariant}`);
}

const segmentFinalizer = ["finalizer.go", "quarantine.go"]
  .map((file) => fs.readFileSync(`internal/storage/segments/${file}`, "utf8"))
  .join("\n");
for (const invariant of [
  "orderedHash != spec.OrderedContentHash",
  'fmt.Errorf("segment_ordered_content_mismatch")',
  "QuarantineInvalidProofs",
  "!pathInfo.Mode().IsRegular()",
]) {
  if (!segmentFinalizer.includes(invariant))
    fail(`missing ordered-content finalization invariant ${invariant}`);
}
for (const forbidden of ['PhysicalType: "FLOAT"', 'PhysicalType: "DOUBLE"']) {
  if (segmentSchema.includes(forbidden))
    fail(`binary floating-point segment column ${forbidden}`);
}

const capacity = fs.readFileSync(
  "internal/storage/segments/capacity.go",
  "utf8",
);
for (const [label, invariant] of [
  ["MinimumHotRetentionDays = 30", /MinimumHotRetentionDays\s*=\s*30/],
  ["MinimumHeadroomPercent = 30", /MinimumHeadroomPercent\s*=\s*30/],
  [
    "MinimumFreeBytes = 10 * 1024 * 1024 * 1024",
    /MinimumFreeBytes\s*=\s*10\s*\*\s*1024\s*\*\s*1024\s*\*\s*1024/,
  ],
  [
    "MaximumSegmentBytes = 256 * 1024 * 1024",
    /MaximumSegmentBytes\s*=\s*256\s*\*\s*1024\s*\*\s*1024/,
  ],
  [
    "MaximumSegmentDuration = time.Hour",
    /MaximumSegmentDuration\s*=\s*time\.Hour/,
  ],
]) {
  if (!invariant.test(capacity)) fail(`missing capacity invariant ${label}`);
}

const segmentRetention = fs.readFileSync(
  "internal/storage/segments/retention.go",
  "utf8",
);
for (const invariant of [
  "MinimumHotRetention = 30 * 24 * time.Hour",
  "retention < MinimumHotRetention",
  'record.Manifest.Compression != "zstd"',
]) {
  if (!segmentRetention.includes(invariant))
    fail(`missing segment retention invariant ${invariant}`);
}

const backupRetention = fs.readFileSync("internal/backup/retention.go", "utf8");
for (const invariant of [
  "MinimumRetainedGenerations = 14",
  "RestoreArtifact(root, manifest, io.Discard, key)",
  '".manifest.json.deleting"',
]) {
  if (!backupRetention.includes(invariant))
    fail(`missing backup retention invariant ${invariant}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `A4 storage boundary passed (${migrationNames.length} migrations, ${requiredTables.length} required tables, Parquet/Zstd codec, integer capacity policy, authenticated 14-generation backup retention)\n`,
);
