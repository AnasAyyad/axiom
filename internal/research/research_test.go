package research

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestChronologicalSplitAndWalkForwardNeverCrossFutureBoundaries(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	split := ChronologicalSplit{
		Train:      Window{Name: "train", Start: start, End: start.Add(10 * time.Hour)},
		Validation: Window{Name: "validation", Start: start.Add(10 * time.Hour), End: start.Add(15 * time.Hour)},
		FinalTest:  Window{Name: "final_test", Start: start.Add(15 * time.Hour), End: start.Add(20 * time.Hour)},
	}
	observations := make([]Observation, 20)
	for index := range observations {
		observations[index] = Observation{At: start.Add(time.Duration(index) * time.Hour), Return: "0.001"}
	}
	partitioned, err := split.Partition(observations)
	if err != nil || len(partitioned["train"]) != 10 || len(partitioned["validation"]) != 5 || len(partitioned["final_test"]) != 5 {
		t.Fatalf("partition = %#v, %v", partitioned, err)
	}
	folds, err := WalkForward(100, 40, 10, 10, 10)
	if err != nil || len(folds) != 5 {
		t.Fatalf("folds = %#v, %v", folds, err)
	}
	for _, fold := range folds {
		if fold.TrainEnd > fold.ValidationStart || fold.ValidationEnd > fold.TestStart {
			t.Fatalf("future leakage fold = %#v", fold)
		}
	}
}

func TestSingleRunReportIsPersistableAndCannotClaimSuiteCoverage(t *testing.T) {
	hash := strings.Repeat("a", 64)
	report, canonical, err := BuildRunEvidenceReport(RunEvidenceInput{ResearchGenerationID: "generation-1",
		RunID: "run-1", Mode: "backtest", ResultHash: hash, ReproducibilityHash: strings.Repeat("b", 64),
		Metrics:   map[string]string{"total_net_return": "0.01", "trades": "1"},
		CreatedAt: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC)})
	digest := sha256.Sum256(canonical)
	if err != nil || report.ManifestHash != hex.EncodeToString(digest[:]) ||
		report.ConfidenceLabel != "insufficient" || report.Viability != "undetermined" ||
		report.Coverage["baseline"] != "completed" ||
		report.Coverage["benchmarks"] != "not_established_by_single_run" ||
		report.Disclaimer != DisclaimerNoProductionProfitability {
		t.Fatalf("single-run report = %#v %v", report, err)
	}
}

func TestBlockBootstrapIsSeededDeterministicAndPreservesExactMean(t *testing.T) {
	returns := []string{"0.01", "-0.02", "0.03", "0", "0.02", "-0.01"}
	first, err := BlockBootstrapMean(returns, 2, 200, "registered-seed-v1")
	second, secondErr := BlockBootstrapMean(returns, 2, 200, "registered-seed-v1")
	if err != nil || secondErr != nil || !reflect.DeepEqual(first, second) || first.Point != "0.005" || len(first.SeedHash) != 64 {
		t.Fatalf("bootstrap = %#v %#v %v %v", first, second, err, secondErr)
	}
	changed, _ := BlockBootstrapMean(returns, 2, 200, "registered-seed-v2")
	if first.SeedHash == changed.SeedHash {
		t.Fatal("different registered seeds produced identical seed evidence")
	}
}

func TestReportRequiresAllStressBenchmarksBreakdownsAndNoProfitClaim(t *testing.T) {
	input := completeReportInput(t)
	first, err := BuildReport(input)
	second, secondErr := BuildReport(input)
	if err != nil || secondErr != nil || first.ManifestHash != second.ManifestHash || len(first.ManifestHash) != 64 ||
		first.Disclaimer != DisclaimerNoProductionProfitability || !first.Stability.Stable {
		t.Fatalf("report = %#v, %v %v", first, err, secondErr)
	}
	input.StrategyEvidence = "This strategy will profit"
	if _, err = BuildReport(input); err == nil {
		t.Fatal("misleading profitability claim was accepted")
	}
}

func TestReportManifestHashAuthenticatesReturnedCanonicalBytes(t *testing.T) {
	report, err := BuildReport(completeReportInput(t))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	if actual := hex.EncodeToString(digest[:]); actual != report.ManifestHash {
		t.Fatalf("manifest hash = %s, canonical digest = %s", report.ManifestHash, actual)
	}
	if bytes.Contains(canonical, []byte("manifest_hash")) {
		t.Fatalf("self-referential hash leaked into canonical manifest: %s", canonical)
	}
}

func TestRegisteredReportCanonicalValidationRejectsTamperingAndMissingRun(t *testing.T) {
	report, err := BuildReport(completeReportInput(t))
	if err != nil {
		t.Fatal(err)
	}
	canonical, _ := json.Marshal(report)
	validated, err := ValidateReportCanonical(canonical, report.ManifestHash, "generation-1", "run-1")
	if err != nil || validated.ManifestHash != report.ManifestHash {
		t.Fatalf("valid report rejected = %#v %v", validated, err)
	}
	if _, err = ValidateReportCanonical(canonical, report.ManifestHash, "generation-1", "missing-run"); err == nil {
		t.Fatal("missing run reference accepted")
	}
	tampered := bytes.Replace(canonical, []byte(`"net_return":"0.01"`), []byte(`"net_return":"0.02"`), 1)
	if _, err = ValidateReportCanonical(tampered, report.ManifestHash, "generation-1", "run-1"); err == nil {
		t.Fatal("tampered report accepted")
	}
}

func completeReportInput(t *testing.T) ReportInput {
	t.Helper()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	interval, err := BlockBootstrapMean([]string{"0.01", "0", "0.02", "-0.01"}, 2, 100, "seed")
	if err != nil {
		t.Fatal(err)
	}
	result := func(name string) ResultSlice {
		return ResultSlice{Name: name, NetReturn: "0.01", MaxDrawdown: "0.02", Trades: 20}
	}
	stressNames := []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"}
	stress := make([]ResultSlice, len(stressNames))
	for index, name := range stressNames {
		stress[index] = result(name)
	}
	return ReportInput{ResearchGenerationID: "generation-1", Hypothesis: "Strict breakouts may retain positive net expectancy after costs.",
		PrimaryMetric: "net_return", Split: ChronologicalSplit{
			Train:      Window{Name: "train", Start: start, End: start.Add(100 * time.Hour)},
			Validation: Window{Name: "validation", Start: start.Add(100 * time.Hour), End: start.Add(150 * time.Hour)},
			FinalTest:  Window{Name: "final_test", Start: start.Add(150 * time.Hour), End: start.Add(200 * time.Hour)}},
		WalkForward: []WalkForwardFold{{TrainStart: 0, TrainEnd: 40, ValidationStart: 40, ValidationEnd: 50, TestStart: 50, TestEnd: 60}},
		Confidence:  interval, Neighborhood: []ResultSlice{result("base"), result("ema_low"), result("ema_high")},
		Capacity: []CapacityPoint{{Notional: "10", NetReturn: "0.01", FillRate: "1"}, {Notional: "150", NetReturn: "0.005", FillRate: "0.9"}},
		Stress:   stress, Benchmarks: []ResultSlice{result("cash"), result("buy_and_hold"), result("static_inventory")},
		Breakdowns: map[string][]ResultSlice{"asset": {result("BTC")}, "regime": {result("up")},
			"holding_period": {result("short")}, "false_breakout": {result("false")}, "drawdown": {result("peak")}},
		Rejections: map[string]uint64{"trend.reject.breakout": 5}, RunReferences: []string{"run-2", "run-1"},
		ConfidenceLabel: "local_tier_b", PlatformCorrectness: "Deterministic platform checks passed locally.",
		StrategyEvidence: "Research evidence remains provisional and uncertain.", ViabilityDisposition: "undetermined", CreatedAt: start.Add(201 * time.Hour)}
}
