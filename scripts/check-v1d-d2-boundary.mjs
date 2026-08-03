import { readFileSync } from "node:fs";

const read = (path) => readFileSync(path, "utf8");
const requireTokens = (path, tokens) => {
  const source = read(path);
  for (const token of tokens) {
    if (!source.includes(token)) throw new Error(`${path} omits ${token}`);
  }
  return source;
};

requireTokens("crypto_bot_v1_codex_spec.md", [
  "### Phase D2: Complete React Command Center and monitoring",
  "navigation is grouped into `Home`, `Activity`, `Strategies`, `Run Lab`, `Risk & Controls`, and `Operations`",
  "REAL TRADING DISABLED",
  "Decisions & Orders",
  "Qualification Center",
]);

requireTokens("web/src/app/navigation.ts", [
  'label: "Home"',
  'label: "Activity"',
  'label: "Strategies"',
  'label: "Run Lab"',
  'label: "Risk & Controls"',
  'label: "Operations"',
  'label: "Decisions & Orders"',
  'label: "System Events"',
  'label: "Strategy Center"',
  'label: "Qualifications"',
]);

requireTokens("web/src/app/App.tsx", [
  'path="activity/decisions-orders"',
  'path="activity/system-events"',
  'path="strategies"',
  'path="run-lab"',
  'path="risk/controls"',
  'path="operations/qualifications"',
  'path="operations/alerts"',
  'path="operations/reports"',
  'path="operations/configuration"',
  'path="operations/users"',
]);

requireTokens("web/src/app/SafetyHeader.tsx", [
  "REAL TRADING DISABLED",
  "Environment",
  "Exchange",
  "Engine",
  "Risk",
  "Data",
  "Updates",
]);

requireTokens("web/src/features/activity/ActivityPage.tsx", [
  "Decisions & Orders",
  "System Events",
]);
requireTokens("web/src/features/activity/ActivityDetailPanel.tsx", [
  "downloadArtifact",
  "/api/v1/exports",
]);
requireTokens("web/src/features/strategies/StrategyCenterPage.tsx", [
  "Purpose, evidence, readiness, and control",
  "blocking_prerequisites",
  "StrategyControlPanel",
]);
requireTokens("web/src/features/qualifications/QualificationCenterPage.tsx", [
  "C6 remains its own exact 72-hour sandbox qualification",
  "QualificationCard",
]);

const runLab = requireTokens("web/src/features/run-lab/RunLabPage.tsx", [
  "approvedRuns",
  "cannot run arbitrary commands, scripts, or unit-test names",
  "They do not prove profitability",
]);
for (const forbidden of [
  "child_process",
  "exec(",
  "spawn(",
  "<input",
  "<textarea",
  "/api/v1/commands/execute",
]) {
  if (runLab.includes(forbidden)) {
    throw new Error(
      `Run Lab exposes forbidden arbitrary execution token: ${forbidden}`,
    );
  }
}

requireTokens("web/src/api/d1Validation.ts", [
  "activityPage",
  "resourcePage",
  "authorization",
  "exportArtifact",
  "d1CommandPaths",
]);
requireTokens("docs/requirements/v1d-d2-traceability.md", [
  "AX-V1D-D02-001",
  "AX-V1D-D02-010",
]);
requireTokens("docs/requirements/v1d-d2-source-coverage.md", [
  "D2 does not complete D3",
]);
requireTokens("docs/adr/0022-v1d-d2-command-center.md", [
  "Run Lab links only to approved product workflows",
]);

console.log("V1D D2 command-center and safety boundary passed");
