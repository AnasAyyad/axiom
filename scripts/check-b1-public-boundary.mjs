import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [b1-public-boundary] ${message}\n`);
};

for (const file of [
  "internal/exchanges/bybit/endpoint_policy.go",
  "internal/exchanges/bybit/public_client.go",
  "internal/exchanges/bybit/public_stream.go",
  "internal/exchanges/bybit/collector.go",
  "internal/exchanges/contracts/recording.go",
  "internal/bootstrap/recorder_role.go",
  "deploy/config/platform-shadow-v1b.json",
  "internal/storage/postgres/migrations/000012_b1_bybit_public.sql",
  "internal/storage/postgres/migrations/000013_b1_exchange_strategy_generalization.sql",
]) {
  if (!fs.existsSync(file)) fail(`missing B1 artifact ${file}`);
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

const production = goFiles("internal/exchanges/bybit")
  .filter((file) => !file.endsWith("_test.go"))
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
  fs.readFileSync("deploy/config/platform-shadow-v1b.json", "utf8"),
);
if (
  configuration.schema_version !== "axiom.config.v1b.1" ||
  configuration.exchanges?.length !== 2 ||
  configuration.exchanges[1]?.id !== "bybit" ||
  configuration.secrets?.length !== 0
) {
  fail("reviewed V1B configuration is not ordered and credential-free");
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `B1 production-public source boundary passed (${exported.size} methods)\n`,
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
