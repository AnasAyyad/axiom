import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [a10-strategy-boundary] ${message}\n`);
};

const root = "internal/strategies/trend";
const files = fs
  .readdirSync(root)
  .filter((name) => name.endsWith(".go") && !name.endsWith("_test.go"))
  .map((name) => path.join(root, name));
const production = files
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  "var _ backtest.Strategy = (*Adapter)(nil)",
  "var _ execution.ExecutionPlanner = (*Planner)(nil)",
  "ReasonEntryAccepted",
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
  '"net/http"',
  '"os/exec"',
  ".Submit(",
  ".Reserve(",
  ".Post(",
]) {
  if (production.includes(forbidden))
    fail(`strategy capability leak ${forbidden}`);
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
  "docs/strategies/trend.md",
]) {
  if (!fs.existsSync(artifact)) fail(`missing A10 artifact ${artifact}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `A10 strategy capability and Python runtime boundary passed (${files.length} Go files)\n`,
);
