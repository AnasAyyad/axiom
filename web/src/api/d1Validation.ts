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

const runCatalog = z
  .object({
    choices: z.array(
      z
        .object({
          strategy_id: z.string().min(1),
          strategy_name: z.string().min(1),
          strategy_version: z.string().min(1),
          mode: z.enum([
            "demonstration",
            "backtest",
            "replay",
            "shadow",
            "testnet",
            "demo",
          ]),
          exchanges: z
            .array(z.enum(["binance", "bybit"]))
            .min(1)
            .max(2),
          instrument: z.string().min(1),
          cadence: z.string().min(1),
          warmup: z.string().min(1),
          order_capable: z.boolean(),
        })
        .loose(),
    ),
    blocker: z
      .object({
        code: z.string().min(1),
        summary: z.string().min(1),
        detail: z.string().min(1),
        suggested_action: z.string().min(1),
      })
      .optional(),
  })
  .loose();

const guidedDemonstrationEvent = z
  .object({
    ordinal: z.number().int().nonnegative(),
    decision: z.string().min(2),
    orders: z.string().min(2),
    execution_events: z.string().min(2),
    balances: z.string().min(2),
  })
  .loose();

const guidedDemonstrationPage = z
  .object({
    items: z.array(
      z
        .object({
          id: z.string().min(1),
          title: z.string().min(1),
          description: z.string().min(1),
          strategy_id: z.string().min(1),
          strategy_version: z.string().min(1),
          synthetic: z.literal(true),
          expected_outcomes: z.array(z.string().min(1)).min(1),
        })
        .loose(),
    ),
  })
  .loose();

const guidedDemonstrationResult = z
  .object({
    id: z.string().min(1),
    strategy_id: z.string().min(1),
    strategy_version: z.string().min(1),
    synthetic: z.literal(true),
    advisory_only: z.boolean(),
    advisory_evidence: z.string().min(2).optional(),
    configuration_hash: z.string().regex(/^[0-9a-f]{64}$/),
    accepted: guidedDemonstrationEvent,
    rejected: guidedDemonstrationEvent,
    metrics: z.string().min(2),
    result_hash: z.string().regex(/^[0-9a-f]{64}$/),
  })
  .loose();

const run = z
  .object({
    id: z.string().min(1),
    friendly_name: z.string().min(1),
    strategy_id: z.string().min(1),
    strategy_version: z.string().min(1),
    mode: z.enum(["backtest", "replay", "shadow"]),
    environment: z.enum(["recorded_data", "production_public"]),
    state: z.string().min(1),
    order_capable: z.boolean(),
    available_actions: z
      .array(z.enum(["pause", "resume", "step", "stop"]))
      .max(4),
    revision,
    waiting_reason: z.string().min(1).optional(),
    created_at: timestamp,
    updated_at: timestamp.optional(),
  })
  .loose();

const runPage = z.object({ items: z.array(run) }).loose();

const runOutputPage = z
  .object({
    items: z.array(
      z
        .object({
          ordinal: revision,
          kind: z.enum(["event", "decision", "order", "execution"]),
          content_hash: z.string().regex(/^[0-9a-f]{64}$/),
          canonical_payload: z.string().min(2).max(1_048_576),
        })
        .loose(),
    ),
  })
  .loose();

const runPortfolioProjection = z
  .object({
    state: z.enum(["recorded", "not_recorded"]),
    ordinal: revision.optional(),
    content_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    canonical_payload: z.string().min(2).max(1_048_576).optional(),
    waiting_reason: z.string().min(1).max(500).optional(),
  })
  .loose();

const runRiskProjection = z
  .object({
    state: z.literal("not_recorded"),
    summary: z.string().min(1).max(500),
  })
  .loose();

const runEvidence = z
  .object({
    state: z.enum(["recorded", "not_recorded"]),
    manifest_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    source_commit: z
      .string()
      .regex(/^[0-9a-f]{40,64}$/)
      .optional(),
    configuration_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    dataset_manifest_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    model_namespace: z.string().min(1).max(200).optional(),
    confidence_tier: z.enum(["A", "B", "C", "D"]).optional(),
  })
  .loose();

const dataCatalogue = z
  .object({
    items: z.array(
      z
        .object({
          name: z.string().min(1),
          source: z.enum(["recorded_public_data", "approved_historical_data"]),
          state: z.enum(["building", "ready", "qualified", "rejected"]),
          quality_tier: z.enum(["unclassified", "tier_a"]).optional(),
          exchanges: z
            .array(z.enum(["binance", "bybit"]))
            .min(1)
            .max(2),
          coverage_start: timestamp,
          coverage_end: timestamp,
          segment_count: z.number().int().nonnegative(),
          known_gap_count: z.number().int().nonnegative(),
          manifest_hash: z.string().regex(/^[0-9a-f]{64}$/),
          supported_modes: z
            .array(z.enum(["backtest", "replay", "shadow"]))
            .min(1),
        })
        .loose(),
    ),
  })
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
  /^GET \/api\/v1\/(?:assets|risk\/controls|orders|fills|alerts|reports|configuration-revisions|lab-runs|qualifications)(?:\?.*)?$/;
const d1DetailPaths =
  /^GET \/api\/v1\/(?:strategies\/(?!trend$)[^/?]+|orders\/[^/?]+|commands\/[^/?]+)$/;
const d1CommandPaths =
  /^(?:POST|DELETE) \/api\/v1\/(?:strategies\/[^/?]+\/(?:configuration|runtime)|risk\/controls\/[^/?]+\/[^/?]+|alerts\/[^/?]+\/(?:acknowledge|escalate)|alert-routes\/[^/?]+\/test|reports|report-schedules(?:\/[^/?]+\/transitions)?|exports\/[^/?]+(?:\/holds)?|incidents(?:\/[^/?]+\/(?:transitions|updates))?|configuration-revisions|lab-runs\/[^/?]+\/[^/?]+|qualifications(?:\/[^/?]+\/abort)?)$/;

export const d1ResponseSchemas: ReadonlyArray<readonly [RegExp, z.ZodType]> = [
  [d1ListPaths, resourcePage],
  [d1DetailPaths, resource],
  [/^GET \/api\/v1\/strategies\/[^/?]+\/versions(?:\?.*)?$/, resourcePage],
  [/^GET \/api\/v1\/activity(?:\?.*)?$/, activityPage],
  [/^GET \/api\/v1\/activity\/[^/?]+$/, activity],
  [/^GET \/api\/v1\/run-catalog$/, runCatalog],
  [/^GET \/api\/v1\/demonstrations$/, guidedDemonstrationPage],
  [/^GET \/api\/v1\/demonstrations\/[^/?]+$/, guidedDemonstrationResult],
  [/^GET \/api\/v1\/runs$/, runPage],
  [/^GET \/api\/v1\/runs\/[^/?]+$/, run],
  [
    /^GET \/api\/v1\/runs\/[^/?]+\/(timeline|decisions|orders|fills)$/,
    runOutputPage,
  ],
  [/^GET \/api\/v1\/runs\/[^/?]+\/portfolio$/, runPortfolioProjection],
  [/^GET \/api\/v1\/runs\/[^/?]+\/risk$/, runRiskProjection],
  [/^GET \/api\/v1\/runs\/[^/?]+\/evidence$/, runEvidence],
  [/^GET \/api\/v1\/data-catalogue$/, dataCatalogue],
  [/^POST \/api\/v1\/authorizations$/, authorization],
  [/^POST \/api\/v1\/exports$/, exportArtifact],
  [/^POST \/api\/v1\/incidents\/[^/?]+\/evidence-bundles$/, exportArtifact],
  [/^GET \/api\/v1\/exports\/[^/?]+$/, exportArtifact],
  [d1CommandPaths, command],
];
