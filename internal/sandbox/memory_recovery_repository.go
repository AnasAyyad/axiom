package sandbox

import (
	"context"
	"sort"
	"time"

	"axiom/internal/execution"
)

// ListUnknown returns bounded, deterministically ordered unknown submissions
// claimed by the exact account lease owner and fencing token.
func (repository *memoryDispatcherRepository) ListUnknown(
	_ context.Context,
	account AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]SubmissionOutbox, error) {
	if account == "" || epoch == 0 || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 32 {
		return nil, contractError("unknown_list_invalid")
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	ids := make([]string, 0)
	for id, record := range repository.outbox {
		if record.Submission.AccountID == account &&
			record.Submission.AccountEpoch == epoch &&
			record.State == OutboxUnknown &&
			record.ClaimOwner == owner &&
			record.FencingToken == fence {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]SubmissionOutbox, 0, len(ids))
	for _, id := range ids {
		result = append(result, repository.outbox[id])
	}
	return result, nil
}

// RecordReconciliation appends one reconciliation result and quarantines all
// affected in-memory capacity when the result is not explainable.
func (repository *memoryDispatcherRepository) RecordReconciliation(
	_ context.Context,
	result ReconciliationResult,
) error {
	if err := result.Validate(); err != nil {
		return err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	if prior, exists := repository.reconciliations[result.ID]; exists {
		if prior.EvidenceHash != result.EvidenceHash || prior.State != result.State ||
			prior.AccountID != result.AccountID || prior.AccountEpoch != result.AccountEpoch {
			return contractError("reconciliation_identity_conflict")
		}
		return nil
	}
	repository.reconciliations[result.ID] = result
	if result.State != "quarantined" {
		return nil
	}
	plans := make(map[string]struct{})
	for id, record := range repository.outbox {
		if record.Submission.AccountID != result.AccountID ||
			record.Submission.AccountEpoch != result.AccountEpoch ||
			record.State == OutboxTerminal {
			continue
		}
		record.State = OutboxUnknown
		repository.outbox[id] = record
		plans[record.Submission.PlanID.String()] = struct{}{}
	}
	for id, reservation := range repository.reservations {
		if reservation.AccountID == result.AccountID &&
			reservation.AccountEpoch == result.AccountEpoch &&
			reservation.ReleasedAt == nil {
			reservation.State = ReservationQuarantined
			repository.reservations[id] = reservation
		}
	}
	for planID := range plans {
		repository.planStates[planID] = "QUARANTINED"
	}
	return nil
}

// ResolveReconciledTerminal releases a zero-fill terminal reservation only
// after a clean reconciliation for the exact account epoch.
func (repository *memoryDispatcherRepository) ResolveReconciledTerminal(
	ctx context.Context,
	outboxID string,
	fence uint64,
	reconciliationID string,
	now time.Time,
	kill KillPoint,
) (bool, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	reconciliation := repository.reconciliations[reconciliationID]
	record, err := repository.validateMemoryReconciledTerminal(
		outboxID, fence, reconciliation, now,
	)
	if err != nil {
		return false, err
	}
	if record.State == OutboxTerminal {
		return false, nil
	}
	order := repository.reducers[record.Submission.OrderID.String()].Snapshot()
	if !requiresTerminalReconciliation(order.State) || len(order.Fills) != 0 {
		return false, nil
	}
	if err := kill.Hit(ctx, KillBeforeReservationRelease); err != nil {
		return false, err
	}
	repository.releaseMemoryReservation(record, order.State, now)
	if err := kill.Hit(ctx, KillAfterReservationRelease); err != nil {
		return false, err
	}
	record.State = OutboxTerminal
	record.UpdatedAt = now
	repository.updateMemoryPlanState(
		record.Submission.PlanID.String(),
		outboxID,
		record,
	)
	repository.outbox[outboxID] = record
	return true, nil
}

func (repository *memoryDispatcherRepository) validateMemoryReconciledTerminal(
	outboxID string,
	fence uint64,
	reconciliation ReconciliationResult,
	now time.Time,
) (SubmissionOutbox, error) {
	record, exists := repository.outbox[outboxID]
	if reconciliation.ID == "" || reconciliation.State != "clean" || !exists ||
		record.FencingToken != fence ||
		record.Submission.AccountID != reconciliation.AccountID ||
		record.Submission.AccountEpoch != reconciliation.AccountEpoch ||
		now.Before(reconciliation.ReconciledAt) ||
		(record.State != OutboxUnknown && record.State != OutboxTerminal) {
		return SubmissionOutbox{}, contractError("reconciled_terminal_invalid")
	}
	return record, nil
}

func (repository *memoryDispatcherRepository) releaseMemoryReservation(
	record SubmissionOutbox,
	orderState execution.OrderState,
	now time.Time,
) {
	for id, reservation := range repository.reservations {
		if reservation.OrderID != record.Submission.OrderID.String() ||
			reservation.ReleasedAt != nil {
			continue
		}
		releasedAt := now
		reservation.State = ReservationReleased
		reservation.ReleasedAt = &releasedAt
		reservation.ReleaseReason = string(orderState)
		repository.reservations[id] = reservation
	}
}

var _ UnknownRecoveryRepository = (*memoryDispatcherRepository)(nil)
