import { readdirSync, readFileSync } from "node:fs";

const activeRoots = [
  "cmd/",
  "internal/",
  "web/src/",
  "web/tests/",
  "api/",
  "deploy/",
  "monitoring/",
  "scripts/",
  "tests/",
  ".github/",
];
const activeFiles = new Set([
  ".env.example",
  "Makefile",
  "docker-compose.yml",
  "sqlc.yaml",
]);
const excludedPrefixes = [
  "docs/",
  "examples/reproducibility/",
  "internal/api/static/dist/",
  "internal/storage/postgres/migrations/",
];
const excludedFiles = new Set([
  // This test verifies immutable, already-applied migration source verbatim.
  "internal/storage/postgres/migrate_test.go",
  // This checker validates immutable requirement IDs and delivery-plan evidence.
  "scripts/check-traceability-traceability.mjs",
]);
const stage = String.raw`(?:A(?:0|[1-9]|10|11)|B[1-8]|C[1-6]|D[1-6]|V1[ABCD]|PR[1-3])`;
const exactStage = new RegExp(
  String.raw`(?:^|[^A-Za-z0-9])${stage}(?=[^A-Za-z0-9]|$)`,
  "i",
);
const identifierStage = new RegExp(
  String.raw`(?:\b[vV]1[a-dA-D](?=[A-Z_])|\b(?:[aA](?:0|[1-9]|10|11)|[bB][1-8]|[cC][1-6]|[dD][1-6])(?=[A-Z_])|[a-z0-9](?:V1[A-D]|A(?:0|[1-9]|10|11)|B[1-8]|C[1-6]|D[1-6])(?=[A-Z_]))`,
);
const filenameStage = new RegExp(
  String.raw`(?:^|[/_.-])(?:a(?:0|[1-9]|10|11)|b[1-8]|c[1-6]|d[1-6]|v1[abcd]|pr[1-3])(?=[/_.-]|[A-Z]|$)`,
  "i",
);

const trackedAndUntracked = [
  ...activeFiles,
  ...activeRoots.flatMap((root) => walk(root.slice(0, -1))),
];

const violations = [];
for (const file of trackedAndUntracked) {
  if (!isActive(file) || isExcluded(file)) continue;
  if (filenameStage.test(file)) {
    violations.push(`${file}: delivery-stage name in active path`);
  }
  let source;
  try {
    source = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  if (source.includes("\0")) continue;
  source.split(/\r?\n/).forEach((line, index) => {
    if (isHistoricalReferenceLine(line)) return;
    const withoutImmutableRequirementIDs = line.replace(
      /AX-V1[ABCD]-[A-Z0-9/-]+/g,
      "",
    );
    const withoutQuotedContent = stripQuotedContent(
      withoutImmutableRequirementIDs,
    );
    if (
      exactStage.test(withoutImmutableRequirementIDs) ||
      identifierStage.test(withoutQuotedContent)
    ) {
      violations.push(`${file}:${index + 1}: ${line.trim()}`);
    }
  });
}

if (violations.length > 0) {
  const shown = violations.slice(0, 200);
  const remainder = violations.length - shown.length;
  throw new Error(
    `delivery-stage terminology remains in active product surfaces:\n${shown.join("\n")}${
      remainder > 0 ? `\n...and ${remainder} more` : ""
    }`,
  );
}

console.log("Semantic naming boundary passed for the complete active tree");

function isActive(file) {
  return (
    activeFiles.has(file) || activeRoots.some((root) => file.startsWith(root))
  );
}

function isExcluded(file) {
  return (
    file.endsWith(".md") ||
    excludedFiles.has(file) ||
    excludedPrefixes.some((prefix) => file.startsWith(prefix))
  );
}

function isHistoricalReferenceLine(line) {
  return (
    line.includes("semantic-naming: historical-reference") ||
    line.includes("internal/storage/postgres/migrations/") ||
    line.includes("docs/") ||
    line.includes("examples/reproducibility/") ||
    line.includes("research/testdata/") ||
    line.includes(".local/a7-soak-")
  );
}

function stripQuotedContent(line) {
  return line
    .replace(/"(?:\\.|[^"\\])*"/g, '""')
    .replace(/'(?:\\.|[^'\\])*'/g, "''")
    .replace(/`(?:\\.|[^`\\])*`/g, "``");
}

function walk(directory) {
  let entries;
  try {
    entries = readdirSync(directory, { withFileTypes: true });
  } catch {
    return [];
  }
  return entries.flatMap((entry) => {
    const path = `${directory}/${entry.name}`;
    if (entry.isDirectory()) {
      if (entry.name === "node_modules" || entry.name === ".git") return [];
      return walk(path);
    }
    return entry.isFile() ? [path] : [];
  });
}
