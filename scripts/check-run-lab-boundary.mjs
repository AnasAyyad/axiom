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
  "### Phase D3: Complete Backtest, Replay, and Shadow Labs", // semantic-naming: historical-reference
  "guided, version-controlled presets",
  "Reproduction bundles contain safe manifests",
  "strategy viability separately from platform readiness",
]);

requireTokens("api/openapi.yaml", [
  "operationId: listShadowSessions",
  "LabInputManifest:",
  "LabLifecycleCapabilities:",
  "ReproductionBundle:",
  "ReplayCheckpoint:",
  "ShadowPnlAttribution:",
  "ShadowDataHealth:",
]);

requireTokens("internal/storage/postgres/run_lab_lab_projections.go", [
  "populateRunLabJobEvidence",
  "runLabLabLifecycle",
  "runLabReplayCheckpoints",
  "canonical_payload",
]);
requireTokens("internal/storage/postgres/run_lab_shadow_projection.go", [
  "runLabShadowDecisions",
  "runLabShadowInventory",
  "runLabShadowPnl",
  "runLabShadowHealth",
  "sealed",
]);
requireTokens(
  "internal/storage/postgres/owner_control_console_export_record.go",
  [
    'record["input_hash"]',
    'record["manifest_hash"]',
    'record["dataset_manifest_hash"]',
    'record["configuration_hash"]',
  ],
);

const guidedPath = "web/src/features/run-lab/RunLabPage.tsx";
const guided =
  read(guidedPath) + read("web/src/features/run-lab/RunChoiceWizard.tsx");
for (const token of [
  "Every choice comes from the server.",
  "latest qualified immutable inputs",
  "reviewed portfolio, risk, fee, fill, latency, and slippage",
  "assumptions.",
  "You never need to copy a dataset, configuration, portfolio, or model ID.",
]) {
  if (!guided.includes(token)) throw new Error(`${guidedPath} omits ${token}`);
}
for (const forbidden of [
  "child_process",
  "exec(",
  "spawn(",
  "production-private",
  "authorization_header",
  "request_signature",
]) {
  if (guided.includes(forbidden)) {
    throw new Error(`Guided lab form exposes forbidden token: ${forbidden}`);
  }
}

requireTokens("web/src/features/labs/LabRunTools.tsx", [
  "/api/v1/lab-runs/",
  "/api/v1/exports",
  "Compare exact run evidence",
  "do not prove profitability",
]);
requireTokens("web/src/app/ReplayLab.tsx", [
  "Durable replay checkpoints",
  "ReplayFaultScheduler",
  "Exact event and decision inspection",
]);
requireTokens("web/src/app/ShadowCenter.tsx", [
  "ShadowSessionEvidence",
  "Compare shadow sessions",
]);
const shadowEvidence =
  read("web/src/features/labs/ShadowSessionEvidence.tsx") +
  read("web/src/features/labs/ShadowSessionResults.tsx");
for (const token of [
  "Recent decisions and risk actions",
  "Owned virtual inventory",
  "Sealed-ledger P&amp;L attribution",
  "Public-data health",
]) {
  if (!shadowEvidence.includes(token)) {
    throw new Error(`Shadow session evidence omits ${token}`);
  }
}
requireTokens("docs/requirements/v1d-d3-traceability.md", [
  "AX-V1D-D03-001",
  "AX-V1D-D03-008",
]);
requireTokens("docs/requirements/v1d-d3-source-coverage.md", [
  "B2, C6, and D5 remain separate", // semantic-naming: historical-reference
]);
requireTokens("docs/adr/0023-v1d-d3-research-labs.md", [
  "keeps execution inputs closed and immutable",
]);

console.log("Run-lab research, evidence, and safety boundary passed");
