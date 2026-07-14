import fs from "node:fs";
import path from "node:path";

const runtimeDirectory = "internal/runtime";
const productionFiles = fs
  .readdirSync(runtimeDirectory)
  .filter((file) => file.endsWith(".go") && !file.endsWith("_test.go"));
let violations = 0;

function fail(file, rule) {
  process.stderr.write(`ERROR [a3-runtime-boundary] ${file}: ${rule}\n`);
  violations += 1;
}

for (const file of productionFiles) {
  const source = fs.readFileSync(path.join(runtimeDirectory, file), "utf8");
  if (/"(?:net\/http|net\/rpc|math\/rand)"/.test(source)) {
    fail(file, "hot-path network RPC or shared random dependency is forbidden");
  }
  if (
    /\btime\.(?:Now|Since)\s*\(/.test(source) &&
    !["clock.go", "lifecycle.go"].includes(file)
  ) {
    fail(file, "ambient time is allowed only inside clock/lifecycle adapters");
  }
  if (/make\s*\(\s*chan\s+[^,)]*\)/.test(source) && file !== "lifecycle.go") {
    fail(file, "unbounded asynchronous channel detected");
  }
}

const moduleFile = fs.readFileSync("go.mod", "utf8").toLowerCase();
if (/\b(?:redis|nats|kafka)\b/.test(moduleFile)) {
  fail(
    "go.mod",
    "an unapproved second coordination broker dependency is present",
  );
}

const pipeline = fs.readFileSync(
  path.join(runtimeDirectory, "pipeline.go"),
  "utf8",
);
const stages = [
  "StagePublicEvent",
  "StageRawRecording",
  "StageValidation",
  "StageMarketView",
  "StageTrend",
  "StageAllocation",
  "StageRisk",
  "StageSimulation",
  "StageDurableOrderJournal",
  "StageOutbox",
  "StageMetricsAudit",
  "StageAPIStream",
];
let previous = -1;
for (const stage of stages) {
  const current = pipeline.indexOf(stage);
  if (current <= previous) {
    fail("pipeline.go", `missing or reordered stage ${stage}`);
  }
  previous = current;
}

if (violations > 0) {
  process.exit(1);
}
process.stdout.write("A3 deterministic runtime boundary passed\n");
