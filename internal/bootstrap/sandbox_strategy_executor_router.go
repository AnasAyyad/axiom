package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

// SandboxStrategyExecutorRouter keeps the scheduler generic while preserving
// separate one-leg and multi-leg construction boundaries. Neither delegate is
// an exchange adapter and the router grants no submission capability.
type SandboxStrategyExecutorRouter struct {
	single sandbox.StrategySessionExecutor
	saga   sandbox.StrategySessionExecutor
}

// NewSandboxStrategyExecutorRouter routes one-leg and multileg strategy families.
func NewSandboxStrategyExecutorRouter(
	single sandbox.StrategySessionExecutor,
	saga sandbox.StrategySessionExecutor,
) (*SandboxStrategyExecutorRouter, error) {
	if single == nil || saga == nil {
		return nil, fmt.Errorf("sandbox_strategy_executor_router_invalid")
	}
	return &SandboxStrategyExecutorRouter{single: single, saga: saga}, nil
}

// EvaluateStrategySession dispatches one evaluation to its strategy-family executor.
func (router *SandboxStrategyExecutorRouter) EvaluateStrategySession(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	record sandbox.StrategySessionConfiguration,
	lease sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	if router == nil || router.single == nil || router.saga == nil || ctx == nil {
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_executor_router_invalid")
	}
	switch work.Strategy {
	case sandbox.StrategyTrend, sandbox.StrategyMeanReversion:
		return router.single.EvaluateStrategySession(ctx, work, record, lease, now)
	case sandbox.StrategyTriangular, sandbox.StrategyCrossExchangeArbitrage:
		return router.saga.EvaluateStrategySession(ctx, work, record, lease, now)
	default:
		return sandbox.StrategySessionEvaluation{}, fmt.Errorf("sandbox_strategy_executor_router_unsupported")
	}
}

var _ sandbox.StrategySessionExecutor = (*SandboxStrategyExecutorRouter)(nil)
