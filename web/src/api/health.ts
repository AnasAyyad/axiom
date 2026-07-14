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
  if (value.phase !== "A1") {
    throw new Error("invalid_health_response");
  }
  return {
    status,
    role: requiredString(value, "role"),
    phase: "A1",
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
  if (
    value.real_trading_enabled !== false ||
    value.release !== "V1A" ||
    value.phase !== "A1" ||
    value.strategy_activation !== "unavailable" ||
    (lifecycle !== "STARTING" &&
      lifecycle !== "READY_PAUSED" &&
      lifecycle !== "STOPPING")
  ) {
    throw new Error("unsafe_system_status");
  }
  return {
    release: "V1A",
    phase: "A1",
    role: requiredString(value, "role"),
    lifecycle_state: lifecycle,
    strategy_activation: "unavailable",
    real_trading_enabled: false,
  };
}
