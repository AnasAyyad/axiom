import { z } from "zod";

import {
  decimal,
  nonnegativeDecimal,
  page,
  revision,
  timestamp,
} from "./validationShared";

const sandboxExchange = z.enum(["binance", "bybit"]);
const sandboxEnvironment = z.enum(["spot_testnet", "demo"]);
const arm = z
  .object({
    id: z.string().min(1),
    session_id: z.string().min(1),
    account_ids: z.array(z.string().min(1)).min(1).max(2),
    state: z.enum(["active", "expired", "revoked"]),
    created_at: timestamp,
    expires_at: timestamp,
    revoked_at: timestamp.optional(),
    revision,
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const capUsage = z
  .object({
    utc_day: z.string().min(1),
    per_order_limit: nonnegativeDecimal,
    daily_limit: nonnegativeDecimal,
    daily_reserved: nonnegativeDecimal,
    daily_remaining: nonnegativeDecimal,
    account_open: z.number().int().nonnegative(),
    account_open_limit: z.literal(1),
    global_open: z.number().int().nonnegative(),
    global_open_limit: z.literal(2),
  })
  .loose();
const account = z
  .object({
    id: z.string().min(1),
    exchange: sandboxExchange,
    environment: sandboxEnvironment,
    state: z.enum([
      "LOCKED",
      "READY_PAUSED",
      "ARMED",
      "DEGRADED",
      "QUARANTINED",
    ]),
    engine_ready: z.boolean(),
    account_epoch: z.number().int().positive(),
    credential_generation: z.number().int().positive(),
    revision,
    session_id: z.string().min(1).optional(),
    session_revision: revision.optional(),
    startup_cycle: z.number().int().nonnegative(),
    private_stream_healthy: z.boolean(),
    reconciliation_clean: z.boolean(),
    evidence_healthy: z.boolean(),
    lease_held: z.boolean(),
    observed_at: timestamp,
    stale: z.boolean(),
    active_arm: arm.optional(),
    cap_usage: capUsage,
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const fill = z
  .object({
    id: z.string().length(64),
    order_id: z.string().min(1),
    quantity: nonnegativeDecimal,
    price: nonnegativeDecimal,
    fee_quantity: nonnegativeDecimal,
    fee_asset: z.enum(["USDT", "BTC", "ETH"]),
    occurred_at: timestamp,
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const order = z
  .object({
    id: z.string().min(1),
    account_id: z.string().min(1),
    exchange: sandboxExchange,
    environment: sandboxEnvironment,
    state: z.enum([
      "APPROVED",
      "SUBMITTING",
      "ACKNOWLEDGED",
      "PARTIALLY_FILLED",
      "FILLED",
      "CANCEL_PENDING",
      "CANCELED",
      "REJECTED",
      "EXPIRED",
      "UNKNOWN",
      "RECOVERY_REQUIRED",
    ]),
    action: z.enum(["ENTRY", "EXIT", "CANCEL", "RECOVERY"]),
    instrument: z.enum(["BTCUSDT", "ETHUSDT", "ETHBTC"]),
    side: z.enum(["buy", "sell"]),
    quantity: nonnegativeDecimal,
    limit_price: nonnegativeDecimal,
    notional: nonnegativeDecimal,
    style: z.enum(["LIMIT_GTC", "LIMIT_IOC", "POST_ONLY"]),
    attempt: z.number().int().nonnegative(),
    recovery_status: z.enum([
      "not_required",
      "required",
      "querying",
      "reconciled",
    ]),
    unknown_since: timestamp.optional(),
    created_at: timestamp,
    updated_at: timestamp,
    revision,
    fills: z.array(fill),
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const difference = z
  .object({
    id: z.string().min(1),
    category: z.string().min(1),
    classification: z.string().min(1),
    asset: z.enum(["USDT", "BTC", "ETH"]).optional(),
    quantity: decimal.optional(),
    critical: z.boolean(),
    state: z.enum(["OPEN", "RESOLVED", "QUARANTINED", "ADJUSTED"]),
    recorded_at: timestamp,
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const reconciliation = z
  .object({
    id: z.string().min(1),
    account_id: z.string().min(1),
    exchange: sandboxExchange,
    account_epoch: z.number().int().positive(),
    state: z.enum(["clean", "quarantined"]),
    reconciled_at: timestamp,
    differences: z.array(difference),
    suspense_count: z.number().int().nonnegative(),
    quarantine_count: z.number().int().nonnegative(),
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const resetIncident = z
  .object({
    id: z.string().min(1),
    account_id: z.string().min(1),
    exchange: sandboxExchange,
    prior_epoch: z.number().int().positive(),
    new_epoch: z.number().int().positive(),
    state: z.enum(["OPEN", "RECONCILING", "RESOLVED", "QUARANTINED"]),
    detected_at: timestamp,
    resolved_at: timestamp.optional(),
    adjustments: z.array(
      z
        .object({
          asset: z.enum(["USDT", "BTC", "ETH"]),
          quantity: decimal,
          pnl_effect: z.literal(false),
          recorded_at: timestamp,
        })
        .loose(),
    ),
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const chaos = z
  .object({
    status: z.enum(["not_run", "passed", "failed"]),
    passed: z.number().int().nonnegative(),
    failed: z.number().int().nonnegative(),
    last_observed_at: timestamp,
  })
  .loose();
const qualificationSlo = z
  .object({
    samples: z.number().int().nonnegative(),
    critical_alert_latency_ms: z.number().int().nonnegative(),
    recovery_duration_ms: z.number().int().nonnegative(),
    duplicate_creates: z.number().int().nonnegative(),
    lost_fills: z.number().int().nonnegative(),
    double_posted_fills: z.number().int().nonnegative(),
    unknown_orders: z.number().int().nonnegative(),
    reconciliation_mismatches: z.number().int().nonnegative(),
    suspense_items: z.number().int().nonnegative(),
    reconnects: z.number().int().nonnegative(),
    restarts: z.number().int().nonnegative(),
    resident_memory_delta_bytes: z.number().int(),
    positive_memory_leak_trend: z.boolean(),
    passing: z.boolean(),
  })
  .loose();
const recoveryIncident = z
  .object({
    account_id: z.string().min(1),
    exchange: sandboxExchange,
    environment: sandboxEnvironment,
    state: z.enum([
      "active",
      "recovered",
      "expired",
      "repeated",
      "unrecoverable",
    ]),
    reason_category: z.string().max(64),
    cause_code: z.string().regex(/^[a-z0-9_]{1,64}$/),
    deadline_at: timestamp,
    clean_check_count: z.number().int().min(0).max(2),
    detected_at: timestamp,
    recovery_timestamp: timestamp.optional(),
    evidence_hash: z.string().length(64),
  })
  .loose();
const qualification = z
  .object({
    state: z.enum([
      "not_started",
      "PENDING",
      "RUNNING",
      "SMOKE_PASSED",
      "PASSED",
      "FAILED",
    ]),
    mode: z.enum(["none", "smoke", "formal"]),
    required_duration_seconds: z.number().int().nonnegative(),
    observed_duration_seconds: z.number().int().nonnegative(),
    profitability_evidence: z.literal(false),
    qualified: z.boolean(),
    failures: z.array(z.string()),
    chaos,
    slo: qualificationSlo,
    recovery_incidents: z.array(recoveryIncident),
    formal_soak_pending: z.boolean(),
    audit_url: z.string().startsWith("/api/v1/audit-events"),
  })
  .loose();
const accepted = z
  .object({
    id: z.string().min(1),
    state: z.enum(["pending", "applied", "rejected", "failed"]),
    target_id: z.string(),
    correlation_id: z.string().min(1),
    created_at: timestamp,
    revision,
  })
  .loose();

export const c6ResponseSchemas: ReadonlyArray<readonly [RegExp, z.ZodType]> = [
  [
    /^GET \/api\/v1\/sandbox\/overview$/,
    z
      .object({
        environment_label: z.literal(
          "BINANCE SPOT TESTNET + BYBIT DEMO / VIRTUAL",
        ),
        real_trading_enabled: z.literal(false),
        observed_at: timestamp,
        stale: z.boolean(),
        accounts: z.array(account),
        active_arms: z.array(arm),
        orders: z.array(order),
        reconciliations: z.array(reconciliation),
        reset_incidents: z.array(resetIncident),
        risk_state: z.enum(["NORMAL", "CAUTIOUS", "PAUSED", "LOCKED"]),
        qualification,
        audit_url: z.string().startsWith("/api/v1/audit-events"),
      })
      .loose(),
  ],
  [/^GET \/api\/v1\/sandbox\/orders\?/, page(order)],
  [
    /^GET \/api\/v1\/sandbox\/reconciliations\?/,
    page(reconciliation).extend({ reset_incidents: z.array(resetIncident) }),
  ],
  [/^GET \/api\/v1\/sandbox\/qualification$/, qualification],
  [
    /^POST \/api\/v1\/sandbox\/authorizations$/,
    z
      .object({
        token: z.string().min(32),
        purpose: z.enum(["sandbox_arm", "risk_unlock"]),
        expires_at: timestamp,
      })
      .loose(),
  ],
  [/^POST \/api\/v1\/sandbox\/sessions\/[^/]+\/arms$/, arm],
  [/^POST \/api\/v1\/sandbox\/orders$/, accepted],
  [/^POST \/api\/v1\/sandbox\/(?:arms|accounts|orders)\/.+$/, accepted],
];
