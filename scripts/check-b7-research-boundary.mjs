import fs from "node:fs";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [b7-research-boundary] ${message}\n`);
};

const researchFiles = [
  "internal/research/b7_registration.go",
  "internal/research/b7_statistics.go",
  "internal/research/b7_validation.go",
  "internal/research/b7_comparison.go",
  "internal/research/b7_promotion.go",
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
  "b7_experiment_preregistrations",
  "b7_validation_suites",
  "b7_champion_challenger_reports",
  "strategy_maturity_states",
  "strategy_maturity_commands",
  "strategy_maturity_events",
  "validate_b7_champion_challenger",
  "CREATE OR REPLACE FUNCTION apply_b7_maturity_promotion(",
  "SECURITY DEFINER",
  "SET search_path = pg_catalog, public",
  "permission_id = 'research.promote'",
  "reauthenticated_at >= p_command_time - interval '10 minutes'",
  "p_command_time >= statement_timestamp() - interval '1 minute'",
  "p_expected_revision",
  "p_idempotency_key",
  "REVOKE ALL ON FUNCTION apply_b7_maturity_promotion(",
  "b7_preregistrations_immutable",
  "b7_validation_suites_immutable",
  "strategy_maturity_events_immutable",
]) {
  if (!migration.includes(required)) {
    fail(`missing persistence invariant ${required}`);
  }
}

const roles = fs.readFileSync("internal/storage/postgres/roles.go", "utf8");
for (const required of [
  "apply_b7_maturity_promotion",
  "b7_experiment_preregistrations",
  "b7_validation_suites",
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
  "internal/storage/postgres/queries/b7_research_promotion.sql",
  "internal/storage/postgres/b7_repository.go",
  "internal/storage/postgres/b7_research_promotion_integration_test.go",
  "research/src/axiom_research/validation.py",
  "research/tests/test_validation.py",
  "research/testdata/b7_statistics_golden.json",
  "docs/research/multi-strategy-validation.md",
  "docs/releases/evidence/b7-local-validation.md",
]) {
  if (!fs.existsSync(artifact)) fail(`missing B7 artifact ${artifact}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `B7 preregistration, independent statistics, explicit promotion, and no-execution boundary passed (${researchFiles.length} Go files)\n`,
);
