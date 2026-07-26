import { z } from "zod";

export const decimal = z.string().regex(/^-?(0|[1-9][0-9]*)(\.[0-9]+)?$/);
export const nonnegativeDecimal = z
  .string()
  .regex(/^(0|[1-9][0-9]*)(\.[0-9]+)?$/);
export const revision = z.string().regex(/^(0|[1-9][0-9]*)$/);
export const timestamp = z.string().min(1);

export const page = (item: z.ZodType) =>
  z.object({ items: z.array(item), revision, has_more: z.boolean() }).loose();

export const quality = z
  .object({
    tier: z.enum([
      "formal_tier_a",
      "local_tier_b",
      "integration_only",
      "unknown",
    ]),
    confidence: z.enum(["high", "medium", "low", "insufficient", "unknown"]),
    freshness: z.enum(["live", "fresh", "stale", "expired", "historical"]),
    source: z.string().min(1),
    observed_at: timestamp,
    provenance_complete: z.boolean(),
  })
  .loose();

export const snapshotPage = (item: z.ZodType) =>
  page(item).extend({ snapshot_revision: revision });
