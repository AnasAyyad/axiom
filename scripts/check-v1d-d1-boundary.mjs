import { readFileSync, readdirSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire(new URL("../web/package.json", import.meta.url));
const { parse } = require("yaml");
const read = (path) => readFileSync(path, "utf8");
const api = parse(read("api/openapi.yaml"));

const requiredOperations = [
  ["post", "/api/v1/authorizations"],
  ["get", "/api/v1/assets"],
  ["get", "/api/v1/strategies/{id}"],
  ["get", "/api/v1/strategies/{id}/versions"],
  ["post", "/api/v1/strategies/{id}/configuration"],
  ["post", "/api/v1/strategies/{id}/runtime"],
  ["get", "/api/v1/risk/controls"],
  ["post", "/api/v1/risk/controls/{scope}/{id}"],
  ["get", "/api/v1/activity"],
  ["get", "/api/v1/activity/{id}"],
  ["get", "/api/v1/orders"],
  ["get", "/api/v1/orders/{id}"],
  ["get", "/api/v1/fills"],
  ["get", "/api/v1/alerts"],
  ["post", "/api/v1/alerts/{id}/acknowledge"],
  ["get", "/api/v1/reports"],
  ["post", "/api/v1/reports"],
  ["post", "/api/v1/exports"],
  ["get", "/api/v1/exports/{id}"],
  ["delete", "/api/v1/exports/{id}"],
  ["post", "/api/v1/exports/{id}/holds"],
  ["post", "/api/v1/incidents/{id}/transitions"],
  ["get", "/api/v1/configuration-revisions"],
  ["post", "/api/v1/configuration-revisions"],
  ["get", "/api/v1/lab-runs"],
  ["post", "/api/v1/lab-runs/{id}/{action}"],
  ["get", "/api/v1/qualifications"],
  ["post", "/api/v1/qualifications"],
  ["post", "/api/v1/qualifications/{id}/abort"],
  ["get", "/api/v1/users"],
  ["post", "/api/v1/users/{id}/roles"],
  ["get", "/api/v1/commands/{id}"],
];

for (const [method, path] of requiredOperations) {
  if (api.paths?.[path]?.[method] === undefined) {
    throw new Error(`missing V1D D1 contract ${method.toUpperCase()} ${path}`);
  }
}

const mutationExceptions = new Set(["POST /api/v1/authorizations"]);
for (const [method, path] of requiredOperations) {
  if (!new Set(["post", "put", "patch", "delete"]).has(method)) continue;
  const operation = api.paths[path][method];
  const refs = (operation.parameters ?? []).map((parameter) => parameter.$ref);
  for (const required of [
    "#/components/parameters/Origin",
    "#/components/parameters/CSRFToken",
  ]) {
    if (!refs.includes(required)) {
      throw new Error(`${method.toUpperCase()} ${path} omits ${required}`);
    }
  }
  if (
    !mutationExceptions.has(`${method.toUpperCase()} ${path}`) &&
    !refs.includes("#/components/parameters/IdempotencyKey")
  ) {
    throw new Error(`${method.toUpperCase()} ${path} omits idempotency`);
  }
}

for (const schema of [
  "HighRiskAuthorizationRequest",
  "D1ResourcePage",
  "ActivityPage",
  "ReasonPresentation",
  "ExportArtifact",
  "QualificationStartRequest",
  "RoleChangeRequest",
]) {
  if (api.components?.schemas?.[schema] === undefined) {
    throw new Error(`V1D D1 schema is absent: ${schema}`);
  }
}

const goSources = (directory, pattern) =>
  readdirSync(directory)
    .filter((name) => pattern.test(name) && !name.endsWith("_test.go"))
    .sort()
    .map((name) => `${directory}/${name}`);
const d1Sources = [
  ...goSources("internal/api/console", /^d1.*\.go$/),
  ...goSources("internal/storage/postgres", /^d1_console_.*\.go$/),
]
  .map(read)
  .join("\n");
for (const forbidden of [
  "internal/exchanges/binance",
  "internal/exchanges/bybit",
  "http.Client",
  "APISecret",
  "APIKey",
  "production-private",
  "production_private",
]) {
  if (d1Sources.includes(forbidden)) {
    throw new Error(
      `V1D D1 control plane contains forbidden token: ${forbidden}`,
    );
  }
}

for (const required of [
  '"real_trading_enabled": false',
  "safeD1SpreadsheetValue",
  "a11StreamAllowed",
  "RevisionBoundAuthorizationPurpose",
]) {
  const allSources =
    d1Sources +
    read("internal/storage/postgres/a11_console_stream.go") +
    goSources("internal/authentication", /^sandbox_authorization.*\.go$/)
      .map(read)
      .join("\n");
  if (!allSources.includes(required)) {
    throw new Error(`V1D D1 fail-closed boundary omits ${required}`);
  }
}

const migration = read(
  "internal/storage/postgres/migrations/000025_v1d_d1_control_plane.sql",
);
for (const required of [
  "v1d_authorization_target_revision_required",
  "CREATE TABLE v1d_activity_projection",
  "CREATE VIEW v1d_activity_explanations",
  "REVOKE ALL ON FUNCTION project_v1d_activity(text,jsonb) FROM PUBLIC",
  "CREATE TABLE v1d_export_artifacts",
  "job_id text NOT NULL UNIQUE REFERENCES jobs(id)",
  "expires_at = created_at + interval '7 days'",
  "protect_v1d_qualification_run",
]) {
  if (!migration.includes(required)) {
    throw new Error(`V1D D1 migration boundary omits ${required}`);
  }
}

console.log(
  `V1D D1 API and safety boundary passed (${requiredOperations.length} operations)`,
);
