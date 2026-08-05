import { execFileSync } from "node:child_process";

// This is a ratchet for the semantic-naming migration. Historical evidence,
// documentation, and already-applied migrations remain traceable; new active
// product changes must not introduce delivery-stage labels.
const baseline = "e7a7b39";
const forbidden =
  /\b(?:A(?:0|[1-9]|10|11)|B[1-8]|C[1-6]|D[1-6]|V1[ABCD]|PR[1-3])\b/;
const paths = [
  "cmd",
  "internal",
  "web/src",
  "api",
  "deploy",
  "monitoring",
  "scripts",
  ".github",
  "Makefile",
];
const diff = execFileSync(
  "git",
  ["diff", "--no-ext-diff", "--unified=0", baseline, "--", ...paths],
  { encoding: "utf8" },
);
const violations = [];
let file = "";
for (const line of diff.split("\n")) {
  if (line.startsWith("+++ b/")) file = line.slice(6);
  if (!line.startsWith("+") || line.startsWith("+++")) continue;
  if (file.includes("/migrations/") || file.endsWith(".md")) continue;
  if (forbidden.test(line.slice(1)))
    violations.push(`${file}: ${line.slice(1)}`);
}
if (violations.length)
  throw new Error(
    `delivery-stage terminology introduced:\n${violations.join("\n")}`,
  );
console.log("Semantic naming ratchet passed");
