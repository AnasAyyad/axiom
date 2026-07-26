package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// EvidenceSource is one immutable run result used by a B7 suite.
type EvidenceSource struct {
	RunID           string `json:"run_id"`
	Mode            string `json:"mode"`
	DatasetTier     string `json:"dataset_tier"`
	ConfidenceLabel string `json:"confidence_label"`
	ResultHash      string `json:"result_hash"`
	Primary         bool   `json:"primary"`
}

// PromotionCriterion is one preregistered measurable pass/fail result.
type PromotionCriterion struct {
	ID     string `json:"id"`
	Passed bool   `json:"passed"`
}

// ValidationSuiteInput is complete multi-strategy statistical evidence for one
// preregistered research generation.
type ValidationSuiteInput struct {
	ID                       string               `json:"id"`
	ResearchGenerationID     string               `json:"research_generation_id"`
	StrategyVersionID        string               `json:"strategy_version_id"`
	StrategyFamily           string               `json:"strategy_family"`
	PreregistrationHash      string               `json:"preregistration_hash"`
	FinalTestConsumptionHash string               `json:"final_test_consumption_hash"`
	Sources                  []EvidenceSource     `json:"sources"`
	WalkForward              []WalkForwardFold    `json:"walk_forward"`
	Confidence               ConfidenceInterval   `json:"confidence"`
	Neighborhood             []ResultSlice        `json:"neighborhood"`
	Capacity                 []CapacityPoint      `json:"capacity"`
	Stress                   []ResultSlice        `json:"stress"`
	Benchmarks               []ResultSlice        `json:"benchmarks"`
	Regimes                  []ResultSlice        `json:"regimes"`
	MultipleTesting          MultipleTestingInput `json:"multiple_testing_input"`
	Sharpe                   SharpeInput          `json:"sharpe_input"`
	ObservedSamples          uint64               `json:"observed_samples"`
	ObservedTrades           uint64               `json:"observed_trades"`
	ObservedShadowDuration   time.Duration        `json:"observed_shadow_duration"`
	Criteria                 []PromotionCriterion `json:"criteria"`
	ConfidenceLabel          string               `json:"confidence_label"`
	PlatformCorrectness      string               `json:"platform_correctness"`
	StrategyEvidence         string               `json:"strategy_evidence"`
	ViabilityDisposition     string               `json:"viability_disposition"`
	CreatedAt                time.Time            `json:"created_at"`
}

// ValidationSuiteManifest is immutable B7 promotion evidence. Maturity remains
// a research-governance label and never claims production profitability.
type ValidationSuiteManifest struct {
	Contract string `json:"contract"`
	ValidationSuiteInput
	MultipleTestingEvidence MultipleTestingEvidence `json:"multiple_testing"`
	SharpeEvidence          SharpeEvidence          `json:"sharpe"`
	Stability               Stability               `json:"stability"`
	EligibleMaturities      []Maturity              `json:"eligible_maturities"`
	Disclaimer              string                  `json:"disclaimer"`
	ManifestHash            string                  `json:"-"`
}

// BuildValidationSuite validates, computes, and seals one B7 suite against its
// exact preregistration.
func BuildValidationSuite(
	registration ExperimentRegistrationManifest,
	input ValidationSuiteInput,
) (ValidationSuiteManifest, []byte, error) {
	input = cloneValidationSuiteInput(input)
	if err := validateValidationSuiteInput(registration, input); err != nil {
		return ValidationSuiteManifest{}, nil, err
	}
	multiple, err := AdjustMultipleTests(input.MultipleTesting)
	if err != nil {
		return ValidationSuiteManifest{}, nil, err
	}
	sharpe, err := AnalyzeSharpe(input.Sharpe)
	if err != nil {
		return ValidationSuiteManifest{}, nil, err
	}
	stability, err := NeighborhoodStability(input.Neighborhood)
	if err != nil {
		return ValidationSuiteManifest{}, nil, err
	}
	manifest := ValidationSuiteManifest{Contract: ValidationSuiteContract,
		ValidationSuiteInput: input, MultipleTestingEvidence: multiple,
		SharpeEvidence: sharpe, Stability: stability,
		Disclaimer: DisclaimerNoProductionProfitability}
	manifest.EligibleMaturities = eligibleMaturities(registration, manifest)
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ValidationSuiteManifest{}, nil, researchError("validation_suite_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	manifest.ManifestHash = hex.EncodeToString(digest[:])
	return manifest, canonical, nil
}

// ValidateValidationSuiteCanonical proves exact stored B7 suite bytes.
func ValidateValidationSuiteCanonical(
	registration ExperimentRegistrationManifest,
	canonical []byte,
	expectedHash string,
) (ValidationSuiteManifest, error) {
	var stored ValidationSuiteManifest
	if !json.Valid(canonical) || json.Unmarshal(canonical, &stored) != nil ||
		stored.Contract != ValidationSuiteContract || !validEvidenceHash(expectedHash) {
		return ValidationSuiteManifest{}, researchError("validation_suite_canonical_invalid")
	}
	rebuilt, encoded, err := BuildValidationSuite(registration, stored.ValidationSuiteInput)
	if err != nil || rebuilt.ManifestHash != expectedHash ||
		!bytes.Equal(encoded, canonical) ||
		!equalValidationDerived(rebuilt, stored) {
		return ValidationSuiteManifest{}, researchError("validation_suite_canonical_invalid")
	}
	return rebuilt, nil
}

// EligibleForMaturity confirms that the suite explicitly qualifies the target.
func (manifest ValidationSuiteManifest) EligibleForMaturity(target Maturity) bool {
	for _, maturity := range manifest.EligibleMaturities {
		if maturity == target {
			return true
		}
	}
	return false
}

func validateValidationSuiteInput(
	registration ExperimentRegistrationManifest,
	input ValidationSuiteInput,
) error {
	if registration.Contract != ExperimentRegistrationContract ||
		input.PreregistrationHash != registration.RegistrationHash ||
		input.ResearchGenerationID != registration.ResearchGenerationID ||
		input.StrategyVersionID != registration.StrategyVersionID ||
		!researchIdentifier.MatchString(input.ID) ||
		!researchIdentifier.MatchString(input.StrategyFamily) ||
		!validEvidenceHash(input.FinalTestConsumptionHash) ||
		len(input.Sources) == 0 || len(input.WalkForward) == 0 ||
		input.Confidence.SeedHash == "" || len(input.Regimes) == 0 ||
		input.ObservedSamples == 0 || input.ObservedTrades == 0 ||
		len(input.Criteria) == 0 || input.CreatedAt.Location() != time.UTC ||
		input.CreatedAt.Before(registration.Split.FinalTest.End) {
		return researchError("validation_suite_incomplete")
	}
	if input.ConfidenceLabel != "formal_tier_a" && input.ConfidenceLabel != "local_tier_b" &&
		input.ConfidenceLabel != "insufficient" && input.ConfidenceLabel != "rejected" {
		return researchError("validation_suite_confidence_invalid")
	}
	if input.ViabilityDisposition != "undetermined" &&
		input.ViabilityDisposition != "viable_for_more_research" &&
		input.ViabilityDisposition != "rejected" {
		return researchError("validation_suite_viability_invalid")
	}
	if containsMisleadingClaim(input.PlatformCorrectness) ||
		containsMisleadingClaim(input.StrategyEvidence) ||
		!validEvidenceSources(input.Sources) || !validCriteria(input.Criteria) ||
		validateCapacity(input.Capacity) != nil ||
		!containsNames(input.Stress, []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"}) ||
		!containsNames(input.Benchmarks, []string{"cash", "buy_and_hold", "static_inventory"}) {
		return researchError("validation_suite_incomplete")
	}
	for _, regime := range input.Regimes {
		if _, _, err := parseResult(regime); err != nil {
			return researchError("validation_suite_incomplete")
		}
	}
	return nil
}

func eligibleMaturities(
	registration ExperimentRegistrationManifest,
	manifest ValidationSuiteManifest,
) []Maturity {
	if !globallyEligible(registration, manifest) {
		return nil
	}
	modes := primaryModes(manifest.Sources)
	result := make([]Maturity, 0, 3)
	if modes["backtest"] {
		result = append(result, MaturityBacktestValidated)
	}
	if modes["backtest"] && modes["replay"] {
		result = append(result, MaturityReplayValidated)
	}
	if modes["backtest"] && modes["replay"] && modes["shadow"] &&
		manifest.ObservedShadowDuration >= registration.MinimumShadowDuration {
		result = append(result, MaturityShadowValidated)
	}
	return result
}

func globallyEligible(
	registration ExperimentRegistrationManifest,
	manifest ValidationSuiteManifest,
) bool {
	if manifest.ConfidenceLabel != "formal_tier_a" ||
		manifest.ViabilityDisposition != "viable_for_more_research" ||
		manifest.ObservedSamples < registration.MinimumSamples ||
		manifest.ObservedTrades < registration.MinimumTrades ||
		!manifest.Stability.Stable || len(manifest.MultipleTestingEvidence.Rejected) == 0 ||
		!manifest.MultipleTestingEvidence.Rejected[0] || !allCriteriaPassed(manifest.Criteria) ||
		!primarySourcesQualified(manifest.Sources) {
		return false
	}
	minimum, _, _ := parseFiniteDecimal(registration.MinimumDeflatedSharpeProbability)
	actual, _, err := parseFiniteDecimal(manifest.SharpeEvidence.DeflatedSharpeProbability)
	if err != nil || actual.Cmp(minimum) < 0 {
		return false
	}
	lower, _, err := parseFiniteDecimal(manifest.Confidence.Lower)
	zero, _, _ := apd.NewFromString("0")
	return err == nil && lower.Cmp(zero) > 0
}

func validEvidenceSources(sources []EvidenceSource) bool {
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if !researchIdentifier.MatchString(source.RunID) ||
			!validEvidenceHash(source.ResultHash) ||
			!containsStrings([]string{"backtest", "replay", "shadow", "paper", "integration"}, []string{source.Mode}) ||
			!containsStrings([]string{"tier_a", "tier_b", "low_confidence", "integration_only"}, []string{source.DatasetTier}) ||
			!containsStrings([]string{"formal_tier_a", "local_tier_b", "insufficient"}, []string{source.ConfidenceLabel}) {
			return false
		}
		key := source.Mode + "|" + source.RunID
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func primarySourcesQualified(sources []EvidenceSource) bool {
	primary := 0
	for _, source := range sources {
		if !source.Primary {
			continue
		}
		primary++
		if source.DatasetTier != "tier_a" || source.ConfidenceLabel != "formal_tier_a" ||
			(source.Mode != "backtest" && source.Mode != "replay" && source.Mode != "shadow") {
			return false
		}
	}
	return primary > 0
}

func primaryModes(sources []EvidenceSource) map[string]bool {
	result := make(map[string]bool)
	for _, source := range sources {
		if source.Primary {
			result[source.Mode] = true
		}
	}
	return result
}

func validCriteria(criteria []PromotionCriterion) bool {
	seen := make(map[string]struct{}, len(criteria))
	for _, criterion := range criteria {
		if !researchIdentifier.MatchString(criterion.ID) {
			return false
		}
		if _, duplicate := seen[criterion.ID]; duplicate {
			return false
		}
		seen[criterion.ID] = struct{}{}
	}
	return true
}

func allCriteriaPassed(criteria []PromotionCriterion) bool {
	for _, criterion := range criteria {
		if !criterion.Passed {
			return false
		}
	}
	return true
}

func equalValidationDerived(left, right ValidationSuiteManifest) bool {
	return left.Contract == right.Contract &&
		left.MultipleTestingEvidence.Method == right.MultipleTestingEvidence.Method &&
		left.SharpeEvidence == right.SharpeEvidence &&
		left.Stability == right.Stability &&
		left.Disclaimer == right.Disclaimer &&
		equalMaturities(left.EligibleMaturities, right.EligibleMaturities)
}

func equalMaturities(left, right []Maturity) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneValidationSuiteInput(input ValidationSuiteInput) ValidationSuiteInput {
	cloned := input
	cloned.Sources = append([]EvidenceSource(nil), input.Sources...)
	cloned.WalkForward = append([]WalkForwardFold(nil), input.WalkForward...)
	cloned.Neighborhood = append([]ResultSlice(nil), input.Neighborhood...)
	cloned.Capacity = append([]CapacityPoint(nil), input.Capacity...)
	cloned.Stress = append([]ResultSlice(nil), input.Stress...)
	cloned.Benchmarks = append([]ResultSlice(nil), input.Benchmarks...)
	cloned.Regimes = append([]ResultSlice(nil), input.Regimes...)
	cloned.MultipleTesting.RawPValues = append([]string(nil), input.MultipleTesting.RawPValues...)
	cloned.Criteria = append([]PromotionCriterion(nil), input.Criteria...)
	sort.Slice(cloned.Sources, func(left, right int) bool {
		if cloned.Sources[left].Mode != cloned.Sources[right].Mode {
			return cloned.Sources[left].Mode < cloned.Sources[right].Mode
		}
		return cloned.Sources[left].RunID < cloned.Sources[right].RunID
	})
	sort.Slice(cloned.Criteria, func(left, right int) bool {
		return cloned.Criteria[left].ID < cloned.Criteria[right].ID
	})
	return cloned
}
