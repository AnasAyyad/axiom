import { describe, expect, it } from "vitest";

import type { APIModel } from "../../api/client";
import { compareLabRuns } from "./labModel";

function job(
  id: string,
  dataset: string,
  resultHash: string,
): APIModel<"JobResource"> {
  return {
    id,
    kind: "backtest",
    state: "SUCCEEDED",
    mode_label: "BACKTEST",
    revision: "3",
    created_at: "2026-08-03T10:00:00Z",
    input_manifest: {
      configuration_id: "configuration-a",
      dataset_id: dataset,
      research_generation_id: "generation-a",
      strategy_version: "trend-following@1.0.0",
      root_seed_hash: "a".repeat(64),
    },
    result: {
      result_hash: resultHash,
      platform_correctness: "verified",
      strategy_evidence: "limited",
      viability: "undetermined",
      reproducibility: "exact",
      report_id: "report-a",
      report_hash: "b".repeat(64),
      confidence_label: "local_tier_b",
      research_coverage: "single_run_incomplete",
      disclaimer: "Research only",
    },
  };
}

describe("compareLabRuns", () => {
  it("marks exact identity and result differences without numeric coercion", () => {
    const rows = compareLabRuns(
      job("left", "dataset-a", "c".repeat(64)),
      job("right", "dataset-b", "d".repeat(64)),
    );
    expect(rows.find((row) => row.field === "Dataset")).toMatchObject({
      left: "dataset-a",
      right: "dataset-b",
      changed: true,
    });
    expect(rows.find((row) => row.field === "Configuration")).toMatchObject({
      changed: false,
    });
    expect(rows.find((row) => row.field === "Result hash")?.changed).toBe(true);
  });
});
