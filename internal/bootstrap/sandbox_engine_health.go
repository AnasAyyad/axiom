package bootstrap

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

type sandboxEngineHealth struct {
	exchangeEligible      bool
	privateHealthy        bool
	leaseHeld             bool
	reconciliationHealthy bool
	ready                 bool
	dispatchAllowed       bool
	recovery              *sandbox.ReconciliationRecovery
	now                   func() time.Time
}

type sandboxEngineHealthLoop interface {
	refreshEligibility(context.Context, bool) (bool, error)
	reconcile(context.Context, bool) error
	evaluateStrategies(context.Context) error
	transitionReadiness(context.Context, bool, bool) (bool, error)
}

func newSandboxEngineHealth() sandboxEngineHealth {
	return newSandboxEngineHealthWithClock(func() time.Time {
		return time.Now().UTC()
	})
}

func newSandboxEngineHealthWithClock(now func() time.Time) sandboxEngineHealth {
	if now == nil {
		return sandboxEngineHealth{}
	}
	recovery, err := sandbox.NewReconciliationRecovery(now().UTC())
	if err != nil {
		return sandboxEngineHealth{}
	}
	return sandboxEngineHealth{
		exchangeEligible:      true,
		privateHealthy:        true,
		leaseHeld:             true,
		reconciliationHealthy: true,
		ready:                 true,
		dispatchAllowed:       true,
		recovery:              recovery,
		now:                   now,
	}
}

func (health *sandboxEngineHealth) observePrivate(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
	signal sandboxPrivateStreamSignal,
) error {
	if ctx.Err() != nil {
		return nil
	}
	if signal.fatal != nil {
		health.privateHealthy = false
		health.reconciliationHealthy = false
		health.dispatchAllowed = false
		if err := health.transition(ctx, loop); err != nil {
			return err
		}
		return fmt.Errorf("sandbox_engine_private_stream_failed")
	}
	health.privateHealthy = signal.healthy
	if !signal.healthy {
		transition, recoveryErr := health.recovery.ObserveIncident(
			health.nowUTC(), sandbox.RecoverySourcePrivateStream,
			signal.failureKind, signal.causeCode,
		)
		health.reconciliationHealthy = false
		health.dispatchAllowed = false
		if err := health.transition(ctx, loop); err != nil {
			return err
		}
		if recoveryErr != nil {
			return recoveryErr
		}
		if transition.State != sandbox.RecoveryActive {
			return fmt.Errorf("sandbox_private_stream_recovery_state_%s", transition.State)
		}
		return nil
	}
	if err := health.transition(ctx, loop); err != nil {
		return err
	}
	if signal.reconcileNow && health.recovery.Active() {
		return health.reconcile(ctx, loop)
	}
	return nil
}

func (health *sandboxEngineHealth) observeRuntime(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
	runtimeErr error,
) error {
	if runtimeErr == nil || ctx.Err() != nil {
		return nil
	}
	health.reconciliationHealthy = false
	health.dispatchAllowed = false
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) refreshEligibility(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
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
		health.dispatchAllowed = false
	}
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) reconcile(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
) error {
	if !health.exchangeEligible || !health.privateHealthy {
		return nil
	}
	if err := health.ensureRecovery(); err != nil {
		return err
	}
	err := loop.reconcile(ctx, true)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return health.observeReconciliationFailure(ctx, loop, err)
	}
	if err = health.observeCleanReconciliation(ctx, loop); err != nil {
		return err
	}
	if !health.reconciliationHealthy {
		return health.transition(ctx, loop)
	}
	return health.evaluateReconciledStrategies(ctx, loop)
}

func (health *sandboxEngineHealth) ensureRecovery() error {
	if health.recovery != nil {
		return nil
	}
	recovery, err := sandbox.NewReconciliationRecovery(health.nowUTC())
	if err == nil {
		health.recovery = recovery
	}
	return err
}

func (health *sandboxEngineHealth) observeReconciliationFailure(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
	reconciliationErr error,
) error {
	kind, cause := sandbox.ClassifyRecoveryFailure(reconciliationErr)
	transition, recoveryErr := health.recovery.ObserveIncident(
		health.nowUTC(), sandbox.RecoverySourceReconciliation, kind, cause,
	)
	health.reconciliationHealthy = false
	health.dispatchAllowed = false
	if err := health.transition(ctx, loop); err != nil {
		return err
	}
	if recoveryErr != nil {
		return recoveryErr
	}
	if transition.State != sandbox.RecoveryActive {
		return fmt.Errorf("sandbox_reconciliation_recovery_state_%s", transition.State)
	}
	return nil
}

func (health *sandboxEngineHealth) observeCleanReconciliation(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
) error {
	if health.recovery.Active() {
		transition, recoveryErr := health.recovery.ObserveClean(
			health.nowUTC(), sandbox.ReconciliationRecoveryHealth{
				StreamHealthy:       health.privateHealthy,
				EvidenceHealthy:     true,
				LeaseHeld:           health.leaseHeld,
				AccountSafe:         health.exchangeEligible,
				ReconciliationClean: true,
			},
		)
		if recoveryErr != nil {
			health.reconciliationHealthy = false
			health.dispatchAllowed = false
			if transitionErr := health.transition(ctx, loop); transitionErr != nil {
				return transitionErr
			}
			return recoveryErr
		}
		health.reconciliationHealthy = transition.State == sandbox.RecoveryRecovered
		health.dispatchAllowed = transition.State == sandbox.RecoveryRecovered
	} else {
		health.reconciliationHealthy = true
		health.dispatchAllowed = true
	}
	return nil
}

func (health *sandboxEngineHealth) evaluateReconciledStrategies(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
) error {
	// Automatic evaluations require account/reconciliation evidence from this
	// cycle and a synchronously refreshed public eligibility snapshot. During
	// bounded recovery this point is reachable only after the second clean
	// reconciliation has restored READY_PAUSED eligibility.
	eligible, refreshErr := loop.refreshEligibility(ctx, health.exchangeEligible)
	if refreshErr != nil {
		return refreshErr
	}
	health.exchangeEligible = eligible
	if !eligible {
		health.reconciliationHealthy = false
		health.dispatchAllowed = false
		return health.transition(ctx, loop)
	}
	if err := loop.evaluateStrategies(ctx); err != nil {
		health.reconciliationHealthy = false
		health.dispatchAllowed = false
	}
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) transition(
	ctx context.Context,
	loop sandboxEngineHealthLoop,
) error {
	target := health.exchangeEligible &&
		health.privateHealthy &&
		health.reconciliationHealthy &&
		health.dispatchAllowed &&
		health.recovery != nil && health.recovery.DispatchAllowed()
	ready, err := loop.transitionReadiness(ctx, health.ready, target)
	health.ready = ready
	return err
}

func (health *sandboxEngineHealth) nowUTC() time.Time {
	if health == nil || health.now == nil {
		return time.Time{}
	}
	return health.now().UTC()
}
