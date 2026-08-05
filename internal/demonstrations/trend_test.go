package demonstrations

import (
	"context"
	"strings"
	"testing"
)

func TestTrendFollowingWalkthroughUsesTheSharedPipelineDeterministically(t *testing.T) {
	first, err := RunTrendFollowing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := RunTrendFollowing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != TrendFollowingID || first.StrategyID != "trend-following" ||
		first.StrategyVersion != "trend-following@1.0.0" || !first.Synthetic ||
		len(first.ConfigurationHash) != 64 || first.ResultHash == "" ||
		first.ResultHash != second.ResultHash {
		t.Fatalf("non-deterministic walkthrough=%+v", first)
	}
	if len(first.Accepted.Orders) == 0 || len(first.Accepted.ExecutionEvents) == 0 ||
		string(first.Rejected.Orders) != "[]" ||
		!strings.Contains(string(first.Rejected.Decision), "trend.reject.unhealthy_market") {
		t.Fatalf("incomplete walkthrough accepted=%s rejected=%s", first.Accepted.Orders, first.Rejected.Decision)
	}
}

func TestCatalogueOnlyExposesExecutableDemonstrations(t *testing.T) {
	items := Catalogue()
	if len(items) != 4 || items[0].ID != TrendFollowingID ||
		items[0].StrategyID != "trend-following" || items[1].ID != MeanReversionID ||
		items[1].StrategyID != "mean-reversion" || items[2].ID != RebalancingID ||
		items[2].StrategyID != "inventory-rebalancing" || items[3].ID != TriangularArbitrageID ||
		items[3].StrategyID != "triangular-arbitrage" || len(items[1].ExpectedOutcomes) < 5 {
		t.Fatalf("catalogue=%+v", items)
	}
	if _, err := Run(context.Background(), "unknown-demo"); err == nil {
		t.Fatal("unknown demonstration was accepted")
	}
}

func TestTriangularArbitrageWalkthroughUsesReadOnlyEvaluatorEvidence(t *testing.T) {
	first, err := RunTriangularArbitrage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), TriangularArbitrageID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AdvisoryOnly || first.ResultHash == "" || first.ResultHash != second.ResultHash ||
		string(first.Accepted.Orders) != "[]" || string(first.Accepted.ExecutionEvents) != "[]" ||
		!strings.Contains(string(first.AdvisoryEvidence), "USDT-BTC-ETH-USDT") ||
		!strings.Contains(string(first.AdvisoryEvidence), "no_eligible_cycle") {
		t.Fatalf("triangular result=%+v", first)
	}
}

func TestInventoryRebalancingWalkthroughIsAdvisoryAndDeterministic(t *testing.T) {
	first, err := RunInventoryRebalancing(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), RebalancingID)
	if err != nil {
		t.Fatal(err)
	}
	if !first.AdvisoryOnly || len(first.AdvisoryEvidence) == 0 || first.ResultHash == "" || first.ResultHash != second.ResultHash || string(first.Accepted.Orders) != "[]" || string(first.Accepted.ExecutionEvents) != "[]" || !strings.Contains(string(first.AdvisoryEvidence), "natural_reverse_arbitrage") || !strings.Contains(string(first.AdvisoryEvidence), "route_unavailable") {
		t.Fatalf("advisory result=%+v", first)
	}
}

func TestMeanReversionWalkthroughUsesTheSharedPipelineDeterministically(t *testing.T) {
	first, err := RunMeanReversion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), MeanReversionID)
	if err != nil {
		t.Fatal(err)
	}
	if first.ResultHash == "" || first.ResultHash != second.ResultHash ||
		len(first.Accepted.Orders) == 0 || len(first.Accepted.ExecutionEvents) == 0 ||
		string(first.Rejected.Orders) != "[]" ||
		!strings.Contains(string(first.Rejected.Decision), "mean_reversion.reject.unhealthy_market") {
		t.Fatalf("incomplete walkthrough accepted=%s rejected=%s", first.Accepted.Orders, first.Rejected.Decision)
	}
}
