import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";

const read = (path) => readFileSync(path, "utf8");
const requireTokens = (path, tokens) => {
  const source = read(path);
  for (const token of tokens) {
    if (!source.includes(token)) throw new Error(`${path} omits ${token}`);
  }
  return source;
};

requireTokens("crypto_bot_v1_codex_spec.md", [
  "### Phase D6: V1 readiness and safety certification", // semantic-naming: historical-reference
  "# 35. V1D final acceptance criteria", // semantic-naming: historical-reference
]);

requireTokens("internal/certification/validate.go", [
  '"traceability"',
  '"application-baseline"',
  '"configuration-reference"',
  '"public-data-qualification"',
  '"strategy-execution"',
  '"multi-exchange-console"',
  '"sandbox-qualification"',
  '"owner-control"',
  '"operational-readiness"',
  '"security"',
  '"accounting"',
  '"reconciliation"',
  '"determinism-reproducibility"',
  '"authorization-reauthentication"',
  '"secret-leakage-redaction"',
  '"operations-recovery"',
  "formal_prerequisites_missing",
  "unresolved_high_severity_finding",
  "section_35_blocked_or_invalid",
]);
requireTokens("cmd/release-certify/main.go", [
  "AXIOM_RELEASE_CERTIFICATION_ENABLED",
  "final certification is default-off",
  "final certification build identity mismatch",
  "WriteVerdictNoReplace",
]);
requireTokens("docs/releases/v1-safety-manifest.md", [
  "Spot-only",
  "Owned-inventory-only sells",
  "Central allocator and risk",
  "No production-private submission",
  "Exact identity set",
  "UNSIGNED / NOT CERTIFIED",
]);

const makefile = requireTokens("Makefile", [
  "release-certification-model-qualify:",
  "release-certification-traceability-qualify:",
  "release-certification-security-qualify:",
  "release-certification-formal:",
  "release-certify:",
]);
const aggregate =
  makefile.match(/release-certify:[\s\S]*?(?=\n\S|$)/)?.[0] ?? "";
if (aggregate.split("##")[0].includes("soak")) {
  throw new Error(
    "release-certification local aggregate must not invoke a soak-named target",
  );
}

for (const path of [
  "docs/requirements/v1d-d6-traceability.md",
  "docs/requirements/v1d-d6-source-coverage.md",
  "docs/releases/v1d-d6-checklist.md",
  "docs/releases/v1d-d6-readiness.md",
  "docs/releases/evidence/v1d-d6-local-validation.md",
  "docs/releases/v1a-v1d-evidence-index.md",
  "docs/releases/v1d-section-35-matrix.md",
  "docs/releases/known-limitations.md",
  "docs/releases/server-declaration.md",
  "docs/releases/restore-evidence-index.md",
  "docs/releases/v1d-d6-release-candidate.md",
  "docs/releases/v1-safety-manifest.md",
  "docs/releases/v1d-d4-readiness.md",
  "docs/releases/v1d-d5-readiness.md",
  "docs/releases/evidence/v1d-d4-local-validation.md",
  "docs/releases/evidence/v1d-d5-local-validation.md",
  "docs/operations/runbook.md",
  "docs/operations/backup-restore.md",
  "docs/configuration/environment.md",
  "docs/accounting/journal-and-valuation.md",
  "docs/research/validation-policy.md",
  "docs/deployment/single-server-compose.md",
  "docs/deployment/tls-and-secrets.md",
  "deploy/config/v1-safety-manifest.example.json",
  "deploy/config/v1-release-candidate.example.json",
  "deploy/config/v1-trusted-reviewers.example.json",
  "deploy/config/v1-release-verdict.schema.json",
  "deploy/config/v1-release-verdict.example.json",
]) {
  read(path);
}

for (const line of read("examples/reproducibility/v1d-d6/SHA256SUMS")
  .trim()
  .split("\n")) {
  const match = /^([0-9a-f]{64})  ([A-Za-z0-9._-]+)$/.exec(line);
  if (!match) throw new Error("example bundle checksum inventory is invalid");
  const actual = createHash("sha256")
    .update(readFileSync(`examples/reproducibility/v1d-d6/${match[2]}`))
    .digest("hex");
  if (actual !== match[1]) {
    throw new Error(`example bundle checksum mismatch for ${match[2]}`);
  }
}

const section35 = read("docs/releases/v1d-section-35-matrix.md");
for (let criterion = 1; criterion <= 22; criterion += 1) {
  if (!section35.includes(`| ${criterion} |`)) {
    throw new Error(`Section 35 matrix omits criterion ${criterion}`);
  }
}
for (const token of [
  "Implemented",
  "Locally verified",
  "Hosted CI verified",
  "Formally qualified",
  "Formally accepted/certified",
  "Blocked",
]) {
  if (!section35.includes(token)) {
    throw new Error(`Section 35 matrix omits status vocabulary ${token}`);
  }
}

const candidate = JSON.parse(
  read("deploy/config/v1-release-candidate.example.json"),
);
if (
  candidate.prerequisites.length !== 0 ||
  candidate.reviews.length !== 0 ||
  candidate.section_35.some((criterion) => criterion.state === "PASSED")
) {
  throw new Error("release-candidate example must remain visibly fail-closed");
}
const trust = JSON.parse(
  read("deploy/config/v1-trusted-reviewers.example.json"),
);
if (trust.reviewers.length !== 0) {
  throw new Error("example trust store must not invent trusted reviewers");
}

for (const area of [
  "security",
  "accounting",
  "reconciliation",
  "determinism-reproducibility",
  "authorization-reauthentication",
  "secret-leakage-redaction",
  "operations-recovery",
]) {
  const review = JSON.parse(
    read(`docs/releases/reviews/${area}.template.json`),
  );
  if (review.area !== area || review.state !== "PENDING" || review.signature) {
    throw new Error(`${area} review template is not fail-closed`);
  }
}

console.log(
  "Release certification, evidence, and documentation boundary passed",
);
