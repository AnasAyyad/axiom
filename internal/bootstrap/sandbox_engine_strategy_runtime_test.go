package bootstrap

import (
	"context"
	"errors"
	"testing"
)

func TestSandboxRefreshingStrategySchedulerKeepsPublicFailureSemantic(t *testing.T) {
	market := &sandboxSagaRefreshFixture{err: errors.New("public unavailable")}
	inner := &sandboxStrategyCycleScheduler{}
	scheduler := &sandboxRefreshingStrategyScheduler{market: market, scheduler: inner}
	if _, err := scheduler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if market.calls != 1 || inner.calls != 1 {
		t.Fatalf("refresh=%d scheduler=%d", market.calls, inner.calls)
	}
}

type sandboxSagaRefreshFixture struct {
	calls int
	err   error
}

func (fixture *sandboxSagaRefreshFixture) Refresh(context.Context) error {
	fixture.calls++
	return fixture.err
}

var _ sandboxStrategyScheduler = (*sandboxRefreshingStrategyScheduler)(nil)
var _ sandboxSagaMarketRefresher = (*sandboxSagaRefreshFixture)(nil)
