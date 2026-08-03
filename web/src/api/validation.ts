import { z } from "zod";

import { b8ResponseSchemas } from "./b8Validation";
import { c6ResponseSchemas } from "./c6Validation";
import { legacyResponseSchemas } from "./legacyValidation";
import { revision, timestamp } from "./validationShared";

const errorSchema = z.object({
  code: z.string(),
  correlation_id: z.string(),
  message: z.string(),
});
const sessionUser = z
  .object({
    id: z.string().min(1),
    email: z.email(),
    roles: z.array(z.string()),
    permissions: z.array(z.string()),
  })
  .loose();
const sessionResponseSchemas: ReadonlyArray<readonly [RegExp, z.ZodType]> = [
  [
    /^POST \/api\/v1\/session\/login$/,
    z
      .object({
        user: sessionUser,
        csrf_token: z.string().min(32),
        expires_at: timestamp,
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/session\/me$/,
    z
      .object({
        user: sessionUser,
        session_id: z.string().min(1),
        session_revision: revision,
        reauthenticated_at: timestamp,
      })
      .loose(),
  ],
];
const responseSchemas = [
  ...sessionResponseSchemas,
  ...c6ResponseSchemas,
  ...b8ResponseSchemas,
  ...legacyResponseSchemas,
];
const streamEventSchema = z
  .object({
    id: z.string().min(1),
    stream: z.string().min(1),
    schema_version: z.literal("axiom.stream.v1"),
    revision,
    entity_revision: revision,
    occurred_at: timestamp,
    correlation_id: z.string().min(1),
    causation_id: z.string().min(1),
    event_type: z.string().min(1),
    payload: z.record(z.string(), z.unknown()),
  })
  .loose();

export function parseAPIError(value: unknown) {
  return errorSchema.safeParse(value);
}

export function parseAPIResponse(key: string, value: unknown) {
  const schema = responseSchemas.find(([pattern]) => pattern.test(key))?.[1];
  if (schema === undefined) return undefined;
  return schema.safeParse(value);
}

export function parseAPIStreamEvent(value: string) {
  return streamEventSchema.safeParse(JSON.parse(value));
}
