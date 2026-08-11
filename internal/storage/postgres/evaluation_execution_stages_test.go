package postgres

import (
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/evaluation"
)

func TestEvaluationRunWindowKeepsBothMatricesBlindToFinalTwentyPercent(t *testing.T) {
	selection := evaluationDatasetSelection{first: 1, split: 80, last: 100}
	for _, mode := range []string{"backtest", "replay"} {
		first, last, err := evaluationRunWindow(selection,
			evaluation.PlannedRun{Mode: mode, RepeatOrdinal: 0})
		if err != nil || first != 1 || last != 80 {
			t.Fatalf("%s matrix window = %d..%d error=%v", mode, first, last, err)
		}
	}
	first, last, err := evaluationRunWindow(selection,
		evaluation.PlannedRun{Mode: "replay", RepeatOrdinal: 2})
	if err != nil || first != 81 || last != 100 {
		t.Fatalf("locked final window = %d..%d error=%v", first, last, err)
	}
}

func TestEvaluationMetricFlagEveryRequiresExplicitLedgerEvidence(t *testing.T) {
	healthy := backtest.CanonicalResult{Metrics: backtest.Metrics{ByStrategy: map[string]string{
		"accounting_reconciled":    "true",
		"negative_inventory_count": "0",
	}}}
	if !evaluationMetricFlagEvery([]backtest.CanonicalResult{healthy, healthy}, "accounting_reconciled", true) {
		t.Fatal("explicit reconciled ledger evidence was rejected")
	}
	if !evaluationMetricFlagEvery([]backtest.CanonicalResult{healthy}, "negative_inventory_count", false) {
		t.Fatal("explicit zero violation count was rejected")
	}

	missing := backtest.CanonicalResult{Metrics: backtest.Metrics{ByStrategy: map[string]string{}}}
	unsafe := healthy
	unsafe.Metrics.ByStrategy = map[string]string{"negative_inventory_count": "1"}
	for name, values := range map[string][]backtest.CanonicalResult{
		"missing": {missing},
		"unsafe":  {unsafe},
		"mixed":   {healthy, missing},
	} {
		t.Run(name, func(t *testing.T) {
			if evaluationMetricFlagEvery(values, "negative_inventory_count", false) {
				t.Fatal("absent or nonzero ledger evidence passed")
			}
		})
	}
}
