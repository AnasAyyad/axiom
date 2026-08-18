import { z } from "zod";

import { revision, timestamp } from "./validationShared";

const campaignState = z.enum([
  "PENDING",
  "RUNNING",
  "PAUSED_RECOVERABLE",
  "COMPLETED",
  "PARTIAL",
  "BLOCKED",
  "CANCELED",
]);
const stageName = z.enum([
  "HISTORICAL_IMPORT",
  "EXISTING_DATA_AUDIT",
  "RECORDER_ROTATION",
  "RECORDER_QUALIFICATION",
  "BACKTEST_MATRIX",
  "REPLAY_MATRIX",
  "CANDIDATE_SELECTION",
  "COMBINED_SHADOW",
  "FINAL_REPORT",
]);
const member = z.object({
  id: z.string().min(1),
  strategy: z.enum([
    "trend-following",
    "mean-reversion",
    "triangular-arbitrage",
    "cross-exchange-arbitrage",
    "inventory-rebalancing",
  ]),
  configuration: z.string().min(1),
  mode: z.enum(["backtest", "replay", "shadow", "advisory"]),
  capital_micros: z.number().int().nonnegative(),
  repeat_ordinal: z.number().int().min(0).max(2),
  cost_stress_bps: z.number().int().min(10_000).max(20_000),
  state: z.enum([
    "PENDING",
    "QUEUED",
    "RUNNING",
    "SUCCEEDED",
    "FAILED",
    "EXCLUDED",
    "CANCELED",
  ]),
  verdict: z.enum(["CONTINUE", "IMPROVE", "REJECT", "BLOCKED"]).optional(),
  reason_code: z.string().min(1).optional(),
  result_hash: z
    .string()
    .regex(/^[0-9a-f]{64}$/)
    .optional(),
  metrics: z.record(z.string(), z.unknown()).optional(),
});
const stageAttempt = z.object({
  attempt: z.number().int().positive(),
  outcome: z.enum(["PAUSED_RECOVERABLE", "COMPLETED", "BLOCKED"]),
  reason_code: z.string().min(1).optional(),
  summary: z.string().min(1).max(500),
  checkpoint_hash: z
    .string()
    .regex(/^[0-9a-f]{64}$/)
    .optional(),
  linked_resource_type: z.string().min(1).optional(),
  linked_resource_id: z.string().min(1).optional(),
  started_at: timestamp,
  finished_at: timestamp,
  retry_at: timestamp.optional(),
});
const campaign = z.object({
  id: z.string().min(1),
  preset: z.literal("balanced_full_v1"),
  state: campaignState,
  current_stage: stageName.optional(),
  completed_stages: z.array(stageName).max(9),
  valid_recording_seconds: z.number().int().nonnegative().optional(),
  valid_shadow_seconds: z.number().int().nonnegative().optional(),
  wall_time_seconds: z.number().int().nonnegative().optional(),
  estimated_remaining_seconds: z.number().int().nonnegative().optional(),
  recorded_bytes: z.number().int().nonnegative().optional(),
  recording_limit_bytes: z.literal(214_748_364_800).optional(),
  measured_bytes_per_hour: z.number().int().nonnegative().optional(),
  shadow_reserved_bytes: z.number().int().nonnegative().optional(),
  recording_last_valid_at: timestamp.optional(),
  shadow_last_valid_at: timestamp.optional(),
  stages: z
    .array(
      z.object({
        stage: stageName,
        state: z.enum([
          "PENDING",
          "RUNNING",
          "PAUSED_RECOVERABLE",
          "COMPLETED",
          "BLOCKED",
          "CANCELED",
        ]),
        attempt: z.number().int().nonnegative(),
        recoverable_failures: z.number().int().nonnegative(),
        reason_code: z.string().min(1).optional(),
        started_at: timestamp.optional(),
        attempt_started_at: timestamp.optional(),
        next_retry_at: timestamp.optional(),
        completed_at: timestamp.optional(),
        updated_at: timestamp,
        attempts: z.array(stageAttempt).max(100).optional(),
      }),
    )
    .max(9)
    .optional(),
  historical_imports: z
    .array(
      z.object({
        exchange: z.enum(["binance", "bybit"]),
        instrument: z.enum(["BTC/USDT", "ETH/USDT"]),
        interval: z.enum(["15m", "1h", "4h"]),
        state: z.enum(["PENDING", "RUNNING", "COMPLETED", "BLOCKED"]),
        window_start: timestamp,
        window_end: timestamp,
        checkpoint_time: timestamp,
        row_count: z.number().int().nonnegative(),
        byte_count: z.number().int().nonnegative(),
        gap_count: z.number().int().nonnegative(),
        reason_code: z.string().min(1).optional(),
      }),
    )
    .max(12)
    .optional(),
  coverage: z
    .array(
      z.object({
        dataset_id: z.string().min(1),
        exchange: z.string().min(1).optional(),
        instrument: z.string().min(1).optional(),
        eligibility: z.enum(["eligible", "ineligible", "blocked"]),
        reason_code: z.string(),
        segment_count: z.number().int().nonnegative(),
        byte_count: z.number().int().nonnegative(),
        gap_count: z.number().int().nonnegative(),
        duplicate_count: z.number().int().nonnegative(),
      }),
    )
    .max(500)
    .optional(),
  matrix: z.array(member).max(200).optional(),
  feed_health: z
    .array(
      z.object({
        exchange: z.enum(["binance", "bybit"]),
        instrument: z.enum(["BTCUSDT", "ETHUSDT", "ETHBTC"]),
        eligible: z.boolean(),
        book_fresh: z.boolean(),
        clock_eligible: z.boolean(),
        latest_event_at: timestamp,
        message_count: z.number().int().nonnegative(),
        queue_drop_count: z.number().int().nonnegative(),
        gap_count: z.number().int().nonnegative(),
        decoder_error_count: z.number().int().nonnegative(),
      }),
    )
    .max(6)
    .optional(),
  shadow: z
    .object({
      state: z.enum([
        "PENDING",
        "RUNNING",
        "PAUSED_RECOVERABLE",
        "COMPLETED",
        "BLOCKED",
        "CANCELED",
      ]),
      valid_seconds: z.number().int().nonnegative(),
      start_ordinal: z.number().int().nonnegative(),
      last_processed_ordinal: z.number().int().nonnegative(),
      shared_capital_micros: z.number().int().nonnegative(),
      protected_reserve_micros: z.number().int().nonnegative(),
      member_ceiling_micros: z.number().int().nonnegative(),
      reason_code: z.string().min(1).optional(),
      members: z.array(member).max(4),
    })
    .optional(),
  reason_code: z.string().min(1).optional(),
  suggested_action: z.string().min(1).optional(),
  revision,
  created_at: timestamp,
  updated_at: timestamp,
});
const eventPage = z.object({
  items: z.array(
    z.object({
      ordinal: revision,
      event_type: z.string().min(1),
      stage: z.string().min(1).optional(),
      reason_code: z.string().min(1).optional(),
      summary: z.string().min(1).optional(),
      occurred_at: timestamp,
    }),
  ),
});
const report = z.object({
  state: z.enum(["not_ready", "final", "partial"]),
  verdict: z.enum(["CONTINUE", "IMPROVE", "REJECT", "BLOCKED"]).optional(),
  reason_code: z.string().min(1).optional(),
  summary: z.string().min(1).optional(),
  report_hash: z
    .string()
    .regex(/^[0-9a-f]{64}$/)
    .optional(),
  content: z.record(z.string(), z.unknown()).optional(),
  generated_at: timestamp,
});
const dataAudit = z.object({
  id: z.string().min(1),
  state: z.enum(["PENDING", "RUNNING", "COMPLETED", "BLOCKED"]),
  reason_code: z.string().min(1).optional(),
  created_at: timestamp,
  completed_at: timestamp.optional(),
});
const command = z.object({
  id: z.string().min(1),
  target_id: z.string().min(1),
  state: z.enum(["pending", "applied", "rejected", "failed"]),
  revision,
  correlation_id: z.string().min(1),
  created_at: timestamp,
});

export const evaluationResponseSchemas: ReadonlyArray<
  readonly [RegExp, z.ZodType]
> = [
  [
    /^GET \/api\/v1\/evaluation-campaigns$/,
    z.object({ items: z.array(campaign).max(100) }),
  ],
  [/^POST \/api\/v1\/evaluation-campaigns$/, campaign],
  [/^POST \/api\/v1\/evaluation-campaigns\/[^/?]+\/cancel$/, command],
  [/^GET \/api\/v1\/evaluation-campaigns\/[^/?]+$/, campaign],
  [/^GET \/api\/v1\/evaluation-campaigns\/[^/?]+\/events$/, eventPage],
  [/^GET \/api\/v1\/evaluation-campaigns\/[^/?]+\/report$/, report],
  [/^POST \/api\/v1\/data-audits$/, dataAudit],
  [/^GET \/api\/v1\/data-audits\/[^/?]+$/, dataAudit],
];
