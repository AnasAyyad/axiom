package sandbox

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/execution"
)

// UnknownRecoveryRepository exposes only the durable facts needed to resolve
// ambiguous submissions under the currently fenced account lease.
type UnknownRecoveryRepository interface {
	ListUnknown(
		context.Context,
		AccountID,
		uint64,
		string,
		uint64,
		time.Time,
		int,
	) ([]SubmissionOutbox, error)
	AppendPrivateEvent(context.Context, string, uint64, PrivateEvent, KillPoint) error
	RecordReconciliation(context.Context, ReconciliationResult) error
	ResolveReconciledTerminal(
		context.Context,
		string,
		uint64,
		string,
		time.Time,
		KillPoint,
	) (bool, error)
}

// UnknownRecoveryHarness queries deterministic client IDs, appends normalized
// private facts, and reconciles authoritative account state. A record remains
// UNKNOWN unless durable order facts resolve it; clean reconciliation alone
// never fabricates a terminal outcome or releases a reservation.
type UnknownRecoveryHarness struct {
	account    AccountID
	epoch      uint64
	worker     string
	fence      uint64
	repository UnknownRecoveryRepository
	broker     OrderBroker
	reconciler Reconciler
	kill       KillPoint
}

// NewUnknownRecoveryHarness binds recovery to one exact account epoch and
// fencing token.
func NewUnknownRecoveryHarness(
	account AccountID,
	epoch uint64,
	worker string,
	fence uint64,
	repository UnknownRecoveryRepository,
	broker OrderBroker,
	reconciler Reconciler,
	kill KillPoint,
) (*UnknownRecoveryHarness, error) {
	if account == "" || epoch == 0 || worker == "" || fence == 0 ||
		repository == nil || broker == nil || reconciler == nil {
		return nil, contractError("unknown_recovery_invalid")
	}
	if kill == nil {
		kill = NoKillPoint{}
	}
	return &UnknownRecoveryHarness{
		account: account, epoch: epoch, worker: worker, fence: fence,
		repository: repository, broker: broker, reconciler: reconciler, kill: kill,
	}, nil
}

// RecoverOnce processes a bounded UNKNOWN page. Query or reconciliation
// failures leave every record and reservation quarantined by construction.
func (harness *UnknownRecoveryHarness) RecoverOnce(
	ctx context.Context,
	now time.Time,
	limit int,
) (int, error) {
	if now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 32 {
		return 0, contractError("unknown_recovery_attempt_invalid")
	}
	records, err := harness.repository.ListUnknown(
		ctx, harness.account, harness.epoch, harness.worker, harness.fence, now, limit,
	)
	if err != nil {
		return 0, err
	}
	recovered := 0
	for _, record := range records {
		terminalResolution, queryErr := harness.queryAndAppend(ctx, record)
		result, reconcileErr := harness.reconcile(ctx)
		if reconcileErr == nil {
			reconcileErr = harness.repository.RecordReconciliation(ctx, result)
		}
		if reconcileErr == nil && result.State == "clean" &&
			terminalResolution {
			_, reconcileErr = harness.repository.ResolveReconciledTerminal(
				ctx,
				record.ID,
				harness.fence,
				result.ID,
				result.ReconciledAt,
				harness.kill,
			)
		}
		if queryErr != nil || reconcileErr != nil {
			return recovered, fmt.Errorf(
				"unknown_recovery_failed: query=%v reconcile=%v",
				queryErr,
				reconcileErr,
			)
		}
		recovered++
	}
	return recovered, nil
}

func (harness *UnknownRecoveryHarness) queryAndAppend(
	ctx context.Context,
	record SubmissionOutbox,
) (bool, error) {
	if err := harness.kill.Hit(ctx, KillBeforeNetworkAttempt); err != nil {
		return false, err
	}
	events, err := harness.broker.Query(
		ctx,
		harness.account,
		harness.epoch,
		record.Submission.ClientOrderID,
	)
	if killErr := harness.kill.Hit(ctx, KillAfterNetworkAttempt); killErr != nil {
		return false, killErr
	}
	if err != nil {
		return false, err
	}
	terminalResolution := len(events) == 0
	for _, event := range events {
		if event.AccountID != harness.account || event.AccountEpoch != harness.epoch ||
			event.ClientOrderID != record.Submission.ClientOrderID ||
			(event.Kind != PrivateOrderEvent && event.Kind != PrivateFillEvent) {
			return false, contractError("unknown_recovery_event_mismatch")
		}
		if event.OrderEvent == nil {
			return false, contractError("unknown_recovery_event_mismatch")
		}
		switch event.OrderEvent.State {
		case execution.OrderFilled, execution.OrderCanceled,
			execution.OrderRejected, execution.OrderExpired:
			terminalResolution = true
		}
		if err = harness.repository.AppendPrivateEvent(
			ctx,
			record.ID,
			harness.fence,
			event,
			harness.kill,
		); err != nil {
			return false, err
		}
	}
	return terminalResolution, nil
}

func (harness *UnknownRecoveryHarness) reconcile(
	ctx context.Context,
) (ReconciliationResult, error) {
	if err := harness.kill.Hit(ctx, KillBeforeNetworkAttempt); err != nil {
		return ReconciliationResult{}, err
	}
	result, err := harness.reconciler.Reconcile(ctx, harness.account, harness.epoch)
	if killErr := harness.kill.Hit(ctx, KillAfterNetworkAttempt); killErr != nil {
		return ReconciliationResult{}, killErr
	}
	if err != nil {
		return ReconciliationResult{}, err
	}
	if result.Validate() != nil || result.AccountID != harness.account ||
		result.AccountEpoch != harness.epoch {
		return ReconciliationResult{}, contractError("unknown_reconciliation_invalid")
	}
	return result, nil
}
