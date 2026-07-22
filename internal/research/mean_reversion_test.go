package research

import (
	"encoding/json"
	"testing"
)

func TestMeanReversionReportRequiresRegimeTrendFilterFastDeclineMAEHoldingAndExactFailures(t *testing.T) {
	input := completeReportInput(t)
	delete(input.Breakdowns, "false_breakout")
	input.Breakdowns["fast_decline_failure"] = []ResultSlice{{Name: "fast_decline", NetReturn: "-0.1", MaxDrawdown: "0.2", Trades: 5}}
	input.Breakdowns["maximum_adverse_excursion"] = []ResultSlice{{Name: "mae", NetReturn: "-0.02", MaxDrawdown: "0.05", Trades: 5}}
	input.Breakdowns["trend_filter_comparison"] = []ResultSlice{{Name: "disabled", NetReturn: "-0.04", MaxDrawdown: "0.1", Trades: 5}}
	input.Rejections = map[string]uint64{"mean_reversion.reject.dangerous_regime": 4,
		"mean_reversion.reject.adx": 3, "mean_reversion.reject.market_quality": 2,
		"mean_reversion.failure.fast_decline": 1}
	manifest, err := BuildMeanReversionReport(input)
	if err != nil || manifest.Contract != MeanReversionReportContract || manifest.ManifestHash == "" {
		t.Fatalf("B3 report = %#v, %v", manifest, err)
	}
	canonical, _ := json.Marshal(manifest)
	restored, err := ValidateMeanReversionReportCanonical(canonical, manifest.ManifestHash,
		input.ResearchGenerationID, input.RunReferences[0])
	if err != nil || restored.ManifestHash != manifest.ManifestHash {
		t.Fatalf("B3 canonical report = %#v, %v", restored, err)
	}
	delete(input.Breakdowns, "fast_decline_failure")
	if _, err = BuildMeanReversionReport(input); err == nil {
		t.Fatal("missing fast-decline report accepted")
	}
}
