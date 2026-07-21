package research

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// RunEvidenceInput is the exact evidence available after one canonical run.
// It deliberately does not promote a single run into a completed research suite.
type RunEvidenceInput struct {
	ResearchGenerationID string            `json:"research_generation_id"`
	RunID                string            `json:"run_id"`
	Mode                 string            `json:"mode"`
	ResultHash           string            `json:"result_hash"`
	ReproducibilityHash  string            `json:"reproducibility_hash"`
	Metrics              map[string]string `json:"metrics"`
	CreatedAt            time.Time         `json:"created_at"`
}

// RunEvidenceReport is an immutable, explicitly incomplete single-run report.
// A completed registered suite must use ReportManifest instead.
type RunEvidenceReport struct {
	RunEvidenceInput
	SchemaVersion       string            `json:"schema_version"`
	ConfidenceLabel     string            `json:"confidence_label"`
	PlatformCorrectness string            `json:"platform_correctness"`
	StrategyEvidence    string            `json:"strategy_evidence"`
	Viability           string            `json:"viability"`
	Coverage            map[string]string `json:"coverage"`
	Disclaimer          string            `json:"disclaimer"`
	ManifestHash        string            `json:"-"`
}

// BuildRunEvidenceReport records what the run established and, equally
// importantly, every research dimension it did not establish.
func BuildRunEvidenceReport(input RunEvidenceInput) (RunEvidenceReport, []byte, error) {
	if input.ResearchGenerationID == "" || input.RunID == "" ||
		(input.Mode != "backtest" && input.Mode != "replay") || !validEvidenceHash(input.ResultHash) ||
		!validEvidenceHash(input.ReproducibilityHash) || len(input.Metrics) == 0 ||
		input.CreatedAt.Location() != time.UTC {
		return RunEvidenceReport{}, nil, researchError("run_report_input_invalid")
	}
	report := RunEvidenceReport{RunEvidenceInput: cloneRunEvidenceInput(input), SchemaVersion: "axiom.research.run-evidence.v1",
		ConfidenceLabel: "insufficient", PlatformCorrectness: "canonical_pipeline_completed",
		StrategyEvidence: "single_registered_run_only", Viability: "undetermined",
		Coverage: map[string]string{
			"baseline":               "completed",
			"chronological_split":    "not_established_by_single_run",
			"walk_forward":           "not_established_by_single_run",
			"bootstrap_confidence":   "not_established_by_single_run",
			"parameter_neighborhood": "not_established_by_single_run",
			"capacity_curve":         "not_established_by_single_run",
			"stress_suite":           "not_established_by_single_run",
			"benchmarks":             "not_established_by_single_run",
			"breakdowns":             "not_established_by_single_run",
		}, Disclaimer: DisclaimerNoProductionProfitability}
	canonical, err := json.Marshal(report)
	if err != nil {
		return RunEvidenceReport{}, nil, researchError("run_report_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	report.ManifestHash = hex.EncodeToString(digest[:])
	return report, canonical, nil
}

func cloneRunEvidenceInput(input RunEvidenceInput) RunEvidenceInput {
	cloned := input
	cloned.Metrics = make(map[string]string, len(input.Metrics))
	for key, value := range input.Metrics {
		cloned.Metrics[key] = value
	}
	return cloned
}

func validEvidenceHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
