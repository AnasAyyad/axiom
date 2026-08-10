package research

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestResearchPromotionPreregistrationIsCanonicalCompleteAndBeforeFinalWindow(t *testing.T) {
	input := completeResearchPromotionRegistrationInput()
	first, canonical, err := RegisterExperiment(input)
	second, secondCanonical, secondErr := RegisterExperiment(input)
	if err != nil || secondErr != nil || first.RegistrationHash != second.RegistrationHash ||
		!bytes.Equal(canonical, secondCanonical) || len(first.RegistrationHash) != 64 {
		t.Fatalf("registration = %#v %#v %v %v", first, second, err, secondErr)
	}
	validated, err := ValidateExperimentRegistrationCanonical(canonical, first.RegistrationHash)
	if err != nil || validated.RegistrationHash != first.RegistrationHash {
		t.Fatalf("canonical registration = %#v %v", validated, err)
	}
	tampered := bytes.Replace(canonical, []byte(`"minimum_trades":100`),
		[]byte(`"minimum_trades":101`), 1)
	if _, err = ValidateExperimentRegistrationCanonical(tampered, first.RegistrationHash); err == nil {
		t.Fatal("tampered preregistration accepted")
	}
	input.RegisteredAt = input.Split.FinalTest.Start
	if _, _, err = RegisterExperiment(input); err == nil {
		t.Fatal("registration at the final-test boundary accepted")
	}
}

func TestResearchPromotionMultipleTestingAndSharpeAreDeterministicAndTrialDeflated(t *testing.T) {
	multiple, err := AdjustMultipleTests(MultipleTestingInput{Method: "benjamini_hochberg_fdr.v1",
		Alpha: "0.05", RawPValues: []string{"0.001", "0.02", "0.4", "0.8"}})
	if err != nil ||
		!reflect.DeepEqual(multiple.AdjustedPValues, []string{"0.004", "0.04", "0.533333333333", "0.8"}) ||
		!reflect.DeepEqual(multiple.Rejected, []bool{true, true, false, false}) {
		t.Fatalf("multiple-testing evidence = %#v %v", multiple, err)
	}
	input := SharpeInput{ObservedSharpe: "2", BenchmarkSharpe: "0", Skewness: "0",
		ExcessKurtosis: "0", Observations: 400, IndependentTrials: 10}
	first, err := AnalyzeSharpe(input)
	second, secondErr := AnalyzeSharpe(input)
	if err != nil || secondErr != nil || first != second ||
		first.DeflatedBenchmarkSharpe == "0" ||
		first.DeflatedSharpeProbability > first.ProbabilisticSharpeProbability {
		t.Fatalf("Sharpe evidence = %#v %#v %v %v", first, second, err, secondErr)
	}
}

func TestResearchPromotionStatisticsMatchIndependentResearchGolden(t *testing.T) {
	payload, err := os.ReadFile("../../research/testdata/research_promotion_statistics_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		MultipleInput MultipleTestingInput    `json:"multiple_testing_input"`
		Multiple      MultipleTestingEvidence `json:"multiple_testing"`
		SharpeInput   SharpeInput             `json:"sharpe_input"`
		Sharpe        SharpeEvidence          `json:"sharpe"`
	}
	if err = json.Unmarshal(payload, &golden); err != nil {
		t.Fatal(err)
	}
	multiple, multipleErr := AdjustMultipleTests(golden.MultipleInput)
	sharpe, sharpeErr := AnalyzeSharpe(golden.SharpeInput)
	if multipleErr != nil || sharpeErr != nil ||
		!reflect.DeepEqual(multiple, golden.Multiple) || sharpe != golden.Sharpe {
		t.Fatalf("research promotion golden mismatch: %#v %#v %v %v", multiple, sharpe,
			multipleErr, sharpeErr)
	}
}

func TestResearchPromotionValidationSuiteQualifiesOnlyTierAPrimaryStatisticalEvidence(t *testing.T) {
	registration, _, err := RegisterExperiment(completeResearchPromotionRegistrationInput())
	if err != nil {
		t.Fatal(err)
	}
	input := completeResearchPromotionValidationInput(registration)
	manifest, canonical, err := BuildValidationSuite(registration, input)
	if err != nil || !manifest.EligibleForMaturity(MaturityBacktestValidated) ||
		!manifest.EligibleForMaturity(MaturityReplayValidated) ||
		!manifest.EligibleForMaturity(MaturityShadowValidated) ||
		manifest.EligibleForMaturity(MaturitySandboxIntegrationValidated) {
		t.Fatalf("eligible suite = %#v %v", manifest.EligibleMaturities, err)
	}
	validated, err := ValidateValidationSuiteCanonical(registration, canonical, manifest.ManifestHash)
	if err != nil || validated.ManifestHash != manifest.ManifestHash {
		t.Fatalf("canonical suite = %#v %v", validated, err)
	}
	tampered := bytes.Replace(canonical, []byte(`"observed_trades":120`),
		[]byte(`"observed_trades":121`), 1)
	if _, err = ValidateValidationSuiteCanonical(registration, tampered, manifest.ManifestHash); err == nil {
		t.Fatal("tampered suite accepted")
	}

	lowConfidence := completeResearchPromotionValidationInput(registration)
	lowConfidence.Sources[0].DatasetTier = "low_confidence"
	lowManifest, _, err := BuildValidationSuite(registration, lowConfidence)
	if err != nil || len(lowManifest.EligibleMaturities) != 0 {
		t.Fatalf("low-confidence primary evidence qualified = %#v %v",
			lowManifest.EligibleMaturities, err)
	}
	integration := completeResearchPromotionValidationInput(registration)
	integration.Sources[0].Mode = "integration"
	integrationManifest, _, err := BuildValidationSuite(registration, integration)
	if err != nil || len(integrationManifest.EligibleMaturities) != 0 {
		t.Fatalf("integration primary evidence qualified = %#v %v",
			integrationManifest.EligibleMaturities, err)
	}
}

func TestResearchPromotionValidationRejectsIncompleteEvidenceAndAllowsSupplementalIntegrationFacts(t *testing.T) {
	registration, _, _ := RegisterExperiment(completeResearchPromotionRegistrationInput())
	input := completeResearchPromotionValidationInput(registration)
	input.Sources = append(input.Sources, EvidenceSource{RunID: "integration-run-supplemental",
		Mode: "integration", DatasetTier: "integration_only", ConfidenceLabel: "insufficient",
		ResultHash: strings.Repeat("9", 64), Primary: false})
	manifest, _, err := BuildValidationSuite(registration, input)
	if err != nil || !manifest.EligibleForMaturity(MaturityShadowValidated) {
		t.Fatalf("supplemental integration fact changed eligibility = %#v %v",
			manifest.EligibleMaturities, err)
	}
	input.Stress = input.Stress[:5]
	if _, _, err = BuildValidationSuite(registration, input); err == nil {
		t.Fatal("incomplete stress suite accepted")
	}
}

func TestResearchPromotionChampionChallengerReportIsEvidenceNotPromotion(t *testing.T) {
	result := func(name string) ResultSlice {
		return ResultSlice{Name: name, NetReturn: "0.02", MaxDrawdown: "0.03", Trades: 120}
	}
	input := ChampionChallengerInput{ID: "comparison-research_promotion", StrategyFamily: "trend",
		ChampionVersionID: "trend-v1", ChallengerVersionID: "trend-v2",
		ChampionEvidenceHash:   strings.Repeat("a", 64),
		ChallengerEvidenceHash: strings.Repeat("b", 64),
		Overall:                []ResultSlice{result("champion"), result("challenger")},
		Regimes:                []ResultSlice{result("up"), result("down")},
		Disposition:            "recommend_challenger", Reason: "registered criteria passed",
		CreatedAt: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)}
	report, canonical, err := BuildChampionChallengerReport(input)
	if err != nil || report.ManifestHash == "" ||
		report.Disclaimer != DisclaimerNoProductionProfitability || !json.Valid(canonical) {
		t.Fatalf("comparison report = %#v %v", report, err)
	}
}

func BenchmarkResearchPromotionValidationSuite(b *testing.B) {
	registration, _, _ := RegisterExperiment(completeResearchPromotionRegistrationInput())
	input := completeResearchPromotionValidationInput(registration)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := BuildValidationSuite(registration, input); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzResearchPromotionMultipleTestingPreservesProbabilityBounds(f *testing.F) {
	f.Add("0.001", "0.02", "0.4")
	f.Add("0", "0.5", "1")
	f.Fuzz(func(t *testing.T, first, second, third string) {
		evidence, err := AdjustMultipleTests(MultipleTestingInput{
			Method: "benjamini_hochberg_fdr.v1", Alpha: "0.05",
			RawPValues: []string{first, second, third}})
		if err != nil {
			return
		}
		for _, adjusted := range evidence.AdjustedPValues {
			if !validProbability(adjusted) {
				t.Fatalf("adjusted probability out of bounds: %q", adjusted)
			}
		}
	})
}

func completeResearchPromotionRegistrationInput() ExperimentRegistrationInput {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	models := make([]ModelAssumption, 0, 6)
	for index, name := range []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"} {
		models = append(models, ModelAssumption{Name: name,
			VersionHash: strings.Repeat(string(rune('a'+index)), 64)})
	}
	return ExperimentRegistrationInput{ID: "experiment-research_promotion",
		ResearchGenerationID: "generation-research_promotion-1", StrategyVersionID: "trend-v1",
		Generation: 1, Hypothesis: "Net expectancy remains positive after registered stresses.",
		PrimaryMetric: "risk_adjusted_net_return",
		ParameterSearch: []SearchParameter{
			{ID: "breakout_window", Values: []string{"19", "20", "21"}},
			{ID: "risk_fraction", Values: []string{"0.004", "0.005", "0.006"}},
		},
		Split: ChronologicalSplit{
			Train:      Window{Name: "train", Start: start, End: start.Add(100 * time.Hour)},
			Validation: Window{Name: "validation", Start: start.Add(100 * time.Hour), End: start.Add(150 * time.Hour)},
			FinalTest:  Window{Name: "final_test", Start: start.Add(150 * time.Hour), End: start.Add(200 * time.Hour)},
		},
		Models: models, Benchmarks: []string{"cash", "buy_and_hold", "static_inventory"},
		MinimumSamples: 400, MinimumTrades: 100, MinimumShadowDuration: 72 * time.Hour,
		MinimumDeflatedSharpeProbability: "0.95",
		StoppingRule:                     "stop after the locked final window closes",
		RejectionRule:                    "reject when any registered criterion fails",
		PromotionRule:                    "require tier A primary evidence and sequential maturity",
		RegisteredSeedHash:               strings.Repeat("f", 64), RegisteredAt: start.Add(149 * time.Hour)}
}

func completeResearchPromotionValidationInput(
	registration ExperimentRegistrationManifest,
) ValidationSuiteInput {
	result := func(name string) ResultSlice {
		return ResultSlice{Name: name, NetReturn: "0.02", MaxDrawdown: "0.03", Trades: 120}
	}
	stress := make([]ResultSlice, 0, 6)
	for _, name := range []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"} {
		stress = append(stress, result(name))
	}
	confidence, _ := BlockBootstrapMean(
		[]string{"0.01", "0.02", "0.03", "0.04"}, 2, 100, "research_promotion-registered-seed",
	)
	sources := []EvidenceSource{
		{RunID: "backtest-research_promotion", Mode: "backtest", DatasetTier: "tier_a",
			ConfidenceLabel: "formal_tier_a", ResultHash: strings.Repeat("1", 64), Primary: true},
		{RunID: "replay-research_promotion", Mode: "replay", DatasetTier: "tier_a",
			ConfidenceLabel: "formal_tier_a", ResultHash: strings.Repeat("2", 64), Primary: true},
		{RunID: "shadow-research_promotion", Mode: "shadow", DatasetTier: "tier_a",
			ConfidenceLabel: "formal_tier_a", ResultHash: strings.Repeat("3", 64), Primary: true},
	}
	return ValidationSuiteInput{ID: "suite-research_promotion",
		ResearchGenerationID: registration.ResearchGenerationID,
		StrategyVersionID:    registration.StrategyVersionID, StrategyFamily: "trend",
		PreregistrationHash:      registration.RegistrationHash,
		FinalTestConsumptionHash: strings.Repeat("4", 64), Sources: sources,
		WalkForward: []WalkForwardFold{{TrainStart: 0, TrainEnd: 200,
			ValidationStart: 200, ValidationEnd: 300, TestStart: 300, TestEnd: 400}},
		Confidence:   confidence,
		Neighborhood: []ResultSlice{result("base"), result("low"), result("high")},
		Capacity: []CapacityPoint{{Notional: "10", NetReturn: "0.02", FillRate: "1"},
			{Notional: "100", NetReturn: "0.01", FillRate: "0.9"}},
		Stress: stress, Benchmarks: []ResultSlice{result("cash"),
			result("buy_and_hold"), result("static_inventory")},
		Regimes: []ResultSlice{result("up"), result("down"), result("sideways")},
		MultipleTesting: MultipleTestingInput{Method: "benjamini_hochberg_fdr.v1",
			Alpha: "0.05", RawPValues: []string{"0.001", "0.02", "0.4", "0.8"}},
		Sharpe: SharpeInput{ObservedSharpe: "2", BenchmarkSharpe: "0",
			Skewness: "0", ExcessKurtosis: "0", Observations: 400, IndependentTrials: 10},
		ObservedSamples: 400, ObservedTrades: 120, ObservedShadowDuration: 72 * time.Hour,
		Criteria: []PromotionCriterion{{ID: "confidence_lower_positive", Passed: true},
			{ID: "stressed_net_return_positive", Passed: true}},
		ConfidenceLabel:      "formal_tier_a",
		PlatformCorrectness:  "Deterministic platform verification passed.",
		StrategyEvidence:     "Tier A statistical evidence remains uncertain.",
		ViabilityDisposition: "viable_for_more_research",
		CreatedAt:            registration.Split.FinalTest.End.Add(time.Hour)}
}
