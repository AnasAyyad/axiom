import { z } from "zod";

import { jobSchema } from "./researchValidation";
import {
  decimal,
  nonnegativeDecimal,
  page,
  revision,
  timestamp,
} from "./validationShared";

const canonicalJSON = z
  .string()
  .min(2)
  .max(1_048_576)
  .refine((value) => {
    try {
      JSON.parse(value);
      return true;
    } catch {
      return false;
    }
  });
const safeAuditJSON = canonicalJSON.max(2_000);
const command = z
  .object({
    id: z.string().min(1),
    state: z.enum(["pending", "applied", "rejected", "failed"]),
    target_id: z.string(),
    revision,
    correlation_id: z.string().min(1),
    created_at: timestamp,
  })
  .loose();
const shadow = z
  .object({
    id: z.string().min(1),
    state: z.enum([
      "QUEUED",
      "RUNNING",
      "PAUSED",
      "CANCEL_REQUESTED",
      "CANCELED",
      "FAILED",
    ]),
    label: z.literal("PUBLIC-LIVE SHADOW / VIRTUAL"),
    public_only: z.literal(true),
    simulation_only: z.literal(true),
    entries_enabled: z.boolean(),
    revision,
    created_at: timestamp,
  })
  .loose();

export const legacyResponseSchemas: ReadonlyArray<
  readonly [RegExp, z.ZodType]
> = [
  [
    /^GET \/api\/v1\/exchanges\/binance\/health$/,
    z
      .object({
        environment: z.literal("production_public"),
        public_only: z.literal(true),
        websocket_state: z.string(),
        book_state: z.string(),
        recorder_state: z.string(),
        observed_at: timestamp,
        revision,
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/exchanges\/binance\/instruments/,
    page(
      z
        .object({
          id: z.string(),
          symbol: z.string(),
          price_tick: nonnegativeDecimal,
          quantity_step: nonnegativeDecimal,
          minimum_notional: nonnegativeDecimal,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/portfolios\?/,
    page(
      z
        .object({
          id: z.string(),
          mode: z.string(),
          label: z.string(),
          equity: nonnegativeDecimal,
          available: nonnegativeDecimal,
          reserved: nonnegativeDecimal,
          revision,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/portfolios\/[^/]+\/journal/,
    page(
      z
        .object({
          id: z.string(),
          transaction_id: z.string(),
          asset: z.string(),
          direction: z.string(),
          quantity: nonnegativeDecimal,
          occurred_at: timestamp,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/portfolios\/[^/]+$/,
    z
      .object({
        id: z.string(),
        mode: z.string(),
        label: z.string(),
        equity: nonnegativeDecimal,
        available: nonnegativeDecimal,
        reserved: nonnegativeDecimal,
        balances: z.array(
          z
            .object({
              asset: z.string(),
              available: nonnegativeDecimal,
              reserved: nonnegativeDecimal,
            })
            .loose(),
        ),
        positions: z.array(
          z
            .object({
              instrument: z.string(),
              quantity: nonnegativeDecimal,
              realized_pnl: decimal,
              unrealized_pnl: decimal,
            })
            .loose(),
        ),
        revision,
        updated_at: timestamp,
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/risk\/status$/,
    z
      .object({
        state: z.enum(["NORMAL", "CAUTIOUS", "PAUSED", "LOCKED"]),
        policy_version: revision,
        recovery_ready: z.boolean(),
        contributors: z.array(
          z
            .object({
              name: z.string(),
              usage: nonnegativeDecimal,
              limit: nonnegativeDecimal,
              reason_code: z.string(),
            })
            .loose(),
        ),
        revision,
        updated_at: timestamp,
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/strategies\/trend\/decisions/,
    page(
      z
        .object({
          id: z.string(),
          outcome: z.string(),
          reason_code: z.string(),
          explanation: z.string(),
          candle_view_id: z.string(),
          market_view_id: z.string(),
          occurred_at: timestamp,
          revision,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/strategies\/trend$/,
    z
      .object({
        version: z.literal("trend.v1a.1"),
        timeframe: z.literal("4h"),
        health: z.string(),
        evidence_maturity: z.string(),
        parameters: z
          .array(
            z
              .object({
                id: z.string(),
                value: z.string(),
                unit: z.string(),
                cadence: z.string(),
                mutability: z.literal("immutable_per_run"),
              })
              .loose(),
          )
          .length(16),
        revision,
      })
      .loose(),
  ],
  [/^GET \/api\/v1\/(backtests|replays)\//, jobSchema],
  [/^POST \/api\/v1\/(backtests|replays)$/, jobSchema],
  [/^GET \/api\/v1\/shadow-sessions\//, shadow],
  [/^POST \/api\/v1\/shadow-sessions$/, shadow],
  [
    /^GET \/api\/v1\/incidents\?/,
    page(
      z
        .object({
          id: z.string(),
          severity: z.string(),
          state: z.string(),
          reason_code: z.string(),
          opened_at: timestamp,
          revision,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/incidents\//,
    z
      .object({
        id: z.string(),
        severity: z.string(),
        state: z.string(),
        reason_code: z.string(),
        opened_at: timestamp,
        timeline: z.array(
          z
            .object({
              id: z.string(),
              event_type: z.string(),
              occurred_at: timestamp,
              correlation_id: z.string(),
              redacted: z.boolean(),
              safe_detail: safeAuditJSON.optional(),
            })
            .loose()
            .superRefine((value, context) => {
              if (!value.redacted && value.safe_detail === undefined) {
                context.addIssue({
                  code: "custom",
                  message: "authorized evidence detail missing",
                });
              }
            }),
        ),
        replay_window: z.object({
          dataset_id: z.string(),
          first_ordinal: revision,
          last_ordinal: revision,
        }),
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/audit-events/,
    page(
      z
        .object({
          id: z.string(),
          event_type: z.string(),
          actor: z.string(),
          correlation_id: z.string(),
          recorded_at: timestamp,
          redacted: z.boolean(),
          safe_detail: safeAuditJSON.optional(),
        })
        .loose()
        .superRefine((value, context) => {
          if (!value.redacted && value.safe_detail === undefined) {
            context.addIssue({
              code: "custom",
              message: "authorized audit detail missing",
            });
          }
        }),
    ),
  ],
  [/^POST \/api\/v1\/(risk|replays\/[^/]+|shadow-sessions\/[^/]+)\//, command],
];
