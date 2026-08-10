package sandbox

import (
	"axiom/internal/backtest"
	"axiom/internal/execution"
)

// StrategyPipelineDependencies are the already-constructed shared execution
// stages for one exact automatic strategy evaluation. This type deliberately
// carries no adapter, credentials, private stream, or broker: durable plan
// approval is the final action available through this construction path.
type StrategyPipelineDependencies struct {
	Allocator backtest.Allocator
	Risk      backtest.RiskEngine
	Planner   execution.ExecutionPlanner
	Store     DispatcherRepository
	Kill      KillPoint
}

// NewAdmittedSingleVenueStrategyPipeline composes the shared evaluation,
// allocation, central-risk, and planning stages with an exact admitted
// Testnet/Demo plan builder. It only supports the one-leg strategies accepted
// by NewSingleVenueStrategyPlanBuilder; callers cannot substitute a different
// snapshot or strategy-owned inventory after admission.
func NewAdmittedSingleVenueStrategyPipeline(
	admission StrategySessionAdmission,
	snapshot AccountSnapshot,
	inventory StrategyOwnedInventory,
	strategy backtest.Strategy,
	dependencies StrategyPipelineDependencies,
	limits SubmissionLimits,
) (*StrategyPipeline, error) {
	builder, err := NewSingleVenueStrategyPlanBuilder(admission, snapshot, inventory)
	if err != nil {
		return nil, err
	}
	return NewStrategyPipeline(strategy, dependencies.Allocator, dependencies.Risk,
		dependencies.Planner, builder, dependencies.Store, limits, dependencies.Kill)
}
