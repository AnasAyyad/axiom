package bootstrap

import (
	"context"
	"testing"
	"time"

	"axiom/internal/sandbox"
)

func TestSandboxStrategyExecutorRouterUsesOnlyTheMatchingPipelineFamily(t *testing.T) {
	now := time.Date(2026, 8, 9, 18, 30, 0, 0, time.UTC)
	single := &routerExecutor{}
	saga := &routerExecutor{}
	router, err := NewSandboxStrategyExecutorRouter(single, saga)
	if err != nil {
		t.Fatal(err)
	}
	for _, strategy := range []string{sandbox.StrategyTrend, sandbox.StrategyMeanReversion,
		sandbox.StrategyTriangular, sandbox.StrategyCrossExchangeArbitrage} {
		work, record := readinessWorkAndConfiguration(t, strategy, now)
		if _, err = router.EvaluateStrategySession(context.Background(), work, record,
			readinessExecutionLease(work), now); err != nil {
			t.Fatal(err)
		}
	}
	if single.calls != 2 || saga.calls != 2 {
		t.Fatalf("single=%d saga=%d", single.calls, saga.calls)
	}
}

type routerExecutor struct{ calls int }

func (executor *routerExecutor) EvaluateStrategySession(
	_ context.Context,
	work sandbox.StrategySessionWork,
	_ sandbox.StrategySessionConfiguration,
	_ sandbox.StrategySessionExecutionLease,
	now time.Time,
) (sandbox.StrategySessionEvaluation, error) {
	executor.calls++
	return sandbox.NewStrategySessionEvaluation(work, sandbox.StrategySessionEvaluationWaiting,
		"waiting_for_strategy_trigger", now)
}
