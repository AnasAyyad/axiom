import fs from "node:fs";

const read = (path) => fs.readFileSync(path, "utf8");

const requiredFiles = [
  "docs/releases/v1c-pr3-checklist.md",
  "docs/releases/v1c-pr3-readiness.md",
  "docs/releases/evidence/v1c-pr3-local-validation.md",
  "docs/requirements/v1c-pr3-traceability.md",
  "docs/requirements/v1c-pr3-source-coverage.md",
  "internal/api/console/sandbox.go",
  "internal/api/console/sandbox_commands.go",
  "internal/qualification/c6/runner.go",
  "cmd/c6-chaos/main.go",
  "internal/storage/postgres/migrations/000024_v1c_c6_console_qualification.sql",
  "web/src/app/SandboxOperationsPage.tsx",
];

for (const path of requiredFiles) {
  if (!fs.existsSync(path)) {
    throw new Error(`V1C PR3 required file is absent: ${path}`);
  }
}

const requireText = (source, values, label) => {
  for (const value of values) {
    if (!source.includes(value)) {
      throw new Error(`V1C PR3 ${label} omits: ${value}`);
    }
  }
};

const openapi = read("api/openapi.yaml");
requireText(
  openapi,
  [
    "/api/v1/sandbox/overview:",
    "/api/v1/sandbox/orders:",
    "/api/v1/sandbox/reconciliations:",
    "/api/v1/sandbox/qualification:",
    "/api/v1/sandbox/authorizations:",
    "/api/v1/sandbox/sessions/{id}/arms:",
    "/api/v1/sandbox/arms/{id}/revoke:",
    "/api/v1/sandbox/accounts/{id}/unlock:",
    "/api/v1/sandbox/orders/{id}/cancel:",
    "/api/v1/sandbox/orders/{id}/query:",
    "/api/v1/sandbox/accounts/{id}/reconcile:",
  ],
  "OpenAPI",
);

const admission = read(
  "internal/storage/postgres/v1c_console_order_admission.go",
);
requireText(
  admission,
  [
    "CanaryAdmission(",
    "BuildCanaryPlan(",
    "ApprovePlan(",
    "EnvironmentBinanceSpotTestnet",
    "EnvironmentBybitDemo",
    "SideBuy",
  ],
  "order admission",
);

const c6Model = read("internal/qualification/c6/model.go");
const c6Runner = read("internal/qualification/c6/runner.go");
requireText(
  c6Model + c6Runner,
  [
    "FormalDuration = 72 * time.Hour",
    "MaximumOrderMicrounits: 10_000_000",
    "MaximumDailyMicrounits: 50_000_000",
    "MaximumOpenPerAccount:  1",
    "MaximumOpenGlobal: 2",
    "ArmDurationSeconds: 900",
    "ProfitabilityEvidence:   false",
    '"production_target"',
  ],
  "qualification contract",
);

const runnerMain = read("cmd/c6-soak/main.go");
requireText(
  runnerMain,
  [
    'os.Getenv("AXIOM_C6_SOAK_ENABLED") != "1"',
    'os.Getenv("AXIOM_C6_SOAK_MODE") != "formal"',
    "Duration: c6.FormalDuration",
    'BuildHash:         os.Getenv("AXIOM_C6_BUILD_HASH")',
    'ImageHash:         os.Getenv("AXIOM_C6_IMAGE_HASH")',
  ],
  "manual runner",
);

const apiSources = [
  "internal/api/console/sandbox.go",
  "internal/api/console/sandbox_commands.go",
  "internal/storage/postgres/v1c_console_order_admission.go",
  "cmd/c6-soak/main.go",
  "cmd/c6-chaos/main.go",
]
  .map(read)
  .join("\n");
for (const forbidden of [
  "internal/exchanges/binance",
  "internal/exchanges/bybit",
  "APISecret",
  "APIKey",
  "http.Client",
]) {
  if (apiSources.includes(forbidden)) {
    throw new Error(
      `V1C PR3 API/observer boundary contains forbidden token: ${forbidden}`,
    );
  }
}

const ui = [
  "web/src/app/AppShell.tsx",
  "web/src/app/SandboxOperationsPage.tsx",
  "web/src/app/SandboxControlsView.tsx",
]
  .map(read)
  .join("\n");
requireText(
  ui,
  ["REAL TRADING DISABLED", "BINANCE SPOT TESTNET", "BYBIT DEMO"],
  "persistent UI safety labels",
);

const migration = read(
  "internal/storage/postgres/migrations/000024_v1c_c6_console_qualification.sql",
);
requireText(
  migration,
  [
    "required_duration_seconds=259200",
    "CHECK (NOT profitability_evidence)",
    "protect_v1c_c6_qualification_run",
    "v1c_engine_runtime_events_immutable",
    "v1c_c6_chaos_events_immutable",
  ],
  "immutable evidence schema",
);

const makefile = read("Makefile");
requireText(
  makefile,
  [
    "c6-api-qualify:",
    "c6-frontend-qualify:",
    "c6-security-qualify:",
    "c6-chaos-qualify:",
    "c6-chaos-record:",
    "c6-soak-smoke:",
    "c6-soak:",
    "v1c-pr3-local-qualify:",
  ],
  "qualification targets",
);

console.log("V1C PR3 C6 source boundary passed");
