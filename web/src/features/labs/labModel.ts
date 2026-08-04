import type { APIModel } from "../../api/client";

export interface LabRunForm {
  readonly configuration: string;
  readonly dataset: string;
  readonly researchGeneration: string;
  readonly strategy: string;
  readonly seed: string;
  readonly speed: "original" | "accelerated" | "maximum";
}

export const emptyLabRun: LabRunForm = {
  configuration: "",
  dataset: "",
  researchGeneration: "",
  strategy: "trend.v1a.1",
  seed: "",
  speed: "maximum",
};

export interface ComparisonRow {
  readonly id: string;
  readonly [key: string]: unknown;
  readonly field: string;
  readonly left: string;
  readonly right: string;
  readonly changed: boolean;
}

export function compareLabRuns(
  left: APIModel<"JobResource">,
  right: APIModel<"JobResource">,
): ComparisonRow[] {
  const fields: ReadonlyArray<
    readonly [string, string | undefined, string | undefined]
  > = [
    ["State", left.state, right.state],
    [
      "Configuration",
      left.input_manifest?.configuration_id,
      right.input_manifest?.configuration_id,
    ],
    [
      "Dataset",
      left.input_manifest?.dataset_id,
      right.input_manifest?.dataset_id,
    ],
    [
      "Research generation",
      left.input_manifest?.research_generation_id,
      right.input_manifest?.research_generation_id,
    ],
    [
      "Strategy version",
      left.input_manifest?.strategy_version,
      right.input_manifest?.strategy_version,
    ],
    [
      "Root seed",
      left.input_manifest?.root_seed_hash,
      right.input_manifest?.root_seed_hash,
    ],
    ["Replay speed", left.input_manifest?.speed, right.input_manifest?.speed],
    [
      "Input hash",
      left.reproduction_bundle?.input_hash,
      right.reproduction_bundle?.input_hash,
    ],
    [
      "Manifest hash",
      left.reproduction_bundle?.manifest_hash,
      right.reproduction_bundle?.manifest_hash,
    ],
    [
      "Dataset manifest hash",
      left.reproduction_bundle?.dataset_manifest_hash,
      right.reproduction_bundle?.dataset_manifest_hash,
    ],
    [
      "Configuration hash",
      left.reproduction_bundle?.configuration_hash,
      right.reproduction_bundle?.configuration_hash,
    ],
    [
      "Model namespace",
      left.reproduction_bundle?.model_namespace_id,
      right.reproduction_bundle?.model_namespace_id,
    ],
    [
      "Code commit",
      left.reproduction_bundle?.code_commit,
      right.reproduction_bundle?.code_commit,
    ],
    [
      "Confidence tier",
      left.reproduction_bundle?.confidence_tier,
      right.reproduction_bundle?.confidence_tier,
    ],
    ["Result hash", left.result?.result_hash, right.result?.result_hash],
    [
      "Platform correctness",
      left.result?.platform_correctness,
      right.result?.platform_correctness,
    ],
    ["Strategy viability", left.result?.viability, right.result?.viability],
  ];
  return fields.map(([field, leftValue, rightValue]) => ({
    id: field,
    field,
    left: leftValue ?? "Not available",
    right: rightValue ?? "Not available",
    changed: leftValue !== rightValue,
  }));
}

export function labDownloadName(id: string, format: string) {
  return `axiom-lab-${id.replaceAll(/[^a-zA-Z0-9._-]/g, "-")}.${format}`;
}
