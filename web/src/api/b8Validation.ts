import { z } from "zod";

import {
  decimal,
  nonnegativeDecimal,
  quality,
  revision,
  snapshotPage,
  timestamp,
} from "./validationShared";

const opportunity = z
  .object({
    id: z.string().min(1),
    kind: z.enum(["triangular", "cross_exchange"]),
    label: z.string().min(1),
    expected_profit: decimal,
    worst_case_profit: decimal,
    maximum_size: nonnegativeDecimal,
    tested_size: nonnegativeDecimal,
    status: z.string().min(1),
    simulation_only: z.literal(true),
    strategy_version: z.string().min(1),
    quality,
    recorded_at: timestamp,
    revision,
  })
  .loose();

const replayFault = z
  .object({
    id: z.string().min(1),
    replay_id: z.string().min(1),
    fault: z.string().min(1),
    ordinal: revision,
    delay_nanos: revision,
    repeatable: z.boolean(),
    revision,
    simulation_only: z.literal(true),
    created_at: timestamp,
  })
  .loose();

export const b8ResponseSchemas: ReadonlyArray<readonly [RegExp, z.ZodType]> = [
  [
    /^GET \/api\/v1\/system\/status$/,
    z
      .object({
        release: z.enum(["V1A", "V1B"]),
        phase: z.enum(["A11", "B8"]),
        role: z.string(),
        lifecycle_state: z.string(),
        strategy_activation: z.string(),
        real_trading_enabled: z.literal(false),
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/exchanges\?/,
    snapshotPage(
      z
        .object({
          id: z.string().min(1),
          name: z.string().min(1),
          environment: z.literal("production_public"),
          public_only: z.literal(true),
          websocket_state: z.string(),
          book_state: z.string(),
          recorder_state: z.string(),
          capabilities: z.array(z.string()),
          quality,
          revision,
        })
        .loose(),
    ),
  ],
  [/^GET \/api\/v1\/opportunities\?/, snapshotPage(opportunity)],
  [
    /^GET \/api\/v1\/opportunities\/[^/?]+$/,
    z
      .object({
        summary: opportunity,
        legs: z.array(
          z
            .object({
              index: z.number().int().nonnegative(),
              exchange: z.string(),
              instrument: z.string(),
              side: z.enum(["buy", "sell"]),
              state: z.string(),
              input_quantity: nonnegativeDecimal,
              trade_quantity: nonnegativeDecimal,
              net_output: nonnegativeDecimal,
              fee_quote_equivalent: nonnegativeDecimal,
              revision,
            })
            .loose(),
        ),
        inventory: z.array(
          z.object({ exchange: z.string(), asset: z.string() }).loose(),
        ),
        recovery: z
          .object({
            attempted: z.boolean(),
            succeeded: z.boolean(),
            quarantined: z.boolean(),
            disposition: z.string(),
            recovery_loss: decimal,
          })
          .loose(),
        timeline: z.array(
          z.object({ index: z.number(), event_type: z.string() }).loose(),
        ),
        cost_attribution: z.record(z.string(), decimal),
        raw_evidence_available: z.boolean(),
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/strategies\?/,
    snapshotPage(
      z
        .object({
          id: z.string().min(1),
          family: z.string(),
          name: z.string(),
          version: z.string(),
          supported_modes: z.array(z.enum(["backtest", "replay", "shadow"])),
          maturity: z.string(),
          evidence_role: z.string(),
          confidence: z.string(),
          viability: z.string(),
          disclaimer: z.string(),
          created_at: timestamp,
          revision,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/inventory\?/,
    snapshotPage(
      z
        .object({
          id: z.string().min(1),
          exchange: z.string(),
          asset: z.string(),
          strategy_version: z.string(),
          experiment_id: z.string(),
          portfolio_id: z.string(),
          before: nonnegativeDecimal,
          after: nonnegativeDecimal,
          available: nonnegativeDecimal,
          reserved: nonnegativeDecimal,
          status: z.string(),
          virtual: z.literal(true),
          quality,
          updated_at: timestamp,
          revision,
        })
        .loose(),
    ).extend({
      combined_balance: z.literal(false),
      isolation_notice: z.string().min(1),
    }),
  ],
  [
    /^GET \/api\/v1\/rebalancing\/recommendations\?/,
    snapshotPage(
      z
        .object({
          id: z.string().min(1),
          method: z.string(),
          source_exchange: z.string(),
          source_asset: z.string(),
          destination_exchange: z.string(),
          destination_asset: z.string(),
          quantity: nonnegativeDecimal,
          total_cost: nonnegativeDecimal,
          risk_score: nonnegativeDecimal,
          advisory_only: z.literal(true),
          warnings: z.array(z.string()),
          quality,
          recorded_at: timestamp,
          revision,
        })
        .loose(),
    ).extend({ execution_available: z.literal(false) }),
  ],
  [
    /^GET \/api\/v1\/rebalancing\/recommendations\/[^/?]+$/,
    z
      .object({
        summary: z
          .object({ id: z.string(), advisory_only: z.literal(true) })
          .loose(),
        route: z.array(
          z
            .object({ index: z.number(), role: z.enum(["trade", "transfer"]) })
            .loose(),
        ),
        checklist: z.array(
          z
            .object({
              index: z.number(),
              instruction: z.string(),
              manual_only: z.literal(true),
            })
            .loose(),
        ),
        execution_available: z.literal(false),
      })
      .loose(),
  ],
  [
    /^GET \/api\/v1\/research\/champion-challenger\?/,
    snapshotPage(
      z
        .object({
          id: z.string().min(1),
          champion_strategy_version: z.string(),
          challenger_strategy_version: z.string(),
          disposition: z.string(),
          confidence: z.string(),
          viability: z.string(),
          disclaimer: z.string(),
          manifest_hash: z.string(),
          created_at: timestamp,
          revision,
        })
        .loose(),
    ),
  ],
  [
    /^GET \/api\/v1\/replays\/[^/]+\/faults$/,
    z
      .object({
        items: z.array(replayFault),
        revision,
        simulation_only: z.literal(true),
      })
      .loose(),
  ],
  [/^POST \/api\/v1\/replays\/[^/]+\/faults$/, replayFault],
  [
    /^POST \/api\/v1\/reports\/[^/]+\/exports$/,
    z
      .object({
        id: z.string().min(1),
        report_id: z.string().min(1),
        format: z.enum(["json", "csv"]),
        content_type: z.enum(["application/json", "text/csv"]),
        content: z.string().min(1),
        payload_hash: z.string().length(64),
        revision,
        simulation_only: z.literal(true),
        created_at: timestamp,
      })
      .loose(),
  ],
];
