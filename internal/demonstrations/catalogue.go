package demonstrations

import (
	"context"
	"errors"
)

// ErrNotFound is returned only when a requested demonstration is not installed.
var ErrNotFound = errors.New("demonstration_not_found")

// Scenario is the server-owned description of one executable synthetic
// walkthrough. It intentionally contains no storage path, account identity,
// credential, or historical-performance claim.
type Scenario struct {
	ID               string
	Title            string
	Description      string
	StrategyID       string
	StrategyVersion  string
	ExpectedOutcomes []string
}

// Catalogue lists only scenarios that this build can execute through a real
// strategy package and shared pipeline.
func Catalogue() []Scenario {
	return []Scenario{{
		ID:              TrendFollowingID,
		Title:           "Trend Following basics",
		Description:     "A synthetic four-hour-candle walkthrough that shows one accepted entry through virtual execution and one market-health rejection.",
		StrategyID:      "trend-following",
		StrategyVersion: "trend-following@1.0.0",
		ExpectedOutcomes: []string{
			"Accepted strategy decision", "Allocator and central-risk approval",
			"Virtual order and partial simulated fill", "Portfolio and journal update",
			"Rejected decision when market health is unavailable",
		},
	}, {
		ID: MeanReversionID, Title: "Mean Reversion basics",
		Description: "A synthetic dual-timeframe walkthrough that shows one accepted entry through virtual execution and one market-health rejection.",
		StrategyID:  "mean-reversion", StrategyVersion: "mean-reversion@1.0.0",
		ExpectedOutcomes: []string{"Accepted strategy decision", "Allocator and central-risk approval", "Virtual order and partial simulated fill", "Portfolio and journal update", "Rejected decision when market health is unavailable"},
	}, {
		ID: RebalancingID, Title: "Inventory Rebalancing basics", Description: "A synthetic advisory walkthrough that prefers a reviewed natural reversal and rejects a stale route fact.", StrategyID: "inventory-rebalancing", StrategyVersion: "inventory-rebalancing@1.0.0", ExpectedOutcomes: []string{"Advisory natural-reversal recommendation", "Exact reviewed route costs and diagnostics", "Stale-fact rejection", "No order, transfer, or exchange submission"},
	}}
}

// Run executes one catalogue scenario. Unknown IDs fail closed and never
// select another scenario by a partial or display-name match.
func Run(ctx context.Context, id string) (Result, error) {
	switch id {
	case TrendFollowingID:
		return RunTrendFollowing(ctx)
	case MeanReversionID:
		return RunMeanReversion(ctx)
	case RebalancingID:
		return RunInventoryRebalancing(ctx)
	default:
		return Result{}, ErrNotFound
	}
}
