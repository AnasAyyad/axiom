import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [b3-strategy-boundary] ${message}\n`);
};

const root = "internal/strategies/meanreversion";
const files = fs
  .readdirSync(root)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => path.join(root, name));
const production = files
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  "var _ backtest.Strategy = (*Adapter)(nil)",
  "var _ backtest.Processor = (*OperationalProcessor)(nil)",
  "var _ execution.ExecutionPlanner = (*Planner)(nil)",
  "ReasonEntryAccepted",
  "ReasonExitATRStop",
  "ReasonExitProtectiveZScore",
  "ReasonExitNormalZScore",
  "ReasonExitMaximumHolding",
  "ReasonDangerousRegime",
  "ReasonMissedOrder",
  "ReasonExpiredOrder",
]) {
  if (!production.includes(required)) fail(`missing invariant ${required}`);
}

for (const forbidden of [
  '"axiom/internal/accounting"',
  '"axiom/internal/simulation"',
  '"axiom/internal/storage',
  '"axiom/internal/exchanges/binance"',
  '"axiom/internal/exchanges/bybit"',
  '"net/http"',
  '"os/exec"',
  ".Submit(",
  ".Reserve(",
  ".Post(",
]) {
  if (production.includes(forbidden))
    fail(`strategy capability leak ${forbidden}`);
}

const configuration = JSON.parse(
  fs.readFileSync("deploy/config/platform-shadow-v1b.json", "utf8"),
);
const parameters = configuration.mean_reversion?.parameters ?? [];
if (
  !["axiom.config.v1b.2", "axiom.config.v1b.3"].includes(
    configuration.schema_version,
  ) ||
  configuration.product !== "spot" ||
  configuration.safety?.fail_closed !== true ||
  configuration.safety?.risk_initial_state !== "PAUSED" ||
  configuration.mean_reversion?.strategy_version !== "mean-reversion.v1b.1" ||
  configuration.mean_reversion?.primary_timeframe !== "1h" ||
  configuration.mean_reversion?.higher_timeframe !== "4h" ||
  parameters.length !== 27 ||
  configuration.secrets?.length !== 0
) {
  fail("reviewed B3 configuration is incomplete or unsafe");
}
for (const parameter of parameters) {
  for (const field of [
    "id",
    "description",
    "value",
    "unit",
    "minimum",
    "maximum",
    "rounding",
    "cadence",
    "warm_up",
    "mutability",
    "model_dependencies",
    "algorithm_version",
    "evaluation_timezone",
    "change_behavior",
    "approval_actor",
    "approval_reference",
    "approved_at",
    "change_reason",
  ]) {
    if (parameter[field] === undefined || parameter[field] === "") {
      fail(`parameter ${parameter.id ?? "<unknown>"} lacks ${field}`);
    }
  }
}

const migration = fs.readFileSync(
  "internal/storage/postgres/migrations/000015_b3_mean_reversion.sql",
  "utf8",
);
for (const invariant of [
  "mean_reversion_decisions",
  "coherent_version_vector_hash",
  "portfolio_ownership_account_id",
  "risk_policy_id",
  "mean_reversion_risk_policy_mismatch",
  "correlation_model_id",
  "mean_reversion_model_type_mismatch",
  "mean_reversion_ownership_strategy_mismatch",
  "reject_immutable_mutation",
]) {
  if (!migration.includes(invariant))
    fail(`missing persistence invariant ${invariant}`);
}

const dockerfile = fs.readFileSync("deploy/docker/Dockerfile", "utf8");
for (const forbidden of [
  "COPY research",
  "python",
  "uv.lock",
  "pyproject.toml",
]) {
  if (dockerfile.toLowerCase().includes(forbidden.toLowerCase())) {
    fail(`runtime image includes offline research marker ${forbidden}`);
  }
}
if (!dockerfile.includes("FROM scratch AS runtime")) {
  fail("runtime image is not the reviewed scratch stage");
}

for (const artifact of [
  "research/pyproject.toml",
  "research/uv.lock",
  "research/src/axiom_research/indicators.py",
  "internal/research/mean_reversion.go",
  "internal/storage/postgres/queries/b3_mean_reversion.sql",
  "internal/storage/postgres/b3_repository.go",
  "docs/strategies/mean-reversion.md",
]) {
  if (!fs.existsSync(artifact)) fail(`missing B3 artifact ${artifact}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `B3 strategy, persistence, configuration, and Python runtime boundary passed (${files.length} Go files, ${parameters.length} parameters)\n`,
);
