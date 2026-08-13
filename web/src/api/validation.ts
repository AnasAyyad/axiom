import { z } from "zod";

import { multiExchangeConsoleResponseSchemas } from "./multiExchangeConsoleValidation";
import { sandboxQualificationResponseSchemas } from "./sandboxQualificationValidation";
import { ownerControlResponseSchemas } from "./ownerControlValidation";
import { operationalEvidenceResponseSchemas } from "./operationalEvidenceValidation";
import { legacyResponseSchemas } from "./legacyValidation";
import { revision, timestamp } from "./validationShared";
import { evaluationResponseSchemas } from "./evaluationValidation";

const errorSchema = z.object({
  code: z.string(),
  correlation_id: z.string(),
  message: z.string(),
  summary: z.string().min(1).optional(),
  detail: z.string().min(1).optional(),
  impact: z.string().min(1).optional(),
  suggested_action: z.string().min(1).optional(),
  current_state: z.string().min(1).optional(),
  required_state: z.string().min(1).optional(),
  blocking_prerequisites: z.array(z.string().min(1)).optional(),
});
const sessionUser = z
  .object({
    id: z.string().min(1),
    email: z.email(),
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
  ...evaluationResponseSchemas,
  ...ownerControlResponseSchemas,
  ...operationalEvidenceResponseSchemas,
  ...sandboxQualificationResponseSchemas,
  ...multiExchangeConsoleResponseSchemas,
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
