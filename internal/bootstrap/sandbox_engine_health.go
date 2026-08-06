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
}

func newSandboxEngineHealth() sandboxEngineHealth {
	return newSandboxEngineHealthAt(time.Now().UTC())
}

func newSandboxEngineHealthAt(at time.Time) sandboxEngineHealth {
	recovery, err := sandbox.NewReconciliationRecovery(at)
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
		health.dispatchAllowed = false
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
	health.dispatchAllowed = false
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
		health.dispatchAllowed = false
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
	if health.recovery == nil {
		recovery, recoveryErr := sandbox.NewReconciliationRecovery(time.Now().UTC())
		if recoveryErr != nil {
			return recoveryErr
		}
		health.recovery = recovery
	}
	err := loop.reconcile(ctx, true)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		kind, cause := sandbox.ClassifyReconciliationFailure(err)
		transition, recoveryErr := health.recovery.ObserveFailure(
			time.Now().UTC(), kind, cause,
		)
		health.reconciliationHealthy = false
		health.dispatchAllowed = false
		if recoveryErr != nil {
			return recoveryErr
		}
		if transition.State != sandbox.RecoveryActive {
			return fmt.Errorf("sandbox_reconciliation_recovery_state_%s", transition.State)
		}
		return health.transition(ctx, loop)
	}
	if health.recovery.Active() {
		transition, recoveryErr := health.recovery.ObserveClean(
			time.Now().UTC(), sandbox.ReconciliationRecoveryHealth{
				StreamHealthy:       health.privateHealthy,
				EvidenceHealthy:     true,
				LeaseHeld:           health.leaseHeld,
				AccountSafe:         health.exchangeEligible,
				ReconciliationClean: true,
			},
		)
		if recoveryErr != nil {
			return recoveryErr
		}
		health.reconciliationHealthy = transition.State == sandbox.RecoveryRecovered
		health.dispatchAllowed = transition.State == sandbox.RecoveryRecovered
	} else {
		health.reconciliationHealthy = true
		health.dispatchAllowed = true
	}
	return health.transition(ctx, loop)
}

func (health *sandboxEngineHealth) transition(
	ctx context.Context,
	loop sandboxEngineLoop,
) error {
	target := health.exchangeEligible &&
		health.privateHealthy &&
		health.reconciliationHealthy &&
		health.recovery != nil && !health.recovery.Active()
	ready, err := loop.transitionReadiness(ctx, health.ready, target)
	health.ready = ready
	return err
}
