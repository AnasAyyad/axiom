import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [b4-strategy-boundary] ${message}\n`);
};

const root = "internal/strategies/triangular";
const files = fs
  .readdirSync(root)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => path.join(root, name));
const production = files
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  "CycleUSDTBTCETHUSDT",
  "CycleUSDTETHBTCUSDT",
  "func Evaluate(",
  "func ApproveCandidate(",
  "func ClaimCandidate(",
  "func Simulate(",
  "OutcomeFullSuccess",
  "OutcomePartialCycle",
  "OutcomeMissedLeg",
  "OutcomeNegativeAfterLatency",
  "OutcomeStrandedAsset",
  "NewLifetimeTrackerWithLimit",
  "NewCycleJournal",
]) {
  if (!production.includes(required)) fail(`missing invariant ${required}`);
}

for (const forbidden of [
  '"axiom/internal/storage',
  '"axiom/internal/exchanges/binance"',
  '"axiom/internal/exchanges/bybit"',
  '"net/http"',
  '"os/exec"',
  "float32",
  "float64",
  ".Submit(",
  "private",
  "authenticated",
]) {
  if (production.includes(forbidden))
    fail(`strategy capability leak ${forbidden}`);
}

const conversionRoot = "internal/strategies/arbitrage";
const conversion = fs
  .readdirSync(conversionRoot)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => fs.readFileSync(path.join(conversionRoot, name), "utf8"))
  .join("\n");
for (const required of [
  "VWAPToBuyBase",
  "VWAPToSellBase",
  "PriceTick",
  "QuantityStep",
  "MinimumQuantity",
  "MinimumNotional",
  "FeeAsset",
  "ThirdAssetPriceInQuote",
  "SourceDust",
]) {
  if (!conversion.includes(required))
    fail(`missing conversion invariant ${required}`);
}
for (const forbidden of ["float32", "float64"]) {
  if (conversion.includes(forbidden)) fail(`conversion uses ${forbidden}`);
}

const configuration = JSON.parse(
  fs.readFileSync("deploy/config/platform-shadow-v1b.json", "utf8"),
);
const triangular = configuration.triangular ?? {};
const parameters = triangular.parameters ?? [];
if (
  configuration.schema_version !== "axiom.config.v1b.3" ||
  configuration.product !== "spot" ||
  configuration.safety?.fail_closed !== true ||
  configuration.safety?.risk_initial_state !== "PAUSED" ||
  triangular.strategy_version !== "triangular.v1b.1" ||
  triangular.settlement_asset !== "USDT" ||
  triangular.dispatch_mode !== "sequential" ||
  triangular.claim_model !== "atomic-multi-resource.v1" ||
  JSON.stringify(triangular.cycles) !==
    JSON.stringify(["USDT-BTC-ETH-USDT", "USDT-ETH-BTC-USDT"]) ||
  parameters.length !== 18 ||
  configuration.secrets?.length !== 0
) {
  fail("reviewed B4 configuration is incomplete or unsafe");
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
  "internal/storage/postgres/migrations/000016_b4_triangular_arbitrage.sql",
  "utf8",
);
for (const invariant of [
  "triangular_candidates",
  "triangular_candidate_legs",
  "claim_b4_resources",
  "settle_b4_claim_group",
  "close_b4_claim_group",
  "triangular_candidate_output_chain_mismatch",
  "triangular_candidate_instrument_mismatch",
  "triangular_simulation_outcomes",
  "triangular_opportunity_lifetimes",
  "triangular_journal_links",
  "SECURITY DEFINER SET search_path = pg_catalog, public",
  "REVOKE EXECUTE",
]) {
  if (!migration.includes(invariant)) {
    fail(`missing persistence invariant ${invariant}`);
  }
}

const dockerfile = fs.readFileSync("deploy/docker/Dockerfile", "utf8");
if (!dockerfile.includes("FROM scratch AS runtime")) {
  fail("runtime image is not the reviewed scratch stage");
}
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

for (const artifact of [
  "internal/storage/postgres/queries/b4_triangular.sql",
  "internal/storage/postgres/b4_repository.go",
  "internal/storage/postgres/b4_triangular_integration_test.go",
  "internal/portfolio/claimset.go",
  "docs/strategies/triangular-arbitrage.md",
]) {
  if (!fs.existsSync(artifact)) fail(`missing B4 artifact ${artifact}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `B4 strategy, exact conversion, claims, persistence, and runtime boundary passed (${files.length} Go files, ${parameters.length} parameters)\n`,
);
