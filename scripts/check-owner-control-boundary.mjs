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
  ["get", "/api/v1/session/me"],
  ["get", "/api/v1/commands/{id}"],
];

for (const [method, path] of requiredOperations) {
  if (api.paths?.[path]?.[method] === undefined) {
    throw new Error(
      `missing owner-control contract ${method.toUpperCase()} ${path}`,
    );
  }
}

for (const [method, path] of [
  ["get", "/api/v1/users"],
  ["post", "/api/v1/users/{id}/roles"],
]) {
  if (api.paths?.[path]?.[method] !== undefined) {
    throw new Error(
      `obsolete multi-user contract remains ${method.toUpperCase()} ${path}`,
    );
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
  "OwnerControlResourcePage",
  "ActivityPage",
  "ReasonPresentation",
  "ExportArtifact",
  "QualificationStartRequest",
  "SessionUser",
  "SessionMe",
]) {
  if (api.components?.schemas?.[schema] === undefined) {
    throw new Error(`owner-control schema is absent: ${schema}`);
  }
}
if (api.components?.schemas?.RoleChangeRequest !== undefined) {
  throw new Error("obsolete RoleChangeRequest schema remains public");
}

const goSources = (directory, pattern) =>
  readdirSync(directory)
    .filter((name) => pattern.test(name) && !name.endsWith("_test.go"))
    .sort()
    .map((name) => `${directory}/${name}`);
const ownerControlSources = [
  ...goSources("internal/api/console", /^owner_control.*\.go$/),
  ...goSources("internal/storage/postgres", /^owner_control_console_.*\.go$/),
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
  if (ownerControlSources.includes(forbidden)) {
    throw new Error(
      `owner-control plane contains forbidden token: ${forbidden}`,
    );
  }
}

for (const required of [
  '"real_trading_enabled": false',
  "safeOwnerControlSpreadsheetValue",
  "ownerConsoleStreamAllowed",
  "RevisionBoundAuthorizationPurpose",
]) {
  const allSources =
    ownerControlSources +
    read("internal/storage/postgres/owner_console_stream.go") +
    goSources("internal/authentication", /^sandbox_authorization.*\.go$/)
      .map(read)
      .join("\n");
  if (!allSources.includes(required)) {
    throw new Error(`owner-control fail-closed boundary omits ${required}`);
  }
}

const migration = read(
  "internal/storage/postgres/migrations/000025_v1d_d1_control_plane.sql",
);
for (const required of [
  "v1d_authorization_target_revision_required", // semantic-naming: historical-reference
  "CREATE TABLE v1d_activity_projection", // semantic-naming: historical-reference
  "CREATE VIEW v1d_activity_explanations", // semantic-naming: historical-reference
  "REVOKE ALL ON FUNCTION project_v1d_activity(text,jsonb) FROM PUBLIC", // semantic-naming: historical-reference
  "CREATE TABLE v1d_export_artifacts", // semantic-naming: historical-reference
  "job_id text NOT NULL UNIQUE REFERENCES jobs(id)",
  "expires_at = created_at + interval '7 days'",
  "protect_v1d_qualification_run", // semantic-naming: historical-reference
]) {
  if (!migration.includes(required)) {
    throw new Error(`owner-control migration boundary omits ${required}`);
  }
}

const ownerBoundary = read(
  "internal/storage/postgres/migrations/000036_single_owner_authorization_boundary.sql",
);
for (const required of [
  "authorization_permissions_historical",
  "authorization_roles_historical",
  "role_permissions_historical",
  "user_roles_historical",
  "owner_accounts_immutable",
]) {
  if (!ownerBoundary.includes(required)) {
    throw new Error(`single-owner boundary omits ${required}`);
  }
}

console.log(
  `Owner-control API and safety boundary passed (${requiredOperations.length} operations)`,
);
