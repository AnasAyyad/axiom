package bootstrap

import (
	"context"
	"testing"

	"axiom/internal/config"
	"axiom/internal/sandbox"
)

func TestSandboxEngineStrategyEvaluationRunsOnlyThroughTheExplicitFreshCycle(t *testing.T) {
	product, err := config.DefaultSandboxConfiguration(config.ModeTestnet)
	if err != nil {
		t.Fatal(err)
	}
	product.Sandbox.IntegrationsEnabled = true
	product.Sandbox.SubmissionEnabled = true
	for index := range product.Sandbox.Exchanges {
		if product.Sandbox.Exchanges[index].ID == string(sandbox.ExchangeBinance) {
			product.Sandbox.Exchanges[index].IntegrationEnabled = true
			product.Sandbox.Exchanges[index].SubmissionEnabled = true
		}
	}
	scheduler := &sandboxStrategyCycleScheduler{}
	loop := sandboxEngineLoop{work: &sandboxEngineRoleWork{
		product: product, exchange: sandbox.ExchangeBinance,
	}, scheduler: scheduler}
	if err = loop.evaluateStrategies(context.Background()); err != nil || scheduler.calls != 1 {
		t.Fatalf("fresh strategy cycle calls=%d error=%v", scheduler.calls, err)
	}
	loop.work.product.Sandbox.SubmissionEnabled = false
	if err = loop.evaluateStrategies(context.Background()); err != nil || scheduler.calls != 1 {
		t.Fatalf("disabled strategy cycle calls=%d error=%v", scheduler.calls, err)
	}
	loop.work.product.Sandbox.SubmissionEnabled = true
	loop.scheduler = nil
	if err = loop.evaluateStrategies(context.Background()); err == nil {
		t.Fatal("enabled engine accepted a missing automatic strategy scheduler")
	}
}

type sandboxStrategyCycleScheduler struct{ calls int }

func (scheduler *sandboxStrategyCycleScheduler) Tick(
	context.Context,
) (sandbox.StrategySessionScheduleResult, error) {
	scheduler.calls++
	return sandbox.StrategySessionScheduleResult{}, nil
}
