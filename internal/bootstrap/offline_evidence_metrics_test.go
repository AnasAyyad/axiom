package bootstrap

import (
	"encoding/json"
	"testing"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/simulation"
)

func TestOfflineFillCostsAttributeRecordedLatencySpreadAndSlippageExactly(t *testing.T) {
	signal, _ := domain.ParsePrice("99")
	executable, _ := domain.ParsePrice("100")
	spreadRate, _ := domain.ParsePercent("0.01")
	zero, _ := domain.ParsePercent("0")
	quantity, _ := domain.ParseQuantity("2")
	fillPrice, _ := domain.ParsePrice("103")
	fillID, _ := domain.NewVirtualFillID("evaluation-cost-fill")
	fee, _ := domain.ParseFee("0")
	context := offlineEventCostContext{signalReference: signal, executionPrice: executable,
		priceModel: simulation.PriceModel{Version: "evaluation-price-v1", Spread: spreadRate,
			Slippage: zero, Impact: zero, AdverseSelection: zero, DecimalScale: 18}}
	spread, slippage, latency, err := offlineFillCosts(context, domain.SideBuy,
		execution.FillFact{ID: fillID, Quantity: quantity, Price: fillPrice, Fee: fee, Ordinal: 1})
	if err != nil || rationalString(spread) != "2" || rationalString(slippage) != "4" ||
		rationalString(latency) != "2" {
		t.Fatalf("costs spread=%s slippage=%s latency=%s error=%v",
			rationalString(spread), rationalString(slippage), rationalString(latency), err)
	}
}

func TestOfflineDecisionRegimePreservesMarketCondition(t *testing.T) {
	if got := offlineDecisionRegime(json.RawMessage(`{"explanation":{"regime":"range_or_constructive"}}`)); got != "range_or_constructive" {
		t.Fatalf("regime=%q", got)
	}
	if got := offlineDecisionRegime(json.RawMessage(`{"action":"no_action"}`)); got != "unclassified" {
		t.Fatalf("fallback regime=%q", got)
	}
}

func TestEvaluationArbitrageMarketConditionUsesExactCandidateEconomics(t *testing.T) {
	tests := []struct {
		expected string
		worst    string
		want     string
	}{
		{expected: "0.002", worst: "0.0001", want: "robust_positive_edge"},
		{expected: "0.002", worst: "0", want: "cost_sensitive_edge"},
		{expected: "0", worst: "-0.001", want: "non_positive_edge"},
	}
	for _, test := range tests {
		got, err := evaluationArbitrageMarketCondition(test.expected, test.worst)
		if err != nil || got != test.want {
			t.Fatalf("condition expected=%s worst=%s got=%q error=%v", test.expected, test.worst, got, err)
		}
	}
	if _, err := evaluationArbitrageMarketCondition("not-a-decimal", "0"); err == nil {
		t.Fatal("invalid candidate economics were accepted")
	}
}

func TestEvaluationMultilegMetricsPreserveConditionPnLAndSampleCounts(t *testing.T) {
	metrics, err := newEvaluationMultilegMetrics("triangular-arbitrage", "2000")
	if err != nil {
		t.Fatal(err)
	}
	profit, _ := rational("3.25")
	loss, _ := rational("-1.5")
	metrics.observeCondition("robust_positive_edge", profit)
	metrics.observeCondition("robust_positive_edge", loss)
	metrics.observeNoOpportunity()
	report := metrics.Metrics()
	if report.ByRegime["robust_positive_edge"] != "1.75" ||
		report.ByRegime["no_eligible_opportunity"] != "0" ||
		report.ByStrategy["condition_samples.robust_positive_edge"] != "2" ||
		report.ByStrategy["condition_samples.no_eligible_opportunity"] != "1" {
		t.Fatalf("condition evidence by_regime=%v by_strategy=%v", report.ByRegime, report.ByStrategy)
	}
}
