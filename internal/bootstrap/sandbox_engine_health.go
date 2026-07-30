package bootstrap

import (
	"context"
	"fmt"
)

type sandboxEngineHealth struct {
	exchangeEligible      bool
	privateHealthy        bool
	reconciliationHealthy bool
	ready                 bool
}

func newSandboxEngineHealth() sandboxEngineHealth {
	return sandboxEngineHealth{
		exchangeEligible:      true,
		privateHealthy:        true,
		reconciliationHealthy: true,
		ready:                 true,
	}
}

func (health *sandboxEngineHealth) observePrivate(
	ctx context.Context,
	loop sandboxEngineLoop,
	signal sandboxPrivateStreamSignal,
) error {
	if ctx.Err() != nil {
		return nil
	}
	if signal.fatal != nil {
		return fmt.Errorf("sandbox_engine_private_stream_failed")
	}
	health.privateHealthy = signal.healthy
	if !signal.healthy {
		health.reconciliationHealthy = false
	}
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) observeRuntime(
	ctx context.Context,
	loop sandboxEngineLoop,
	runtimeErr error,
) error {
	if runtimeErr == nil || ctx.Err() != nil {
		return nil
	}
	health.reconciliationHealthy = false
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) refreshEligibility(
	ctx context.Context,
	loop sandboxEngineLoop,
) error {
	eligible, err := loop.refreshEligibility(
		ctx, health.exchangeEligible,
	)
	if err != nil {
		return err
	}
	health.exchangeEligible = eligible
	if !eligible {
		health.reconciliationHealthy = false
	}
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) reconcile(
	ctx context.Context,
	loop sandboxEngineLoop,
) error {
	if !health.exchangeEligible || !health.privateHealthy {
		return nil
	}
	err := loop.reconcile(ctx, true)
	if ctx.Err() != nil {
		return nil
	}
	health.reconciliationHealthy = err == nil
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) transition(
	ctx context.Context,
	loop sandboxEngineLoop,
) error {
	target := health.exchangeEligible &&
		health.privateHealthy &&
		health.reconciliationHealthy
	ready, err := loop.transitionReadiness(ctx, health.ready, target)
	health.ready = ready
	return err
}
