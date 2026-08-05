package demonstrations

import (
	"context"
	"fmt"
)

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
	}}
}

// Run executes one catalogue scenario. Unknown IDs fail closed and never
// select another scenario by a partial or display-name match.
func Run(ctx context.Context, id string) (Result, error) {
	switch id {
	case TrendFollowingID:
		return RunTrendFollowing(ctx)
	default:
		return Result{}, fmt.Errorf("demonstration_not_found")
	}
}
