package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"time"
)

// ValidationSuiteContract is the canonical research promotion multi-strategy evidence schema.
const ValidationSuiteContract = "multi-strategy-validation.v1"

// ExperimentRegistrationContract is the canonical research promotion preregistration schema.
const ExperimentRegistrationContract = "research-preregistration.v1"

var researchIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,191}$`)

// SearchParameter is one preregistered bounded strategy search dimension.
type SearchParameter struct {
	ID     string   `json:"id"`
	Values []string `json:"values"`
}

// ModelAssumption pins one research cost, quality, or execution model.
type ModelAssumption struct {
	Name        string `json:"name"`
	VersionHash string `json:"version_hash"`
}

// ExperimentRegistrationInput is the complete measurable contract fixed before
// the final test window can be observed.
type ExperimentRegistrationInput struct {
	ID                               string             `json:"id"`
	ResearchGenerationID             string             `json:"research_generation_id"`
	StrategyVersionID                string             `json:"strategy_version_id"`
	Generation                       uint32             `json:"generation"`
	Hypothesis                       string             `json:"hypothesis"`
	PrimaryMetric                    string             `json:"primary_metric"`
	ParameterSearch                  []SearchParameter  `json:"parameter_search"`
	Split                            ChronologicalSplit `json:"split"`
	Models                           []ModelAssumption  `json:"models"`
	Benchmarks                       []string           `json:"benchmarks"`
	MinimumSamples                   uint64             `json:"minimum_samples"`
	MinimumTrades                    uint64             `json:"minimum_trades"`
	MinimumShadowDuration            time.Duration      `json:"minimum_shadow_duration"`
	MinimumDeflatedSharpeProbability string             `json:"minimum_deflated_sharpe_probability"`
	StoppingRule                     string             `json:"stopping_rule"`
	RejectionRule                    string             `json:"rejection_rule"`
	PromotionRule                    string             `json:"promotion_rule"`
	RegisteredSeedHash               string             `json:"registered_seed_hash"`
	RegisteredAt                     time.Time          `json:"registered_at"`
}

// ExperimentRegistrationManifest is immutable canonical preregistration
// evidence. RegistrationHash authenticates the exact JSON bytes.
type ExperimentRegistrationManifest struct {
	Contract string `json:"contract"`
	ExperimentRegistrationInput
	RegistrationHash string `json:"-"`
}

// RegisterExperiment validates and seals one preregistration before final-test
// consumption.
func RegisterExperiment(input ExperimentRegistrationInput) (ExperimentRegistrationManifest, []byte, error) {
	input = cloneRegistrationInput(input)
	if err := validateRegistrationInput(input); err != nil {
		return ExperimentRegistrationManifest{}, nil, err
	}
	manifest := ExperimentRegistrationManifest{
		Contract: ExperimentRegistrationContract, ExperimentRegistrationInput: input,
	}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ExperimentRegistrationManifest{}, nil, researchError("registration_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	manifest.RegistrationHash = hex.EncodeToString(digest[:])
	return manifest, canonical, nil
}

// ValidateExperimentRegistrationCanonical proves exact stored ResearchPromotion
// preregistration bytes and identity.
func ValidateExperimentRegistrationCanonical(
	canonical []byte,
	expectedHash string,
) (ExperimentRegistrationManifest, error) {
	var stored ExperimentRegistrationManifest
	if !json.Valid(canonical) || json.Unmarshal(canonical, &stored) != nil ||
		stored.Contract != ExperimentRegistrationContract || !validEvidenceHash(expectedHash) {
		return ExperimentRegistrationManifest{}, researchError("registration_canonical_invalid")
	}
	rebuilt, encoded, err := RegisterExperiment(stored.ExperimentRegistrationInput)
	if err != nil || rebuilt.RegistrationHash != expectedHash || !bytes.Equal(encoded, canonical) {
		return ExperimentRegistrationManifest{}, researchError("registration_canonical_invalid")
	}
	return rebuilt, nil
}

func validateRegistrationInput(input ExperimentRegistrationInput) error {
	if !researchIdentifier.MatchString(input.ID) ||
		!researchIdentifier.MatchString(input.ResearchGenerationID) ||
		!researchIdentifier.MatchString(input.StrategyVersionID) ||
		input.Generation == 0 || input.Hypothesis == "" || input.PrimaryMetric == "" ||
		input.MinimumSamples == 0 || input.MinimumTrades == 0 ||
		input.MinimumSamples > math.MaxInt64 || input.MinimumTrades > math.MaxInt64 ||
		input.MinimumShadowDuration <= 0 || input.StoppingRule == "" ||
		input.RejectionRule == "" || input.PromotionRule == "" ||
		!validEvidenceHash(input.RegisteredSeedHash) ||
		input.RegisteredAt.Location() != time.UTC ||
		!input.RegisteredAt.Before(input.Split.FinalTest.Start) {
		return researchError("registration_incomplete")
	}
	if err := input.Split.Validate(); err != nil {
		return err
	}
	if !validProbability(input.MinimumDeflatedSharpeProbability) ||
		!validSearchParameters(input.ParameterSearch) ||
		!validModelAssumptions(input.Models) ||
		!containsStrings(input.Benchmarks, []string{"cash", "buy_and_hold", "static_inventory"}) {
		return researchError("registration_incomplete")
	}
	return nil
}

func validSearchParameters(parameters []SearchParameter) bool {
	if len(parameters) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		if !researchIdentifier.MatchString(parameter.ID) || len(parameter.Values) == 0 {
			return false
		}
		if _, duplicate := seen[parameter.ID]; duplicate {
			return false
		}
		seen[parameter.ID] = struct{}{}
		for _, value := range parameter.Values {
			if _, _, err := parseFiniteDecimal(value); err != nil {
				return false
			}
		}
	}
	return true
}

func validModelAssumptions(models []ModelAssumption) bool {
	required := []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"}
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		if !researchIdentifier.MatchString(model.Name) || !validEvidenceHash(model.VersionHash) {
			return false
		}
		if _, duplicate := seen[model.Name]; duplicate {
			return false
		}
		seen[model.Name] = struct{}{}
	}
	for _, name := range required {
		if _, exists := seen[name]; !exists {
			return false
		}
	}
	return true
}

func cloneRegistrationInput(input ExperimentRegistrationInput) ExperimentRegistrationInput {
	cloned := input
	cloned.ParameterSearch = make([]SearchParameter, len(input.ParameterSearch))
	for index, parameter := range input.ParameterSearch {
		cloned.ParameterSearch[index] = parameter
		cloned.ParameterSearch[index].Values = append([]string(nil), parameter.Values...)
	}
	cloned.Models = append([]ModelAssumption(nil), input.Models...)
	cloned.Benchmarks = append([]string(nil), input.Benchmarks...)
	sort.Slice(cloned.ParameterSearch, func(left, right int) bool {
		return cloned.ParameterSearch[left].ID < cloned.ParameterSearch[right].ID
	})
	sort.Slice(cloned.Models, func(left, right int) bool {
		return cloned.Models[left].Name < cloned.Models[right].Name
	})
	sort.Strings(cloned.Benchmarks)
	return cloned
}

func containsStrings(values, required []string) bool {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return false
		}
		set[value] = struct{}{}
	}
	for _, value := range required {
		if _, exists := set[value]; !exists {
			return false
		}
	}
	return true
}
