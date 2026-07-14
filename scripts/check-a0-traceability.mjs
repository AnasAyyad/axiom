import fs from "node:fs";

const planPath = process.argv[2];

const matrix = fs.readFileSync("docs/requirements/traceability.md", "utf8");
const coverage = fs.readFileSync(
  "docs/requirements/source-coverage.md",
  "utf8",
);
const plan = planPath ? fs.readFileSync(planPath, "utf8") : undefined;
const failures = [];
const validStatuses = new Set([
  "Not started",
  "In progress",
  "Implemented",
  "Verified",
  "Blocked",
  "Retired",
]);

const rows = matrix
  .split(/\r?\n/)
  .filter((line) => line.startsWith("| AX-V1A-"))
  .map((line) => ({
    line,
    fields: line
      .split("|")
      .slice(1, -1)
      .map((x) => x.trim()),
  }));
const ids = rows.map((row) => row.fields[0]);
const idSet = new Set(ids);

if (rows.length !== 381)
  failures.push("expected 381 matrix rows, found " + rows.length);
if (idSet.size !== rows.length)
  failures.push("matrix requirement IDs are not unique");
for (const row of rows) {
  if (row.fields.length !== 8 || row.fields.some((field) => field === "")) {
    failures.push("malformed or empty matrix field: " + row.fields[0]);
  }
  if (!validStatuses.has(row.fields[7])) {
    failures.push("invalid status for " + row.fields[0] + ": " + row.fields[7]);
  }
}

const a0Rows = rows.filter((row) => row.fields[0].startsWith("AX-V1A-A00-"));
if (a0Rows.length !== 37)
  failures.push("expected 37 A0 rows, found " + a0Rows.length);
for (const row of a0Rows) {
  if (row.fields[7] !== "Verified")
    failures.push(row.fields[0] + " is not Verified");
  if (!row.fields[6].includes("releases/evidence/a0-review.md")) {
    failures.push(row.fields[0] + " does not register the A0 review evidence");
  }
}

const retired = rows.filter((row) => row.fields[7] === "Retired");
if (retired.length !== 10)
  failures.push("expected 10 retired rows, found " + retired.length);
for (const row of retired) {
  if (!/Superseded by AX-V1A-/.test(row.fields[2])) {
    failures.push(row.fields[0] + " does not name an explicit successor");
  }
}

const coverageIDs = new Set(
  coverage.match(
    /AX-V1A-(?:A(?:00|01|02|03|04|05|06|07|08|09|10|11)|RG)-[A-Z]+-[0-9]{3}/g,
  ) ?? [],
);
for (const id of idSet) {
  if (!coverageIDs.has(id))
    failures.push("matrix ID absent from source coverage: " + id);
}
for (const id of coverageIDs) {
  if (!idSet.has(id)) failures.push("unknown source-coverage ID: " + id);
}

if (plan !== undefined) {
  const planA11 = plan.match(
    /Implement OpenAPI-described endpoints:\s*([\s\S]*?)\n\s*API rules:/,
  );
  if (!planA11) {
    failures.push("could not locate the Plan A11 endpoint block");
  } else {
    const planEndpoints = [
      ...planA11[1].matchAll(/^\s*-\s+(GET|POST)\s+(\/\S+)\s*$/gm),
    ].map((match) => match[1] + " " + match[2]);
    const coverageEndpoints = [
      ...coverage.matchAll(
        /^\| P§6\/A11-EP-[0-9]{2} \| `((?:GET|POST) \/[^`]+)` \|/gm,
      ),
    ].map((match) => match[1]);
    if (planEndpoints.length !== 30)
      failures.push(
        "expected 30 Plan endpoints, found " + planEndpoints.length,
      );
    if (coverageEndpoints.length !== 30)
      failures.push(
        "expected 30 coverage endpoints, found " + coverageEndpoints.length,
      );
    if (JSON.stringify(planEndpoints) !== JSON.stringify(coverageEndpoints)) {
      failures.push("Plan and source-coverage endpoint lists differ");
    }
  }
}

for (const forbidden of [
  "ROW_COUNT",
  "P§7/RV-ALERT",
  "owning phase",
  "A11-FUN-001 through",
]) {
  if (matrix.includes(forbidden) || coverage.includes(forbidden)) {
    failures.push("vague or invalid traceability marker remains: " + forbidden);
  }
}

if (failures.length > 0) {
  for (const failure of failures) console.error(failure);
  process.exit(1);
}

console.log(
  "validated 381 unique requirements, 37 verified A0 rows, 10 retired IDs, and complete reverse coverage" +
    (plan === undefined
      ? " (external Plan comparison skipped)"
      : " with 30 exact Plan A11 endpoints"),
);
