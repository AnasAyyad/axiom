import fs from "node:fs";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [multi-exchange-console-boundary] ${message}\n`);
};
const read = (file) => fs.readFileSync(file, "utf8");

const openapi = read("api/openapi.yaml");
for (const required of [
  "/api/v1/exchanges:",
  "/api/v1/opportunities:",
  "/api/v1/opportunities/{id}:",
  "/api/v1/strategies:",
  "/api/v1/inventory:",
  "/api/v1/rebalancing/recommendations:",
  "/api/v1/research/champion-challenger:",
  "/api/v1/replays/{id}/faults:",
  "/api/v1/reports/{id}/exports:",
  "combined_balance: { type: boolean, enum: [false] }",
  "execution_available: { type: boolean, enum: [false] }",
  "simulation_only: { type: boolean, enum: [true] }",
  "opportunity,",
  "strategy,",
  "inventory,",
  "rebalancing,",
  "research,",
]) {
  if (!openapi.includes(required))
    fail(`OpenAPI invariant missing ${required}`);
}

const backendFiles = [
  "internal/api/console/multi_exchange_console_read.go",
  "internal/api/console/multi_exchange_console_commands.go",
  "internal/storage/postgres/multi_exchange_console_exchange_strategy.go",
  "internal/storage/postgres/multi_exchange_console_opportunities.go",
  "internal/storage/postgres/multi_exchange_console_inventory_rebalancing.go",
  "internal/storage/postgres/multi_exchange_console_research_commands.go",
  "internal/storage/postgres/multi_exchange_console_research_read.go",
];
const backend = backendFiles.map(read).join("\n");
for (const required of [
  "func (store *OwnerConsoleStore) Exchanges(",
  "func (store *OwnerConsoleStore) Opportunities(",
  "func (store *OwnerConsoleStore) Inventory(",
  "func (store *OwnerConsoleStore) Rebalancing(",
  "func (store *OwnerConsoleStore) ChampionChallenger(",
  "func (store *OwnerConsoleStore) ScheduleReplayFault(",
  "func (store *OwnerConsoleStore) ExportReport(",
  "CombinedBalance: false",
  "ExecutionAvailable: false",
]) {
  if (!backend.includes(required))
    fail(`backend invariant missing ${required}`);
}
for (const forbidden of [
  /SubmitProductionOrder/i,
  /PlaceRealOrder/i,
  /EnableRealTrading/i,
  /Execute(Transfer|Withdrawal)/i,
  /Initiate(Transfer|Withdrawal)/i,
  /private[_ -]exchange[_ -]client/i,
]) {
  if (forbidden.test(backend)) fail(`backend capability leak ${forbidden}`);
}

const migration = read(
  "internal/storage/postgres/migrations/000020_b8_multi_exchange_console.sql",
);
for (const required of [
  "b8_replay_fault_schedule_states", // semantic-naming: historical-reference
  "b8_replay_fault_schedules", // semantic-naming: historical-reference
  "b8_report_exports", // semantic-naming: historical-reference
  "simulation_only boolean NOT NULL CHECK (simulation_only)",
  "b8_fault_schedules_reference_guard", // semantic-naming: historical-reference
  "b8_fault_schedules_immutable", // semantic-naming: historical-reference
  "b8_report_exports_reference_guard", // semantic-naming: historical-reference
  "b8_report_exports_immutable", // semantic-naming: historical-reference
]) {
  if (!migration.includes(required))
    fail(`migration invariant missing ${required}`);
}

const ui = [
  read("web/src/app/AppShell.tsx"),
  read("web/src/app/SafetyHeader.tsx"),
  read("web/src/app/ExchangeOpportunityPages.tsx"),
  read("web/src/app/RebalancingResearchPages.tsx"),
  read("web/src/app/ReplayFaultScheduler.tsx"),
  read("web/src/app/StrategyInventoryPages.tsx"),
  read("web/src/api/multiExchangeConsoleValidation.ts"),
].join("\n");
for (const required of [
  "REAL-MONEY TRADING IS NOT AVAILABLE",
  "Combined balance:",
  "no transfer controls",
  "simulation-only",
  "combined_balance: z.literal(false)",
  "execution_available: z.literal(false)",
]) {
  if (!ui.toLowerCase().includes(required.toLowerCase())) {
    fail(`UI safety invariant missing ${required}`);
  }
}

for (const artifact of [
  "internal/storage/postgres/queries/multi_exchange_console_console.sql",
  "internal/storage/postgres/multi_exchange_console_console_integration_test.go",
  "web/src/app/MultiExchangePages.test.tsx",
  "web/tests/e2e/owner-console-workflow.spec.ts",
  "docs/adr/0019-b8-generic-multi-exchange-console.md",
  "docs/api/b8-console.md",
  "docs/releases/evidence/b8-local-validation.md",
]) {
  if (!fs.existsSync(artifact)) {
    fail(`Multi-exchange-console artifact missing ${artifact}`);
  }
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `Generic multi-exchange console, simulation-only commands, advisory lock, and no-production-execution boundary passed (${backendFiles.length} backend files)\n`,
);
