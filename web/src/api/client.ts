import type { components } from "./generated/schema";
import {
  parseAPIError,
  parseAPIResponse,
  parseAPIStreamEvent,
} from "./validation";

type SchemaName = keyof components["schemas"];
export type APIModel<Name extends SchemaName> = components["schemas"][Name];

let csrfToken = readCookie("axiom_csrf");

export class APIError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly correlationID: string,
  ) {
    super(code);
  }
}

export function setCSRFToken(value: string) {
  csrfToken = value;
}

export function parseStreamEvent(value: string) {
  return parseAPIStreamEvent(value);
}

export async function getAPI<Name extends SchemaName>(
  path: string,
): Promise<APIModel<Name>> {
  return request<APIModel<Name>>(path, { method: "GET" });
}

export async function postAPI<Name extends SchemaName>(
  path: string,
  body: unknown,
  idempotencyKey?: string,
): Promise<APIModel<Name>> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };
  if (csrfToken !== "") headers["X-CSRF-Token"] = csrfToken;
  if (idempotencyKey !== undefined) headers["Idempotency-Key"] = idempotencyKey;
  return request<APIModel<Name>>(path, {
    method: "POST",
    headers,
    body: JSON.stringify(body),
  });
}

async function request<T>(path: string, init: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, credentials: "same-origin" });
  if (!response.ok) {
    const parsed = parseAPIError(await safeJSON(response));
    throw new APIError(
      response.status,
      parsed.success ? parsed.data.code : "invalid_server_response",
      parsed.success ? parsed.data.correlation_id : "unavailable",
    );
  }
  if (response.status === 204) return undefined as T;
  const key = `${init.method ?? "GET"} ${path}`;
  const parsed = parseAPIResponse(key, await safeJSON(response));
  if (parsed === undefined)
    throw new APIError(502, "unvalidated_server_response", "unavailable");
  if (!parsed.success)
    throw new APIError(502, "invalid_server_response", "unavailable");
  return parsed.data as T;
}

async function safeJSON(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return null;
  }
}

export function newIdempotencyKey(prefix: string) {
  return `${prefix}-${crypto.randomUUID()}`;
}

function readCookie(name: string) {
  const prefix = `${encodeURIComponent(name)}=`;
  const match = document.cookie
    .split("; ")
    .find((part) => part.startsWith(prefix));
  return match ? decodeURIComponent(match.slice(prefix.length)) : "";
}
