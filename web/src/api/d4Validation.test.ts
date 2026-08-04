import { describe, expect, it } from "vitest";

import { parseAPIResponse } from "./validation";

describe("D4 browser contract validation", () => {
  it("accepts a provenance-complete report", () => {
    const parsed = parseAPIResponse("GET /api/v1/reports/report-d4", {
      id: "report-d4",
      job_id: "job-d4",
      report_type: "risk",
      state: "SUCCEEDED",
      provenance: {
        mode: "operational",
        confidence_tier: "operational",
        valuation_basis: "not applicable",
        model_provenance: { report_schema: "axiom.report.v1d.d4" },
        maturity: "operational",
        source_identity: "a".repeat(64),
        source_revision: "4",
      },
      generated_at: "2026-08-04T12:00:00Z",
      content_hash: "b".repeat(64),
      created_at: "2026-08-04T11:59:00Z",
      revision: "3",
    });
    expect(parsed?.success).toBe(true);
  });

  it("rejects an audit verdict outside the closed contract", () => {
    const parsed = parseAPIResponse("GET /api/v1/audit-verification", {
      verdict: "unknown",
      checked_events: 4,
      head_hash: "a".repeat(64),
      verified_at: "2026-08-04T12:00:00Z",
    });
    expect(parsed?.success).toBe(false);
  });

  it("requires immutable alert delivery timing", () => {
    const parsed = parseAPIResponse("GET /api/v1/alerts/alert-d4", {
      id: "alert-d4",
      severity: "warning",
      reason_code: "alert_delivery",
      component: "report-worker",
      state: "open",
      occurrences: 1,
      revision: "2",
      correlation_id: "correlation-d4",
      created_at: "2026-08-04T12:00:00Z",
      last_seen_at: "2026-08-04T12:00:01Z",
      deliveries: [
        { id: "attempt-d4", sink_name: "webhook", attempt: 1, state: "failed" },
      ],
      escalations: [],
    });
    expect(parsed?.success).toBe(false);
  });
});
