package evaluation

import "testing"

func TestBalancedFullDefinitionIsCompleteAndCapitalBalanced(t *testing.T) {
	definition := BalancedFullDefinition()
	if err := ValidateBalancedFullDefinition(definition); err != nil {
		t.Fatal(err)
	}
	if len(CapitalLevelsMicros) != 4 || CapitalLevelsMicros[0] != 500_000_000 ||
		CapitalLevelsMicros[3] != 2_000_000_000 {
		t.Fatalf("capital levels = %#v", CapitalLevelsMicros)
	}
	if got := len(BalancedFullRuns()); got != 136 {
		t.Fatalf("run count = %d", got)
	}
	for _, candidate := range definition {
		final := BalancedFinalRuns(candidate)
		if len(final) != 2 || final[0].RepeatOrdinal != 2 || final[1].RepeatOrdinal != 2 ||
			final[0].CostStressBPS != 10_000 || final[1].CostStressBPS != 15_000 {
			t.Fatalf("final runs for %s = %#v", candidate.ConfigurationKey, final)
		}
	}
}

func TestCandidateVerdictsFailClosedAndPreserveLosingEvidence(t *testing.T) {
	base := EvidenceMetrics{NetResultMicros: 1_000_000, Stress15NetMicros: 100_000,
		GrossProfitMicros: 5_000_000, LargestWinMicros: 1_000_000, MaximumDrawdownBPS: 250,
		TradeCount: 30, DatasetCorrect: true, RuntimeCorrect: true, AccountingReconciled: true,
		NoNegativeInventory: true, NoDuplicateFill: true, NoUnsupportedSale: true,
		DeterministicRepeat: true}
	if verdict, _ := EvaluateCandidate(StrategyTrend, base, BalancedSelectionPolicy()); verdict != VerdictContinue {
		t.Fatalf("verdict = %s", verdict)
	}
	base.NetResultMicros = -1
	if verdict, reason := EvaluateCandidate(StrategyTrend, base, BalancedSelectionPolicy()); verdict != VerdictReject || reason != "FINAL_TEST_NOT_POSITIVE" {
		t.Fatalf("verdict = %s, reason = %s", verdict, reason)
	}
	base.DatasetCorrect = false
	if verdict, _ := EvaluateCandidate(StrategyTrend, base, BalancedSelectionPolicy()); verdict != VerdictBlocked {
		t.Fatalf("verdict = %s", verdict)
	}
}

func TestInventoryUsesAdvisoryEvidenceNotFakeTrades(t *testing.T) {
	metrics := EvidenceMetrics{RouteEvidenceCount: 20, SnapshotEvidenceCount: 1,
		DatasetCorrect: true, RuntimeCorrect: true, AccountingReconciled: true,
		NoNegativeInventory: true, NoDuplicateFill: true, NoUnsupportedSale: true,
		DeterministicRepeat: true}
	if verdict, _ := EvaluateCandidate(StrategyInventory, metrics, BalancedSelectionPolicy()); verdict != VerdictContinue {
		t.Fatalf("verdict = %s", verdict)
	}
}
