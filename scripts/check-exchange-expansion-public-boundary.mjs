import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(
    `ERROR [exchange-expansion-public-boundary] ${message}\n`,
  );
};

for (const file of [
  "internal/exchanges/bybit/endpoint_policy.go",
  "internal/exchanges/bybit/public_client.go",
  "internal/exchanges/bybit/public_stream.go",
  "internal/exchanges/bybit/collector.go",
  "internal/exchanges/contracts/recording.go",
  "internal/bootstrap/recorder_role.go",
  "deploy/config/platform-research.json",
  "internal/storage/postgres/migrations/000012_b1_bybit_public.sql",
  "internal/storage/postgres/migrations/000013_b1_exchange_strategy_generalization.sql",
]) {
  if (!fs.existsSync(file)) fail(`missing exchange-expansion artifact ${file}`);
}

const generalization = fs.readFileSync(
  "internal/storage/postgres/migrations/000013_b1_exchange_strategy_generalization.sql",
  "utf8",
);
for (const required of [
  "portfolio_ownership_strategy_reference",
  "shadow_public_exchange_reference",
  "exchange_id text REFERENCES exchanges(id)",
]) {
  if (!generalization.includes(required))
    fail(`missing relational ownership invariant ${required}`);
}
for (const forbidden of [
  "CHECK (strategy_key = 'trend')",
  "CHECK (public_exchange = 'binance-production-public')",
]) {
  if (generalization.includes(forbidden))
    fail(`legacy single-strategy/exchange constraint ${forbidden}`);
}

const sandboxRuntimePrivateFiles = new Set(
  [
    "authenticated_client.go",
    "authenticated_operations.go",
    "authenticated_policy.go",
    "authenticated_response.go",
    "private_decoder.go",
    "private_stream.go",
    "private_transport.go",
    "sandbox_adapter.go",
    "sandbox_balances.go",
    "sandbox_budget.go",
    "sandbox_clock.go",
    "sandbox_eligibility.go",
    "sandbox_fills.go",
    "sandbox_filter_helpers.go",
    "sandbox_filters.go",
    "sandbox_history.go",
    "sandbox_normalize.go",
    "sandbox_payloads.go",
    "sandbox_rate.go",
    "sandbox_snapshot.go",
  ].map((file) => `internal/exchanges/bybit/${file}`),
);
const production = goFiles("internal/exchanges/bybit")
  .filter(
    (file) =>
      !file.endsWith("_test.go") && !sandboxRuntimePrivateFiles.has(file),
  )
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  'publicRESTOrigin  = "https://api.bybit.com"',
  'publicWSOrigin    = "wss://stream.bybit.com/v5/public/spot"',
  'publicEndpointSet = "bybit-public-v1"',
  "RecordPublicRaw",
  "RecordPublicCanonical",
  "RecordSourceGap",
  "SnapshotRecorded",
  "SubscribeRecorded",
  "BookDepth: 1000",
  'CandleIntervals: []string{"15m", "1h", "4h"}',
]) {
  if (!production.includes(required)) fail(`missing invariant ${required}`);
}

for (const forbidden of [
  "/v5/" + "order/",
  "/v5/" + "account/",
  "/v5/" + "asset/withdraw/",
  "/v5/" + "asset/transfer/",
  "api-demo." + "bybit.com",
  "api-testnet." + "bybit.com",
]) {
  if (production.includes(forbidden))
    fail(`forbidden route/origin ${forbidden}`);
}

const allowed = new Set([
  "Capabilities",
  "Health",
  "RateBudget",
  "MonotonicOffset",
  "SampleServerTime",
  "SampleServerTimeRecorded",
  "Snapshot",
  "SnapshotRecorded",
  "StrategyInstrumentRules",
  "Instruments",
  "Trades",
  "Candles",
  "Ticker",
  "Subscribe",
  "SubscribeObserved",
  "SubscribeRecorded",
]);
const exported = new Set();
for (const match of production.matchAll(
  /func \(client \*PublicClient\) ([A-Z][A-Za-z0-9_]*)\(/g,
)) {
  exported.add(match[1]);
}
for (const method of exported) {
  if (!allowed.has(method))
    fail(`unexpected exported PublicClient method ${method}`);
}

const configuration = JSON.parse(
  fs.readFileSync("deploy/config/platform-research.json", "utf8"),
);
if (
  configuration.schema_version !== "axiom.configuration@1.5.0" ||
  configuration.exchanges?.length !== 2 ||
  configuration.exchanges[1]?.id !== "bybit" ||
  (configuration.secrets != null && configuration.secrets.length !== 0)
) {
  fail("reviewed exchange configuration is not ordered and credential-free");
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `Exchange-expansion production-public source boundary passed (${exported.size} methods)\n`,
);

function goFiles(root) {
  const results = [];
  walk(root, results);
  return results.sort();
}

function walk(directory, results) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) walk(target, results);
    else if (target.endsWith(".go")) results.push(target);
  }
}
