package bootstrap

import (
	"fmt"

	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/strategies/inventoryrebalancing"
)

// newInventoryRebalancingOperationalProcessor installs the deterministic
// recommendation optimizer in the complete no-action advisory pipeline.
func newInventoryRebalancingOperationalProcessor(claim backtest.JobClaim) (backtest.Processor, error) {
	if claim.Manifest.StrategyVersion != "inventory-rebalancing@1.0.0" ||
		claim.Manifest.Mode != "backtest" && claim.Manifest.Mode != "replay" ||
		config.Validate(claim.Configuration) != nil ||
		config.ValidateRebalancingConfiguration(claim.Configuration.Rebalancing) != nil {
		return nil, fmt.Errorf("inventory_rebalancing_runtime_configuration_invalid")
	}
	runtime, err := inventoryrebalancing.NewRuntime(claim.Configuration.Rebalancing, claim.Manifest.ConfigurationHash)
	if err != nil {
		return nil, err
	}
	return backtest.NewAdvisoryPipelineProcessor(backtest.AdvisoryPipelineDependencies{
		Strategy: runtime, Allocator: runtime, Risk: runtime, Planner: runtime,
		Accounting: runtime, Reconciliation: runtime,
		Metrics: func() backtest.Metrics {
			return backtest.Metrics{TotalNetReturn: "0", AnnualizedReturn: "0",
				MaximumDrawdown: "0", CurrentDrawdown: "0", SharpeRatio: "0",
				SortinoRatio: "0", CalmarRatio: "0", ProfitFactor: "0",
				Expectancy: "0", WinRate: "0", AverageWin: "0",
				AverageLoss: "0", LargestWin: "0", LargestLoss: "0",
				Turnover: "0", Exposure: "0", FeesPaid: "0", FeePercentGrossProfit: "0",
				SlippagePercentGrossProfit: "0", RecoveryLoss: "0", TimeInMarket: "0",
				ByAsset: map[string]string{}, ByExchange: map[string]string{},
				ByStrategy: map[string]string{"inventory-rebalancing": "advisory_only"}, ByRegime: map[string]string{}}
		},
	})
}
