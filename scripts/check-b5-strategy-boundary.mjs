import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [b5-strategy-boundary] ${message}\n`);
};

const root = "internal/strategies/crossarb";
const files = fs
  .readdirSync(root)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => path.join(root, name));
const production = files
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  "BuyBinanceSellBybit",
  "BuyBybitSellBinance",
  "func Evaluate(",
  "func EvaluateUniverse(",
  "func ValidateCoherentBooks(",
  "func ApproveCandidate(",
  "func ClaimCandidate(",
  "func Simulate(",
  "OutcomeBothFilled",
  "OutcomeBuyOnly",
  "OutcomeSellOnly",
  "OutcomePartialBuy",
  "OutcomePartialSell",
  "OutcomePartialBoth",
  "OutcomeBothMissed",
  "OutcomeNegativeBeforeArrival",
  "OutcomeDelayedUnknown",
  "NewCrossExchangeJournal",
  "RebalancingNeed",
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
]) {
  if (production.includes(forbidden)) {
    fail(`strategy capability leak ${forbidden}`);
  }
}
if (
  !production.includes("It cannot submit authenticated or production orders.")
) {
  fail("public-data-only simulation boundary is not explicit");
}

const configuration = JSON.parse(
  fs.readFileSync("deploy/config/platform-shadow-v1b.json", "utf8"),
);
const crossExchange = configuration.cross_exchange ?? {};
const parameters = crossExchange.parameters ?? [];
if (
  configuration.schema_version !== "axiom.config.v1b.4" ||
  configuration.product !== "spot" ||
  configuration.safety?.fail_closed !== true ||
  configuration.safety?.risk_initial_state !== "PAUSED" ||
  crossExchange.strategy_version !== "cross-exchange.v1b.1" ||
  crossExchange.settlement_asset !== "USDT" ||
  crossExchange.dispatch_mode !== "concurrent" ||
  crossExchange.pricing_model !== "cross-exchange-closed-cycle.v1" ||
  crossExchange.claim_model !== "atomic-multi-resource.v1" ||
  crossExchange.rebalancing_mode !== "advisory_only" ||
  JSON.stringify(crossExchange.instruments) !==
    JSON.stringify(["BTCUSDT", "ETHUSDT"]) ||
  JSON.stringify(crossExchange.exchanges) !==
    JSON.stringify(["binance", "bybit"]) ||
  JSON.stringify(crossExchange.directions) !==
    JSON.stringify(["buy_binance_sell_bybit", "buy_bybit_sell_binance"]) ||
  parameters.length !== 20 ||
  configuration.secrets != null
) {
  fail("reviewed B5 configuration is incomplete or unsafe");
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
  "internal/storage/postgres/migrations/000017_b5_cross_exchange_arbitrage.sql",
  "utf8",
);
for (const invariant of [
  "cross_exchange_candidates",
  "cross_exchange_candidate_members",
  "cross_exchange_candidate_legs",
  "cross_exchange_inventory_snapshots",
  "claim_b5_resources",
  "settle_b5_claim_group",
  "close_b5_claim_group",
  "cross_exchange_candidate_member_evidence_mismatch",
  "cross_exchange_candidate_inventory_mismatch",
  "cross_exchange_simulation_outcomes",
  "cross_exchange_rebalancing_needs",
  "cross_exchange_journal_links",
  "advisory_only boolean NOT NULL CHECK (advisory_only)",
  "SECURITY DEFINER SET search_path = pg_catalog, public",
  "REVOKE EXECUTE",
]) {
  if (!migration.includes(invariant)) {
    fail(`missing persistence invariant ${invariant}`);
  }
}

for (const artifact of [
  "internal/storage/postgres/queries/b5_cross_exchange.sql",
  "internal/storage/postgres/b5_repository.go",
  "internal/storage/postgres/b5_cross_exchange_integration_test.go",
  "docs/strategies/cross-exchange-arbitrage.md",
  "docs/releases/evidence/b5-local-validation.md",
]) {
  if (!fs.existsSync(artifact)) fail(`missing B5 artifact ${artifact}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `B5 coherent views, closed-cycle economics, claims, persistence, and runtime boundary passed (${files.length} Go files, ${parameters.length} parameters)\n`,
);
