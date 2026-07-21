import { readFileSync, readdirSync, statSync } from "node:fs";
import { createRequire } from "node:module";
import { join, relative } from "node:path";

const require = createRequire(new URL("../web/package.json", import.meta.url));
const { parse } = require("yaml");

const root = process.cwd();
const openapi = parse(readFileSync(join(root, "api/openapi.yaml"), "utf8"));
const required = [
  ["get", "/health/live"],
  ["get", "/health/ready"],
  ["post", "/api/v1/session/login"],
  ["post", "/api/v1/session/logout"],
  ["get", "/api/v1/session/me"],
  ["get", "/api/v1/system/status"],
  ["get", "/api/v1/exchanges/binance/health"],
  ["get", "/api/v1/exchanges/binance/instruments"],
  ["get", "/api/v1/portfolios"],
  ["get", "/api/v1/portfolios/{id}"],
  ["get", "/api/v1/portfolios/{id}/journal"],
  ["get", "/api/v1/risk/status"],
  ["post", "/api/v1/risk/pause"],
  ["post", "/api/v1/risk/resume"],
  ["get", "/api/v1/strategies/trend"],
  ["get", "/api/v1/strategies/trend/decisions"],
  ["post", "/api/v1/backtests"],
  ["get", "/api/v1/backtests/{id}"],
  ["post", "/api/v1/replays"],
  ["get", "/api/v1/replays/{id}"],
  ["post", "/api/v1/replays/{id}/pause"],
  ["post", "/api/v1/replays/{id}/resume"],
  ["post", "/api/v1/replays/{id}/step"],
  ["post", "/api/v1/shadow-sessions"],
  ["post", "/api/v1/shadow-sessions/{id}/stop"],
  ["get", "/api/v1/shadow-sessions/{id}"],
  ["get", "/api/v1/incidents"],
  ["get", "/api/v1/incidents/{id}"],
  ["get", "/api/v1/audit-events"],
  ["get", "/api/v1/stream"],
];
for (const [method, path] of required) {
  if (openapi.paths?.[path]?.[method] === undefined)
    throw new Error(`missing A11 contract ${method.toUpperCase()} ${path}`);
}

const frontend = files(join(root, "web/src")).filter((path) =>
  /\.(ts|tsx)$/.test(path),
);
for (const path of frontend) {
  const source = readFileSync(path, "utf8");
  if (
    /from ["']echarts(?:\/|["'])/.test(source) &&
    !path.endsWith("/components/EvidenceChart.tsx")
  ) {
    throw new Error(
      `ECharts bypasses the project adapter: ${relative(root, path)}`,
    );
  }
  if (
    !path.endsWith("/components/EvidenceChart.tsx") &&
    /\b(?:parseFloat|parseInt|Number)\s*\(/.test(source)
  ) {
    throw new Error(
      `browser-side numeric interpretation outside chart adapter: ${relative(root, path)}`,
    );
  }
}

const shell = readFileSync(join(root, "web/src/app/AppShell.tsx"), "utf8");
for (const label of [
  "REAL TRADING DISABLED",
  "SHADOW · VIRTUAL",
  "production_public",
]) {
  if (!shell.includes(label))
    throw new Error(`persistent A11 safety label missing: ${label}`);
}

const compose = parse(readFileSync(join(root, "docker-compose.yml"), "utf8"));
const secretNames = [
  "bootstrap_owner_email",
  "bootstrap_owner_password_hash",
  "csrf_key",
  "session_signing_key",
];
for (const name of secretNames) {
  const consumers = Object.entries(compose.services ?? {})
    .filter(([, service]) => (service.secrets ?? []).includes(name))
    .map(([service]) => service);
  if (consumers.length !== 1 || consumers[0] !== "api")
    throw new Error(`A11 secret ${name} consumers = ${consumers.join(",")}`);
}
for (const service of ["engine-shadow", "recorder", "backtest-worker"]) {
  const environment = JSON.stringify(
    compose.services?.[service]?.environment ?? {},
  );
  if (/AUTH_|API_KEY|SECRET_KEY|PRIVATE_KEY|SIGNING_KEY/.test(environment))
    throw new Error(
      `${service} received an authentication or exchange signing input`,
    );
}

console.log(
  `A11 contract and console boundary passed (${required.length} required operations)`,
);

function files(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = join(directory, entry);
    return statSync(path).isDirectory() ? files(path) : [path];
  });
}
