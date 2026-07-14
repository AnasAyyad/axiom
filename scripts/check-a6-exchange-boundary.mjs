import fs from "node:fs";
import path from "node:path";

const failures = [];
function fail(message) {
  failures.push(message);
  process.stderr.write(`ERROR [a6-exchange-boundary] ${message}\n`);
}

const requiredFiles = [
  "internal/exchanges/contracts/capabilities.go",
  "internal/exchanges/contracts/errors.go",
  "internal/exchanges/contracts/market_data.go",
  "internal/exchanges/contracts/rate_budget.go",
  "internal/exchanges/contracts/retry.go",
  "internal/exchanges/binance/capabilities.go",
  "internal/exchanges/binance/normalize_market.go",
  "internal/exchanges/emulator/scenario.go",
  "internal/exchanges/emulator/server.go",
  "testdata/exchanges/binance/normalized.golden.json",
];
for (const file of requiredFiles) {
  if (!fs.existsSync(file)) fail(`missing required artifact ${file}`);
}

const contracts = [
  "capabilities.go",
  "errors.go",
  "market_data.go",
  "rate_budget.go",
  "retry.go",
]
  .map((file) =>
    fs.readFileSync(`internal/exchanges/contracts/${file}`, "utf8"),
  )
  .join("\n");
for (const invariant of [
  "type MarketDataSource interface",
  "type InstrumentCatalog interface",
  "type HistoricalReader interface",
  "type CapabilitySource interface",
  "ErrorCapability",
  "ErrorRateLimit",
  "ErrorTransient",
  "ErrorTimestamp",
  "ErrorFilter",
  "ErrorInsufficientFunds",
  "ErrorMaintenance",
  "ErrorValidation",
  "ErrorAmbiguousState",
  "RetryReconcile",
  "RecoveryReserve",
  "JitterSeed",
]) {
  if (!contracts.includes(invariant))
    fail(`missing contract invariant ${invariant}`);
}

const capabilitySource = fs.readFileSync(
  "internal/exchanges/binance/capabilities.go",
  "utf8",
);
for (const feature of [
  "FeaturePrivateData",
  "FeatureOrders",
  "FeatureImmediateOrCancel",
  "FeatureFillOrKill",
  "FeaturePostOnly",
  "FeatureCancellation",
  "FeatureClientGeneratedIDs",
  "FeatureReconciliation",
]) {
  if (!capabilitySource.includes(`unsupported(exchangecontracts.${feature})`))
    fail(`Binance V1A does not explicitly reject ${feature}`);
}

const forbiddenCallable = [
  /\b(?:Place|Submit|Create|Amend|Replace|Query)Order\b/,
  new RegExp(
    `\\b(?:${["Request" + "Signer", "Signed" + "Transport", "Authenticated" + "Client"].join("|")})\\b`,
  ),
  new RegExp(
    `\\b(?:${["API" + "Key", "Secret" + "Key", "Exchange" + "Credentials"].join("|")})\\b`,
  ),
  /\bhmac\.New\b/,
];
for (const file of goFiles("internal/exchanges")) {
  if (file.endsWith("_test.go")) continue;
  const source = fs.readFileSync(file, "utf8");
  for (const expression of forbiddenCallable) {
    if (expression.test(source)) fail(`forbidden callable boundary in ${file}`);
  }
}

for (const file of goFiles(".")) {
  if (file.includes("node_modules") || file.includes("/.git/")) continue;
  if (
    file.endsWith("_test.go") ||
    file.startsWith("internal/exchanges/emulator/")
  )
    continue;
  const source = fs.readFileSync(file, "utf8");
  if (source.includes('"axiom/internal/exchanges/emulator"'))
    fail(`platform source imports test-only emulator from ${file}`);
}

const expectedFixtures = [
  "candle-stream.json",
  "candles.json",
  "depth-snapshot.json",
  "depth-update.json",
  "exchange-info-unknown-status.json",
  "exchange-info.json",
  "normalized.golden.json",
  "trades.json",
];
for (const fixture of expectedFixtures) {
  const target = path.join("testdata/exchanges/binance", fixture);
  if (!fs.existsSync(target)) {
    fail(`missing sanitized fixture ${fixture}`);
    continue;
  }
  const source = fs.readFileSync(target, "utf8");
  try {
    JSON.parse(source);
  } catch {
    fail(`fixture is not valid JSON: ${fixture}`);
  }
  for (const sensitive of [
    "api" + "Key",
    "secret",
    "signature",
    "Authorization",
    "Cookie",
    "passphrase",
  ]) {
    if (source.toLowerCase().includes(sensitive.toLowerCase()))
      fail(`fixture contains sensitive field ${fixture}`);
  }
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `A6 exchange boundary validated (${expectedFixtures.length} fixtures)\n`,
);

function goFiles(root) {
  const results = [];
  walk(root, results);
  return results.sort();
}

function walk(directory, results) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    if (
      [".git", ".local", ".secrets", "node_modules", "dist"].includes(
        entry.name,
      )
    )
      continue;
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) walk(target, results);
    else if (target.endsWith(".go")) results.push(target.replace(/^\.\//, ""));
  }
}
