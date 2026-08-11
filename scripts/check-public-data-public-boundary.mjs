import fs from "node:fs";
import path from "node:path";

const failures = [];
const fail = (message) => {
  failures.push(message);
  process.stderr.write(`ERROR [public-data-boundary] ${message}\n`);
};

for (const file of [
  "internal/exchanges/binance/endpoint_policy.go",
  "internal/exchanges/binance/public_client.go",
  "internal/exchanges/binance/public_stream.go",
  "internal/exchanges/binance/collector.go",
  "internal/marketdata/book.go",
  "internal/recorder/binance_sink.go",
  "internal/qualification/public_data_qualification_soak_test.go",
  "internal/bootstrap/recorder_role.go",
]) {
  if (!fs.existsSync(file)) fail(`missing public-data artifact ${file}`);
}

const sandboxRuntimePrivateFiles = new Set(
  [
    "authenticated_client.go",
    "authenticated_operations.go",
    "authenticated_policy.go",
    "authenticated_response.go",
    "private_decoder.go",
    "private_subscription.go",
    "private_stream.go",
    "private_transport.go",
    "sandbox_adapter.go",
    "sandbox_clock.go",
    "sandbox_eligibility.go",
    "sandbox_filter_validation.go",
    "sandbox_filters.go",
    "sandbox_normalize.go",
    "sandbox_rate.go",
    "sandbox_recovery.go",
    "sandbox_reset.go",
    "sandbox_snapshot.go",
  ].map((file) => `internal/exchanges/binance/${file}`),
);
const productionFiles = goFiles("internal/exchanges/binance").filter(
  (file) => !file.endsWith("_test.go") && !sandboxRuntimePrivateFiles.has(file),
);
const production = productionFiles
  .map((file) => fs.readFileSync(file, "utf8"))
  .join("\n");

for (const [label, required] of [
  [
    "production REST origin",
    /publicRESTOrigin\s*=\s*"https:\/\/data-api\.binance\.vision"/,
  ],
  [
    "production WebSocket origin",
    /publicWSOrigin\s*=\s*"wss:\/\/data-stream\.binance\.vision"/,
  ],
  ["production endpoint set", /publicEndpointSet\s*=\s*"market-data-only-v1"/],
  ["raw recorder", /RecordPublicRaw/],
  ["canonical recorder", /RecordPublicCanonical/],
  ["gap recorder", /RecordSourceGap/],
  ["recorded snapshot", /SnapshotRecorded/],
  ["recorded subscription", /SubscribeRecorded/],
]) {
  if (!required.test(production)) fail(`missing invariant ${label}`);
}

const endpointPolicy = fs.readFileSync(
  "internal/exchanges/binance/endpoint_policy.go",
  "utf8",
);
for (const [label, required] of [
  [
    "Testnet public endpoint set",
    /testnetPublicEndpointSet\s*=\s*"testnet-market-data-only-v1"/,
  ],
  [
    "Testnet public REST origin",
    /testnetPublicRESTOrigin\s*=\s*"https:\/\/testnet\.binance\.vision"/,
  ],
  [
    "Testnet public WebSocket origin",
    /testnetPublicWSOrigin\s*=\s*"wss:\/\/stream\.testnet\.binance\.vision"/,
  ],
]) {
  if (!required.test(endpointPolicy)) fail(`missing invariant ${label}`);
}
for (const file of productionFiles) {
  if (
    file !== "internal/exchanges/binance/endpoint_policy.go" &&
    fs.readFileSync(file, "utf8").includes("testnet.binance")
  ) {
    fail(`Testnet public origin escaped endpoint policy ${file}`);
  }
}

for (const forbidden of [
  "/api/v3/" + "order",
  "/api/v3/" + "account",
  "/" + "sapi/",
  "/" + "fapi/",
  "api." + "binance.com",
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
  "CandlePage",
  "Subscribe",
  "SubscribeObserved",
  "SubscribeRecorded",
  "RestoreStreamGeneration",
]);
for (const method of exported) {
  if (!allowed.has(method))
    fail(`unexpected exported PublicClient method ${method}`);
}

if (failures.length > 0) process.exit(1);
process.stdout.write(
  `Public-data production-public source boundary passed (${exported.size} methods)\n`,
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
