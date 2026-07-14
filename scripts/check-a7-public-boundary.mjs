import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [a7-public-boundary] ${message}\n`);
};

for (const file of [
  "internal/exchanges/binance/endpoint_policy.go",
  "internal/exchanges/binance/public_client.go",
  "internal/exchanges/binance/public_stream.go",
  "internal/exchanges/binance/collector.go",
  "internal/marketdata/book.go",
  "internal/recorder/binance_sink.go",
  "internal/qualification/a7_soak_test.go",
  "internal/bootstrap/recorder_role.go",
]) {
  if (!fs.existsSync(file)) fail(`missing A7 artifact ${file}`);
}

const production = goFiles("internal/exchanges/binance")
  .filter((file) => !file.endsWith("_test.go"))
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const required of [
  'publicRESTOrigin  = "https://data-api.binance.vision"',
  'publicWSOrigin    = "wss://data-stream.binance.vision"',
  'publicEndpointSet = "market-data-only-v1"',
  "RecordPublicRaw",
  "RecordPublicCanonical",
  "RecordSourceGap",
  "SnapshotRecorded",
  "SubscribeRecorded",
]) {
  if (!production.includes(required)) fail(`missing invariant ${required}`);
}

for (const forbidden of [
  "/api/v3/" + "order",
  "/api/v3/" + "account",
  "/" + "sapi/",
  "/" + "fapi/",
  "api." + "binance.com",
  "test" + "net.binance",
]) {
  if (production.includes(forbidden))
    fail(`forbidden production route/origin ${forbidden}`);
}

const exported = new Set();
for (const match of production.matchAll(
  /func \(client \*PublicClient\) ([A-Z][A-Za-z0-9_]*)\(/g,
)) {
  exported.add(match[1]);
}
const allowed = new Set([
  "Capabilities",
  "Ping",
  "SampleServerTime",
  "SampleServerTimeRecorded",
  "TimeHealth",
  "Snapshot",
  "SnapshotRecorded",
  "MonotonicOffset",
  "Instruments",
  "Trades",
  "Candles",
  "Subscribe",
  "SubscribeObserved",
  "SubscribeRecorded",
]);
for (const method of exported) {
  if (!allowed.has(method))
    fail(`unexpected exported PublicClient method ${method}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `A7 production-public source boundary passed (${exported.size} methods)\n`,
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
