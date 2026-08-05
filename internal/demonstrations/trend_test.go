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
	if len(items) != 1 || items[0].ID != TrendFollowingID ||
		items[0].StrategyID != "trend-following" || len(items[0].ExpectedOutcomes) < 5 {
		t.Fatalf("catalogue=%+v", items)
	}
	if _, err := Run(context.Background(), "unknown-demo"); err == nil {
		t.Fatal("unknown demonstration was accepted")
	}
}
