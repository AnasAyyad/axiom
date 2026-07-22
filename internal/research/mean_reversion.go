package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// MeanReversionReportContract is separate from the immutable Trend contract.
const MeanReversionReportContract = "mean-reversion-report.v1"

// MeanReversionReportManifest is canonical registered B3 research evidence.
type MeanReversionReportManifest struct {
	Contract string `json:"report_contract"`
	ReportInput
	Stability    Stability `json:"stability"`
	Disclaimer   string    `json:"disclaimer"`
	ManifestHash string    `json:"-"`
}

// BuildMeanReversionReport validates B3-specific failure and regime coverage
// without relaxing the existing Trend report contract.
func BuildMeanReversionReport(input ReportInput) (MeanReversionReportManifest, error) {
	if err := validateMeanReversionReportInput(input); err != nil {
		return MeanReversionReportManifest{}, err
	}
	stability, err := NeighborhoodStability(input.Neighborhood)
	if err != nil {
		return MeanReversionReportManifest{}, err
	}
	manifest := MeanReversionReportManifest{Contract: MeanReversionReportContract,
		ReportInput: cloneReportInput(input), Stability: stability, Disclaimer: DisclaimerNoProductionProfitability}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return MeanReversionReportManifest{}, researchError("report_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	manifest.ManifestHash = hex.EncodeToString(digest[:])
	return manifest, nil
}

// ValidateMeanReversionReportCanonical proves exact stored B3 report bytes.
func ValidateMeanReversionReportCanonical(canonical []byte, expectedHash, generationID, runID string) (MeanReversionReportManifest, error) {
	var stored MeanReversionReportManifest
	if !json.Valid(canonical) || json.Unmarshal(canonical, &stored) != nil || expectedHash == "" ||
		generationID == "" || stored.Contract != MeanReversionReportContract || stored.ResearchGenerationID != generationID {
		return MeanReversionReportManifest{}, researchError("mean_reversion_report_canonical_invalid")
	}
	rebuilt, err := BuildMeanReversionReport(stored.ReportInput)
	if err != nil || rebuilt.ManifestHash != expectedHash || rebuilt.Stability != stored.Stability ||
		stored.Disclaimer != DisclaimerNoProductionProfitability {
		return MeanReversionReportManifest{}, researchError("mean_reversion_report_canonical_invalid")
	}
	reencoded, err := json.Marshal(rebuilt)
	if err != nil || !bytes.Equal(reencoded, canonical) {
		return MeanReversionReportManifest{}, researchError("mean_reversion_report_canonical_invalid")
	}
	if runID != "" && !containsRunReference(rebuilt.RunReferences, runID) {
		return MeanReversionReportManifest{}, researchError("report_run_reference_missing")
	}
	return rebuilt, nil
}

func validateMeanReversionReportInput(input ReportInput) error {
	if input.ResearchGenerationID == "" || input.Hypothesis == "" || input.PrimaryMetric == "" ||
		len(input.WalkForward) == 0 || input.Confidence.SeedHash == "" || len(input.Capacity) < 2 ||
		len(input.RunReferences) == 0 || input.CreatedAt.Location() != time.UTC ||
		containsMisleadingClaim(input.PlatformCorrectness) || containsMisleadingClaim(input.StrategyEvidence) {
		return researchError("mean_reversion_report_input_incomplete")
	}
	if err := input.Split.Validate(); err != nil {
		return err
	}
	if input.ConfidenceLabel != "local_tier_b" && input.ConfidenceLabel != "formal_tier_a" &&
		input.ConfidenceLabel != "insufficient" && input.ConfidenceLabel != "rejected" {
		return researchError("report_confidence_invalid")
	}
	if input.ViabilityDisposition != "undetermined" && input.ViabilityDisposition != "viable_for_more_research" &&
		input.ViabilityDisposition != "rejected" {
		return researchError("report_viability_invalid")
	}
	if err := validateCapacity(input.Capacity); err != nil {
		return err
	}
	if !containsNames(input.Stress, []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"}) ||
		!containsNames(input.Benchmarks, []string{"cash", "buy_and_hold", "static_inventory"}) {
		return researchError("report_scenarios_incomplete")
	}
	for _, key := range []string{"asset", "regime", "holding_period", "fast_decline_failure",
		"maximum_adverse_excursion", "trend_filter_comparison", "drawdown"} {
		if len(input.Breakdowns[key]) == 0 {
			return researchError("mean_reversion_report_breakdown_incomplete")
		}
	}
	for _, reason := range []string{"mean_reversion.reject.dangerous_regime", "mean_reversion.reject.adx",
		"mean_reversion.reject.market_quality", "mean_reversion.failure.fast_decline"} {
		if _, ok := input.Rejections[reason]; !ok {
			return researchError("mean_reversion_report_failures_incomplete")
		}
	}
	return nil
}

func containsRunReference(references []string, wanted string) bool {
	for _, reference := range references {
		if reference == wanted {
			return true
		}
	}
	return false
}
