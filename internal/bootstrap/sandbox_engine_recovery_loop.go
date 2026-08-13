package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

func (loop sandboxEngineLoop) evaluateStrategies(ctx context.Context) error {
	if !loop.work.sandboxSubmissionEnabled() {
		return nil
	}
	if loop.scheduler == nil {
		return fmt.Errorf("sandbox_engine_strategy_scheduler_unavailable")
	}
	if _, err := loop.scheduler.Tick(ctx); err != nil {
		return fmt.Errorf("sandbox_engine_strategy_scheduler_failed")
	}
	return nil
}

func (loop sandboxEngineLoop) recover(
	ctx context.Context,
	eligible bool,
) error {
	if !eligible {
		return nil
	}
	started := time.Now()
	recovered, err := loop.recovery.RecoverOnce(ctx, started.UTC(), 2)
	if recovered == 0 && err == nil {
		return nil
	}
	occurredAt := time.Now().UTC()
	recordErr := loop.store.RecordEngineRuntimeEvent(
		ctx,
		loop.account.AccountID,
		loop.account.Epoch,
		loop.work.exchange,
		loop.fence,
		"UNKNOWN_RECOVERY",
		time.Since(started),
		err == nil,
		occurredAt,
	)
	if recordErr != nil {
		return recordErr
	}
	return err
}

func (loop sandboxEngineLoop) reconcile(
	ctx context.Context,
	eligible bool,
) error {
	if !eligible {
		return nil
	}
	started := time.Now()
	result, err := loop.work.reconcile(
		ctx, loop.store, loop.adapter, loop.account,
	)
	if err == nil && result.State != "clean" {
		err = fmt.Errorf(
			"sandbox_engine_reconciliation_state_%s", result.State,
		)
	}
	occurredAt := time.Now().UTC()
	failureKind, causeCode := sandbox.ClassifyReconciliationFailure(err)
	recordErr := loop.store.RecordEngineRuntimeReconciliationEvent(
		ctx,
		loop.account.AccountID,
		loop.account.Epoch,
		loop.work.exchange,
		loop.fence,
		time.Since(started),
		err == nil && result.State == "clean",
		failureKind,
		causeCode,
		occurredAt,
	)
	if recordErr != nil {
		return recordErr
	}
	if err != nil {
		return fmt.Errorf("sandbox_engine_reconciliation_failed: %w", err)
	}
	return nil
}
