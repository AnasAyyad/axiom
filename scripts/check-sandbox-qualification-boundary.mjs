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
  "internal/api/console/sandbox_authorization_commands.go",
  "internal/qualification/sandboxqualification/runner.go",
  "internal/storage/postgres/migrations/000024_v1c_c6_console_qualification.sql",
  "web/src/app/SandboxOperationsPage.tsx",
];

for (const path of requiredFiles) {
  if (!fs.existsSync(path)) {
    throw new Error(`sandbox-qualification required file is absent: ${path}`);
  }
}

const requireText = (source, values, label) => {
  for (const value of values) {
    if (!source.includes(value)) {
      throw new Error(`sandbox-qualification ${label} omits: ${value}`);
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
  "internal/storage/postgres/sandbox_runtime_console_order_admission.go",
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

const sandboxQualificationModel = read(
  "internal/qualification/sandboxqualification/model.go",
);
const sandboxQualificationRunner = read(
  "internal/qualification/sandboxqualification/runner.go",
);
requireText(
  sandboxQualificationModel + sandboxQualificationRunner,
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

const runnerMain = read("cmd/sandbox-qualification/main.go");
requireText(
  runnerMain,
  [
    'os.Getenv("AXIOM_SANDBOX_QUALIFICATION_ENABLED") != "1"',
    'os.Getenv("AXIOM_SANDBOX_QUALIFICATION_MODE") != "formal"',
    "Duration: sandboxQualification.FormalDuration",
    'BuildHash:         os.Getenv("AXIOM_SANDBOX_QUALIFICATION_BUILD_HASH")',
    'ImageHash:         os.Getenv("AXIOM_SANDBOX_QUALIFICATION_IMAGE_HASH")',
  ],
  "manual runner",
);

const apiSources = [
  "internal/api/console/sandbox.go",
  "internal/api/console/sandbox_commands.go",
  "internal/api/console/sandbox_authorization_commands.go",
  "internal/storage/postgres/sandbox_runtime_console_order_admission.go",
  "cmd/sandbox-qualification/main.go",
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
      `sandbox-qualification API/observer boundary contains forbidden token: ${forbidden}`,
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
  ["REAL-MONEY TRADING IS NOT AVAILABLE", "BINANCE SPOT TESTNET", "BYBIT DEMO"],
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
    "protect_v1c_c6_qualification_run", // semantic-naming: historical-reference
    "v1c_engine_runtime_events_immutable", // semantic-naming: historical-reference
    "v1c_c6_chaos_events_immutable", // semantic-naming: historical-reference
  ],
  "immutable evidence schema",
);

const makefile = read("Makefile");
requireText(
  makefile,
  [
    "sandbox-api-qualify:",
    "sandbox-frontend-qualify:",
    "sandbox-security-qualify:",
    "sandbox-chaos-qualify:",
    "sandbox-qualification-smoke:",
    "sandbox-qualification-formal:",
    "sandbox-qualification:",
  ],
  "qualification targets",
);

console.log("Sandbox-qualification source boundary passed");
