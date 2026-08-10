import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [inventory-rebalancing-boundary] ${message}\n`);
};

const root = "internal/rebalancing";
const files = fs
  .readdirSync(root)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => path.join(root, name));
const production = files
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  "func NewGraph(",
  "func (graph *Graph) Optimize(",
  "func SealEdge(",
  "func ConfigurationFromReviewed(",
  "NaturalReverseMethod",
  "GraphRouteMethod",
  "CostBreakdown",
  "Provenance",
  "ManualChecklist",
  "AdvisoryOnly",
]) {
  if (!production.includes(required)) fail(`missing invariant ${required}`);
}

for (const forbidden of [
  '"axiom/internal/storage',
  '"axiom/internal/exchanges',
  '"net/http"',
  '"os/exec"',
  "float32",
  "float64",
  "ExecuteTransfer",
  "SubmitTransfer",
  "InitiateTransfer",
  "ExecuteWithdrawal",
  "SubmitWithdrawal",
  "InitiateWithdrawal",
  "WithdrawalClient",
  "TransferClient",
]) {
  if (production.includes(forbidden)) {
    fail(`optimizer capability leak ${forbidden}`);
  }
}
if (
  !production.includes(
    "It has no external asset-movement transport or credential",
  )
) {
  fail("advisory-only transport boundary is not explicit");
}

const configuration = JSON.parse(
  fs.readFileSync("deploy/config/platform-research.json", "utf8"),
);
const rebalancing = configuration.rebalancing ?? {};
const parameters = rebalancing.parameters ?? [];
if (
  configuration.schema_version !== "axiom.configuration@1.5.0" ||
  configuration.product !== "spot" ||
  configuration.safety?.fail_closed !== true ||
  configuration.safety?.risk_initial_state !== "PAUSED" ||
  rebalancing.optimizer_version !== "inventory-rebalancing@1.0.0" ||
  rebalancing.fact_schema_version !== "rebalancing-fact.v1" ||
  rebalancing.cost_model_version !== "rebalancing-cost.v1" ||
  rebalancing.mode !== "advisory_only" ||
  rebalancing.natural_reversal_policy !== "prefer_eligible_before_transfer" ||
  JSON.stringify(rebalancing.approved_assets) !==
    JSON.stringify(["BTC", "ETH", "USDT"]) ||
  JSON.stringify(rebalancing.exchanges) !==
    JSON.stringify(["binance", "bybit"]) ||
  parameters.length !== 12 ||
  (configuration.secrets != null && configuration.secrets.length !== 0)
) {
  fail("reviewed inventory-rebalancing configuration is incomplete or unsafe");
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
  "internal/storage/postgres/migrations/000018_b6_advisory_rebalancing.sql",
  "utf8",
);
for (const invariant of [
  "rebalancing_fact_sets",
  "rebalancing_route_facts",
  "rebalancing_recommendations",
  "rebalancing_recommendation_steps",
  "rebalancing_checklist_steps",
  "rebalancing_selected_fact_ineligible",
  "rebalancing_natural_reverse_mismatch",
  "rebalancing_graph_route_mismatch",
  "advisory_only boolean NOT NULL CHECK (advisory_only)",
  "SECURITY DEFINER SET search_path = pg_catalog, public",
  "rebalancing_recommendations_immutable",
]) {
  if (!migration.includes(invariant)) {
    fail(`missing persistence invariant ${invariant}`);
  }
}

const surface = [
  "api/openapi.yaml",
  ...walkFiles("internal/api", [".go"]),
  ...walkFiles("web/src", [".ts", ".tsx", ".js", ".jsx"]),
  "deploy/config/platform-research.json",
]
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");
for (const forbidden of [
  /execute[_ -]?(transfer|withdrawal)/i,
  /submit[_ -]?(transfer|withdrawal)/i,
  /initiate[_ -]?(transfer|withdrawal)/i,
  /(transfer|withdrawal)[_ -]?execution[_ -]?enabled/i,
  /"(transfers|withdrawals)_enabled"\s*:\s*true/i,
]) {
  if (forbidden.test(surface)) {
    fail(`API/UI/config execution surface ${forbidden}`);
  }
}

for (const artifact of [
  "internal/storage/postgres/queries/inventory_rebalancing_rebalancing.sql",
  "internal/storage/postgres/inventory_rebalancing_repository.go",
  "internal/storage/postgres/inventory_rebalancing_rebalancing_integration_test.go",
  "docs/strategies/advisory-rebalancing.md",
  "docs/releases/evidence/b6-local-validation.md",
]) {
  if (!fs.existsSync(artifact)) {
    fail(`missing inventory-rebalancing artifact ${artifact}`);
  }
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `Inventory-rebalancing deterministic advisory optimizer and no-execution boundary passed (${files.length} Go files, ${parameters.length} parameters)\n`,
);

function walkFiles(directory, extensions) {
  const result = [];
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const full = path.join(directory, entry.name);
    if (entry.isDirectory()) result.push(...walkFiles(full, extensions));
    else if (extensions.some((extension) => entry.name.endsWith(extension))) {
      result.push(full);
    }
  }
  return result.sort();
}
