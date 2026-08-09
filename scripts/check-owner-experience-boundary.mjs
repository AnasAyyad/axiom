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
  "### Phase D2: Complete React Command Center and monitoring", // semantic-naming: historical-reference
  "navigation is grouped into `Overview`, `Explore`, `Run`, `Monitor`,",
  "`Portfolio & Risk`, and `System`",
  "REAL-MONEY TRADING IS NOT AVAILABLE",
  "Decisions & Orders",
  "Readiness lists approved qualifications and drills",
]);

requireTokens("web/src/app/navigation.ts", [
  'label: "Overview"',
  'label: "Explore"',
  'label: "Run"',
  'label: "Monitor"',
  'label: "Risk & Controls"',
  'label: "System"',
  'label: "Getting Started"',
  'label: "New Run"',
  'label: "Guided Demonstrations"',
  'label: "Live Shadow"',
  'label: "Exchange Sandbox"',
  'label: "System Events"',
  'label: "Readiness"',
]);

const routes = requireTokens("web/src/app/App.tsx", [
  'path="getting-started"',
  'path="glossary"',
  'path="exchanges"',
  'path="data-catalogue"',
  'path="activity/decisions-orders"',
  'path="activity/system-events"',
  'path="strategies"',
  'path="run-lab"',
  'path="guided-demonstrations"',
  'path="shadow"',
  'path="operations/sandbox"',
  'path="risk/controls"',
  'path="operations/qualifications"',
  'path="operations/alerts"',
  'path="operations/reports"',
  'path="operations/configuration"',
  'path="audit"',
]);
if (routes.includes('path="operations/users"')) {
  throw new Error(
    "single-owner console retained an obsolete user-management route",
  );
}

requireTokens("web/src/app/SafetyHeader.tsx", [
  "REAL-MONEY TRADING IS NOT AVAILABLE",
  "Environment",
  "Binance data",
  "Bybit data",
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
  "A smoke pass cannot",
  "become a formal pass.",
  "QualificationCard",
]);

const runLabPath = "web/src/features/run-lab/RunLabPage.tsx";
const runLab =
  read(runLabPath) + read("web/src/features/run-lab/RunChoiceWizard.tsx");
for (const token of [
  "catalog.choices",
  "Every choice comes from the server.",
  "do not prove profitability.",
]) {
  if (!runLab.includes(token)) throw new Error(`${runLabPath} omits ${token}`);
}
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

requireTokens("web/src/api/ownerControlValidation.ts", [
  "activityPage",
  "resourcePage",
  "authorization",
  "exportArtifact",
  "ownerControlCommandPaths",
]);
requireTokens("docs/requirements/v1d-d2-traceability.md", [
  "AX-V1D-D02-001",
  "AX-V1D-D02-010",
]);
requireTokens("docs/requirements/v1d-d2-source-coverage.md", [
  "D2 does not complete D3", // semantic-naming: historical-reference
]);
requireTokens("docs/adr/0022-v1d-d2-command-center.md", [
  "Run Lab links only to approved product workflows",
]);

console.log("Owner-experience command-center and safety boundary passed");
