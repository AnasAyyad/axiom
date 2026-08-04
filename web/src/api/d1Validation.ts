import { z } from "zod";

import { page, revision, timestamp } from "./validationShared";

const reason = z
  .object({
    code: z.string().min(1),
    summary: z.string().min(1),
    explanation: z.string().min(1),
    suggested_action: z.string().min(1),
    severity: z.enum(["info", "warning", "error", "critical"]),
    unknown: z.boolean(),
    version: revision,
  })
  .loose();

const resource = z
  .object({
    id: z.string().min(1),
    kind: z.string().min(1),
    state: z.string().min(1),
    revision,
    correlation_id: z.string().min(1),
    occurred_at: timestamp.optional(),
    attributes: z.record(z.string(), z.unknown()),
    links: z.record(z.string(), z.string()),
    reason: reason.optional(),
  })
  .loose();

const resourcePage = page(resource)
  .extend({ snapshot_revision: revision })
  .loose();

const activity = z
  .object({
    id: z.string().min(1),
    activity_revision: revision,
    view: z.enum(["decisions_orders", "system_events"]),
    source_type: z.string().min(1),
    source_id: z.string().min(1),
    source_revision: z.string().min(1),
    outcome: z.string().min(1),
    strategy_id: z.string().optional(),
    instrument_id: z.string().optional(),
    exchange_id: z.string().optional(),
    side: z.enum(["buy", "sell"]).optional(),
    mode: z
      .enum(["backtest", "replay", "paper", "shadow", "testnet", "demo"])
      .optional(),
    reason,
    correlation_id: z.string().min(1),
    causation_id: z.string().optional(),
    occurred_at: timestamp,
    details: z.record(z.string(), z.unknown()),
    links: z.record(z.string(), z.string()),
  })
  .loose();

const activityPage = page(activity)
  .extend({ snapshot_revision: revision })
  .loose();

const command = z
  .object({
    id: z.string().min(1),
    target_id: z.string().min(1),
    state: z.enum(["pending", "applied", "rejected", "failed"]),
    revision,
    correlation_id: z.string().min(1),
    created_at: timestamp,
  })
  .loose();

const authorization = z
  .object({
    token: z.string().min(16),
    purpose: z.enum([
      "strategy_configuration",
      "risk_control",
      "qualification_start",
      "configuration_activation",
      "role_change",
      "artifact_hold",
    ]),
    target_revision: revision,
    expires_at: timestamp,
  })
  .loose();

const exportArtifact = z
  .object({
    id: z.string().min(1),
    command_id: z.string().min(1),
    job_id: z.string().min(1),
    resource_type: z.string().min(1),
    resource_id: z.string().min(1),
    format: z.enum(["txt", "csv", "json", "jsonl"]),
    content_type: z.string().min(1),
    content: z.string().optional(),
    content_hash: z.string().min(1),
    size_bytes: revision,
    redaction_version: z.string().min(1),
    created_at: timestamp,
    expires_at: timestamp,
    held: z.boolean(),
    deleted: z.boolean(),
    revision,
  })
  .loose();

const d1ListPaths =
  /^GET \/api\/v1\/(?:assets|risk\/controls|orders|fills|alerts|reports|configuration-revisions|lab-runs|qualifications|users)(?:\?.*)?$/;
const d1DetailPaths =
  /^GET \/api\/v1\/(?:strategies\/(?!trend$)[^/?]+|orders\/[^/?]+|commands\/[^/?]+)$/;
const d1CommandPaths =
  /^(?:POST|DELETE) \/api\/v1\/(?:strategies\/[^/?]+\/(?:configuration|runtime)|risk\/controls\/[^/?]+\/[^/?]+|alerts\/[^/?]+\/(?:acknowledge|escalate)|alert-routes\/[^/?]+\/test|reports|report-schedules(?:\/[^/?]+\/transitions)?|exports\/[^/?]+(?:\/holds)?|incidents(?:\/[^/?]+\/(?:transitions|updates))?|configuration-revisions|lab-runs\/[^/?]+\/[^/?]+|qualifications(?:\/[^/?]+\/abort)?|users\/[^/?]+\/roles)$/;

export const d1ResponseSchemas: ReadonlyArray<readonly [RegExp, z.ZodType]> = [
  [d1ListPaths, resourcePage],
  [d1DetailPaths, resource],
  [/^GET \/api\/v1\/strategies\/[^/?]+\/versions(?:\?.*)?$/, resourcePage],
  [/^GET \/api\/v1\/activity(?:\?.*)?$/, activityPage],
  [/^GET \/api\/v1\/activity\/[^/?]+$/, activity],
  [/^POST \/api\/v1\/authorizations$/, authorization],
  [/^POST \/api\/v1\/exports$/, exportArtifact],
  [/^POST \/api\/v1\/incidents\/[^/?]+\/evidence-bundles$/, exportArtifact],
  [/^GET \/api\/v1\/exports\/[^/?]+$/, exportArtifact],
  [d1CommandPaths, command],
];
