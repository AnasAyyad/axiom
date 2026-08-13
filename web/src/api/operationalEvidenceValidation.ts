import { z } from "zod";

import { page, revision, timestamp } from "./validationShared";

const reportProvenance = z
  .object({
    mode: z.string(),
    confidence_tier: z.string(),
    valuation_basis: z.string(),
    model_provenance: z.record(z.string(), z.string()),
    maturity: z.string(),
    source_identity: z.string(),
    source_revision: revision,
  })
  .strict();
const report = z
  .object({
    id: z.string(),
    job_id: z.string(),
    schedule_id: z.string().optional(),
    report_type: z.string(),
    state: z.string(),
    provenance: reportProvenance,
    generated_at: timestamp.optional(),
    content_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    failure_code: z.string().optional(),
    created_at: timestamp,
    revision,
  })
  .loose();
const schedule = z
  .object({
    id: z.string(),
    report_type: z.string(),
    frequency: z.string(),
    minute_utc: z.number().int(),
    hour_utc: z.number().int().optional(),
    weekday_utc: z.number().int().optional(),
    state: z.string(),
    next_run_at: timestamp,
    last_run_at: timestamp.optional(),
    revision,
    created_at: timestamp,
    updated_at: timestamp,
  })
  .loose();
const delivery = z
  .object({
    id: z.string(),
    sink_name: z.string(),
    attempt: z.number().int().positive(),
    state: z.string(),
    reason_code: z.string().optional(),
    started_at: timestamp,
    completed_at: timestamp,
    latency_ms: z.number().int().nonnegative().optional(),
  })
  .loose();
const alert = z
  .object({
    id: z.string(),
    severity: z.string(),
    reason_code: z.string(),
    component: z.string(),
    state: z.string(),
    occurrences: z.number().int().positive(),
    revision,
    correlation_id: z.string(),
    incident_id: z.string().optional(),
    created_at: timestamp,
    last_seen_at: timestamp,
    deliveries: z.array(delivery),
    escalations: z.array(z.object({ id: z.string() }).loose()),
  })
  .loose();
const routes = z
  .object({
    items: z.array(
      z
        .object({
          id: z.string(),
          sink_name: z.string(),
          enabled: z.boolean(),
          minimum_severity: z.string(),
          revision,
        })
        .loose(),
    ),
    revision,
  })
  .loose();
const verification = z
  .object({
    verdict: z.enum(["valid", "broken"]),
    checked_events: z.number().int().nonnegative(),
    head_hash: z.string(),
    first_broken_sequence: z.number().int().positive().optional(),
    reason_code: z.string().optional(),
    verified_at: timestamp,
  })
  .loose();

export const operationalEvidenceResponseSchemas: ReadonlyArray<
  readonly [RegExp, z.ZodType]
> = [
  [/^GET \/api\/v1\/reports\/[^/?]+$/, report],
  [/^GET \/api\/v1\/report-schedules(?:\?.*)?$/, page(schedule)],
  [/^GET \/api\/v1\/alerts\/[^/?]+$/, alert],
  [/^GET \/api\/v1\/alert-routes$/, routes],
  [/^GET \/api\/v1\/audit-verification$/, verification],
];
