import { z } from "zod";

const decimal = z.string().regex(/^-?(0|[1-9][0-9]*)(\.[0-9]+)?$/);
const nonnegativeDecimal = z.string().regex(/^(0|[1-9][0-9]*)(\.[0-9]+)?$/);
const revision = z.string().regex(/^(0|[1-9][0-9]*)$/);
const timestamp = z.string().min(1);
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
const replayInspection = z
  .object({
    event_count: revision,
    ordinal: revision,
    event_hash: z.string().regex(/^[0-9a-f]{64}$/),
    canonical_event: canonicalJSON,
    canonical_decision: canonicalJSON,
    canonical_orders: canonicalJSON,
    canonical_execution_events: canonicalJSON,
    canonical_balances: canonicalJSON,
  })
  .strict();
const jobResult = z
  .object({
    result_hash: z.string().regex(/^[0-9a-f]{64}$/),
    platform_correctness: z.string(),
    strategy_evidence: z.string(),
    viability: z.enum(["undetermined", "viable_for_more_research", "rejected"]),
    reproducibility: z.string(),
    report_id: z.string(),
    report_hash: z.string().regex(/^[0-9a-f]{64}$/),
    confidence_label: z.enum([
      "local_tier_b",
      "formal_tier_a",
      "insufficient",
      "rejected",
    ]),
    research_coverage: z.enum([
      "single_run_incomplete",
      "registered_suite_complete",
    ]),
    disclaimer: z.string(),
    metrics: z.record(z.string(), decimal).optional(),
  })
  .strict();
const researchResultSlice = z
  .object({
    name: z.string().min(1),
    net_return: decimal,
    max_drawdown: nonnegativeDecimal,
    trades: z.number().int().nonnegative(),
  })
  .strict();
const registeredResearchReport = z
  .object({
    id: z.string().min(1),
    research_generation_id: z.string().min(1),
    manifest_hash: z.string().regex(/^[0-9a-f]{64}$/),
    confidence_label: z.enum(["local_tier_b", "formal_tier_a", "rejected"]),
    platform_correctness: z.string(),
    strategy_evidence: z.string(),
    viability: z.enum(["undetermined", "viable_for_more_research", "rejected"]),
    disclaimer: z.string(),
    run_references: z.array(z.string().min(1)).min(1),
    benchmarks: z.array(researchResultSlice),
    stress: z.array(researchResultSlice),
    capacity: z.array(
      z
        .object({
          notional: nonnegativeDecimal,
          net_return: decimal,
          fill_rate: nonnegativeDecimal,
        })
        .strict(),
    ),
    canonical_manifest: canonicalJSON,
    created_at: timestamp,
  })
  .strict();

export const jobSchema = z
  .object({
    id: z.string().min(1),
    kind: z.enum(["backtest", "replay"]),
    state: z.enum([
      "QUEUED",
      "RUNNING",
      "PAUSE_REQUESTED",
      "PAUSED",
      "CANCEL_REQUESTED",
      "CANCELED",
      "SUCCEEDED",
      "FAILED",
    ]),
    mode_label: z.enum(["BACKTEST", "REPLAY"]),
    revision,
    created_at: timestamp,
    updated_at: timestamp.optional(),
    progress: nonnegativeDecimal.optional(),
    cursor_ordinal: revision.optional(),
    failure_code: z.string().optional(),
    result: jobResult.optional(),
    registered_report: registeredResearchReport.optional(),
    replay_inspection: replayInspection.optional(),
  })
  .loose();
