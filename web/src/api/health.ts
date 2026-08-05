import type { components } from "./generated/schema";

export type HealthResponse = components["schemas"]["HealthResponse"];
export type BuildInformation = components["schemas"]["BuildInformation"];
export type SystemStatus = components["schemas"]["SystemStatus"];
export type VersionResponse = components["schemas"]["VersionResponse"];

async function requestJSON(path: string): Promise<unknown> {
  const response = await fetch(path, {
    headers: { Accept: "application/json" },
    cache: "no-store",
  });
  const body: unknown = await response.json();
  if (!response.ok && response.status !== 503) {
    throw new Error("health_request_failed");
  }
  return body;
}

function record(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("invalid_health_response");
  }
  return value as Record<string, unknown>;
}

function requiredString(value: Record<string, unknown>, key: string): string {
  const field = value[key];
  if (typeof field !== "string" || field.length === 0) {
    throw new Error("invalid_health_response");
  }
  return field;
}

export async function getReadiness(): Promise<HealthResponse> {
  const value = record(await requestJSON("/health/ready"));
  const status = requiredString(value, "status");
  if (status !== "ready" && status !== "not_ready") {
    throw new Error("invalid_health_response");
  }
  return {
    status,
    ...(typeof value.reason_code === "string"
      ? { reason_code: value.reason_code }
      : {}),
  };
}

export async function getBuild(): Promise<BuildInformation> {
  const value = record(await requestJSON("/api/v1/system/build"));
  if (typeof value.dirty !== "boolean") {
    throw new Error("invalid_build_response");
  }
  return {
    version: requiredString(value, "version"),
    commit: requiredString(value, "commit"),
    built_at: requiredString(value, "built_at"),
    go_version: requiredString(value, "go_version"),
    dirty: value.dirty,
  };
}

export async function getStatus(): Promise<SystemStatus> {
  const value = record(await requestJSON("/api/v1/system/status"));
  const lifecycle = requiredString(value, "lifecycle_state");
  const readiness = requiredString(value, "readiness_state");
  const activation = requiredString(value, "strategy_activation");
  if (
    value.real_trading_enabled !== false ||
    !["ready", "blocked", "degraded"].includes(readiness) ||
    !["unavailable", "trend.v1a.1"].includes(activation) ||
    (lifecycle !== "STARTING" &&
      lifecycle !== "READY_PAUSED" &&
      lifecycle !== "STOPPING" &&
      lifecycle !== "RUNNING" &&
      lifecycle !== "DEGRADED")
  ) {
    throw new Error("unsafe_system_status");
  }
  return {
    application_version: requiredString(value, "application_version"),
    build_commit: requiredString(value, "build_commit"),
    configuration_identity: requiredString(value, "configuration_identity"),
    readiness_state: readiness as SystemStatus["readiness_state"],
    lifecycle_state: lifecycle,
    strategy_activation: activation as SystemStatus["strategy_activation"],
    real_trading_enabled: false,
  };
}
