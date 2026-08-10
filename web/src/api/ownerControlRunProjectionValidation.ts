import { z } from "zod";

import {
  decimal,
  nonnegativeDecimal,
  revision,
  timestamp,
} from "./validationShared";

export const runPortfolioProjection = z
  .object({
    state: z.enum(["recorded", "not_recorded"]),
    summary: z.string().min(1).max(500).optional(),
    realized_pnl: decimal.optional(),
    unrealized_pnl: decimal.optional(),
    total_pnl: decimal.optional(),
    account_drawdown: nonnegativeDecimal.optional(),
    slippage: nonnegativeDecimal.optional(),
    positions: z
      .array(
        z
          .object({
            exchange: z.enum(["binance", "bybit"]),
            instrument: z.string().min(3).max(32),
            quantity: nonnegativeDecimal,
            total_cost: nonnegativeDecimal,
            weighted_average_cost: nonnegativeDecimal,
            realized_pnl: decimal,
            valuation_state: z.enum(["complete", "unvalued_fee_asset"]),
            updated_at: timestamp,
          })
          .strict(),
      )
      .max(20)
      .optional(),
    fees: z
      .array(
        z
          .object({
            exchange: z.enum(["binance", "bybit"]),
            instrument: z.string().min(3).max(32),
            asset: z.string().min(2).max(32),
            fee: nonnegativeDecimal,
            rebate: nonnegativeDecimal,
          })
          .strict(),
      )
      .max(20)
      .optional(),
    ordinal: revision.optional(),
    content_hash: z
      .string()
      .regex(/^[0-9a-f]{64}$/)
      .optional(),
    canonical_payload: z.string().min(2).max(1_048_576).optional(),
    waiting_reason: z.string().min(1).max(500).optional(),
  })
  .loose();

export const runRiskProjection = z
  .object({
    state: z.enum(["recorded", "not_recorded"]),
    summary: z.string().min(1).max(500),
    status: z.enum(["normal", "waiting", "blocked"]).optional(),
    blockers: z.array(z.string().min(1).max(500)).max(20).optional(),
    observations: z
      .array(
        z
          .object({
            exchange: z.enum(["binance", "bybit"]),
            instrument: z.string().min(3).max(32),
            policy_version: revision,
            account_drawdown: nonnegativeDecimal,
            utc_day_loss: nonnegativeDecimal,
            rolling_24_hour_loss: nonnegativeDecimal,
            strategy_loss: nonnegativeDecimal,
            asset_exposure: nonnegativeDecimal,
            combined_exposure: nonnegativeDecimal,
            exchange_exposure: nonnegativeDecimal,
            reserve: nonnegativeDecimal,
            reserved_capital: nonnegativeDecimal,
            spread: nonnegativeDecimal,
            slippage: nonnegativeDecimal,
            open_orders: z.number().int().min(0).max(2),
            quality_score: z.number().int().min(0).max(100),
            health_blockers: z.array(z.string().min(1).max(80)).max(9),
            observed_at: timestamp,
            evidence_hash: z.string().regex(/^[0-9a-f]{64}$/),
          })
          .strict(),
      )
      .max(20)
      .optional(),
  })
  .loose();
