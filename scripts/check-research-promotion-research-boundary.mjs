import fs from "node:fs";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [research-promotion-boundary] ${message}\n`);
};

const researchFiles = [
  "internal/research/research_promotion_registration.go",
  "internal/research/research_promotion_statistics.go",
  "internal/research/research_promotion_validation.go",
  "internal/research/research_promotion_comparison.go",
  "internal/research/research_promotion_promotion.go",
];
const research = researchFiles
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  'ExperimentRegistrationContract = "research-preregistration.v1"',
  'ValidationSuiteContract = "multi-strategy-validation.v1"',
  "func RegisterExperiment(",
  "func AdjustMultipleTests(",
  "func AnalyzeSharpe(",
  "func BuildValidationSuite(",
  "func BuildChampionChallengerReport(",
  "func ValidMaturityTransition(",
  'authentication.RequirePermission(principal, "research.promote")',
  "authentication.RecentReauthentication",
  "request.ExpectedRevision > 0",
  "validTarget := request.Target == MaturityBacktestValidated",
  "request.Target == MaturityRejected",
]) {
  if (!research.includes(required))
    fail(`missing research invariant ${required}`);
}

for (const forbidden of [
  '"axiom/internal/accounting"',
  '"axiom/internal/execution"',
  '"axiom/internal/exchanges"',
  '"axiom/internal/portfolio"',
  '"axiom/internal/storage"',
  '"net/http"',
  '"os/exec"',
]) {
  if (research.includes(forbidden))
    fail(`research capability leak ${forbidden}`);
}

const migration = fs.readFileSync(
  "internal/storage/postgres/migrations/000019_b7_research_promotion.sql",
  "utf8",
);
for (const required of [
  "b7_experiment_preregistrations", // semantic-naming: historical-reference
  "b7_validation_suites", // semantic-naming: historical-reference
  "b7_champion_challenger_reports", // semantic-naming: historical-reference
  "strategy_maturity_states",
  "strategy_maturity_commands",
  "strategy_maturity_events",
  "validate_b7_champion_challenger", // semantic-naming: historical-reference
  "CREATE OR REPLACE FUNCTION apply_b7_maturity_promotion(", // semantic-naming: historical-reference
  "SECURITY DEFINER",
  "SET search_path = pg_catalog, public",
  "permission_id = 'research.promote'",
  "reauthenticated_at >= p_command_time - interval '10 minutes'",
  "p_command_time >= statement_timestamp() - interval '1 minute'",
  "p_expected_revision",
  "p_idempotency_key",
  "REVOKE ALL ON FUNCTION apply_b7_maturity_promotion(", // semantic-naming: historical-reference
  "b7_preregistrations_immutable", // semantic-naming: historical-reference
  "b7_validation_suites_immutable", // semantic-naming: historical-reference
  "strategy_maturity_events_immutable",
]) {
  if (!migration.includes(required)) {
    fail(`missing persistence invariant ${required}`);
  }
}

const roles = [
  "internal/storage/postgres/roles.go",
  "internal/storage/postgres/role_grants.go",
]
  .map((path) => fs.readFileSync(path, "utf8"))
  .join("\n");
for (const required of [
  "apply_research_promotion_maturity_promotion",
  "research_promotion_experiment_preregistrations",
  "research_promotion_validation_suites",
  "strategy_maturity_states",
]) {
  if (!roles.includes(required)) fail(`missing role boundary ${required}`);
}

const dockerfile = fs
  .readFileSync("deploy/docker/Dockerfile", "utf8")
  .toLowerCase();
for (const forbidden of [
  "copy research",
  "python",
  "uv.lock",
  "pyproject.toml",
]) {
  if (dockerfile.includes(forbidden)) {
    fail(`runtime image includes offline research marker ${forbidden}`);
  }
}

for (const artifact of [
  "internal/storage/postgres/queries/research_promotion_research_promotion.sql",
  "internal/storage/postgres/research_promotion_repository.go",
  "internal/storage/postgres/research_promotion_research_promotion_integration_test.go",
  "research/src/axiom_research/validation.py",
  "research/tests/test_validation.py",
  "research/testdata/research_promotion_statistics_golden.json",
  "docs/research/multi-strategy-validation.md",
  "docs/releases/evidence/b7-local-validation.md",
]) {
  if (!fs.existsSync(artifact)) {
    fail(`missing research-promotion artifact ${artifact}`);
  }
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `Research-promotion preregistration, independent statistics, explicit promotion, and no-execution boundary passed (${researchFiles.length} Go files)\n`,
);
