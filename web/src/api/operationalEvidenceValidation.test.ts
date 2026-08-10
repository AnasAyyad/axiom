import { describe, expect, it } from "vitest";

import { parseAPIResponse } from "./validation";

describe("operational evidence browser contract validation", () => {
  it("accepts a provenance-complete report", () => {
    const parsed = parseAPIResponse(
      "GET /api/v1/reports/report-operational_evidence",
      {
        id: "report-operational_evidence",
        job_id: "job-operational_evidence",
        report_type: "risk",
        state: "SUCCEEDED",
        provenance: {
          mode: "operational",
          confidence_tier: "operational",
          valuation_basis: "not applicable",
          model_provenance: {
            report_schema: "axiom.report.owner_console.operational_evidence",
          },
          maturity: "operational",
          source_identity: "a".repeat(64),
          source_revision: "4",
        },
        generated_at: "2026-08-04T12:00:00Z",
        content_hash: "b".repeat(64),
        created_at: "2026-08-04T11:59:00Z",
        revision: "3",
      },
    );
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
    const parsed = parseAPIResponse(
      "GET /api/v1/alerts/alert-operational_evidence",
      {
        id: "alert-operational_evidence",
        severity: "warning",
        reason_code: "alert_delivery",
        component: "report-worker",
        state: "open",
        occurrences: 1,
        revision: "2",
        correlation_id: "correlation-operational_evidence",
        created_at: "2026-08-04T12:00:00Z",
        last_seen_at: "2026-08-04T12:00:01Z",
        deliveries: [
          {
            id: "attempt-operational_evidence",
            sink_name: "webhook",
            attempt: 1,
            state: "failed",
          },
        ],
        escalations: [],
      },
    );
    expect(parsed?.success).toBe(false);
  });
});
