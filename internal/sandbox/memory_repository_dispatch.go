package sandbox

import (
	"context"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"

	"github.com/cockroachdb/apd/v3"
)

func (repository *memoryDispatcherRepository) validatePlanLeg(
	plan ApprovedSandboxPlan,
	submission Submission,
	reservation DurableReservation,
	maxOrder domain.Notional,
	armAccounts map[AccountID]struct{},
	accountOpen, globalOpen int,
	limits SubmissionLimits,
) (*execution.OrderReducer, *apd.Decimal, error) {
	if submission.ApprovedAt != plan.ApprovedAt || submission.PlanID.String() != plan.ID ||
		submission.Validate(maxOrder) != nil {
		return nil, nil, contractError("plan_submission_invalid")
	}
	if _, armed := armAccounts[submission.AccountID]; !armed {
		return nil, nil, contractError("plan_account_not_armed")
	}
	exchange := exchangeForAccount(submission.AccountID)
	if err := validateMemoryEntryEvidence(plan, submission, exchange); err != nil {
		return nil, nil, err
	}
	key := clientIdentity(submission)
	if prior, exists := repository.clientIDs[key]; exists && prior != submission.OrderID.String() {
		return nil, nil, contractError("client_order_id_conflict")
	}
	if accountOpen > limits.MaximumOpenPerAccount {
		return nil, nil, contractError("account_open_cap")
	}
	if globalOpen > limits.MaximumOpenGlobal {
		return nil, nil, contractError("global_open_cap")
	}
	value, _, err := apd.NewFromString(submission.Notional.String())
	if err != nil {
		return nil, nil, contractError("notional_invalid")
	}
	reducer, err := approvedReducer(submission)
	if err != nil {
		return nil, nil, err
	}
	if reservation.ValidateFor(submission) != nil {
		return nil, nil, contractError("reservation_invalid")
	}
	return reducer, value, nil
}

func validateMemoryEntryEvidence(
	plan ApprovedSandboxPlan,
	submission Submission,
	exchange Exchange,
) error {
	if submission.Action != IntentEntry {
		if len(plan.EntrySafety) != 0 {
			return contractError("entry_safety_rejected")
		}
		return nil
	}
	eligibility, ok := EligibilityForSubmission(plan, submission, exchange)
	if !ok || !eligibility.Eligible ||
		eligibility.Exchange != string(exchange) ||
		eligibility.Instrument != submission.Instrument.Symbol() ||
		eligibility.ObservedAt.IsZero() ||
		eligibility.ObservedAt.Location() != time.UTC ||
		eligibility.ObservedAt.After(plan.ApprovedAt) ||
		plan.ApprovedAt.Sub(eligibility.ObservedAt) > 250*time.Millisecond {
		return contractError("public_ineligible")
	}
	safety, ok := plan.EntrySafety[submission.AccountID]
	if !ok || safety.ValidateFor(submission, exchange, plan.ApprovedAt) != nil {
		return contractError("entry_safety_rejected")
	}
	return nil
}

// ClaimOutbox applies the model fencing lease and returns a bounded page.
func (repository *memoryDispatcherRepository) ClaimOutbox(
	ctx context.Context,
	account AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	ttl time.Duration,
	limit int,
	kill KillPoint,
) ([]SubmissionOutbox, error) {
	if err := kill.Hit(ctx, KillBeforeLeaseTransition); err != nil {
		return nil, err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	ids := make([]string, 0)
	for id, record := range repository.outbox {
		if record.Submission.AccountID == account && record.Submission.AccountEpoch == epoch &&
			repository.entryClaimAllowed(record, now) &&
			(record.State == OutboxPending ||
				(record.State == OutboxClaimed && !record.ClaimExpiresAt.After(now) && record.FencingToken < fence)) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	result := make([]SubmissionOutbox, 0, len(ids))
	for _, id := range ids {
		record := repository.outbox[id]
		record.State, record.ClaimOwner, record.FencingToken = OutboxClaimed, owner, fence
		record.ClaimExpiresAt, record.UpdatedAt, record.Attempt = now.Add(ttl), now, record.Attempt+1
		repository.outbox[id] = record
		result = append(result, record)
	}
	if err := kill.Hit(ctx, KillAfterLeaseTransition); err != nil {
		return nil, err
	}
	return result, nil
}

// MarkSubmitting advances the canonical model reducer before network I/O.
func (repository *memoryDispatcherRepository) MarkSubmitting(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	record, err := repository.claimed(id, fence)
	if err != nil {
		return err
	}
	if !repository.entryClaimAllowed(record, now) {
		return ErrEntryBlocked
	}
	reducer := repository.reducers[record.Submission.OrderID.String()]
	switch reducer.Snapshot().State {
	case execution.OrderSubmitting, execution.OrderAcknowledged,
		execution.OrderPartiallyFilled, execution.OrderCancelPending,
		execution.OrderFilled, execution.OrderCanceled, execution.OrderRejected,
		execution.OrderExpired, execution.OrderUnknown,
		execution.OrderRecoveryRequired, execution.OrderRecovered:
		record.UpdatedAt = now
		repository.outbox[id] = record
		return nil
	case execution.OrderApproved:
		// The canonical reducer transition is applied below.
	default:
		return contractError("order_recovery_state_invalid")
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	if _, err := reducer.Reduce(orderEvent(record.Submission, execution.OrderSubmitting, "dispatch", 5, now)); err != nil {
		return err
	}
	record.UpdatedAt = now
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	repository.outbox[id] = record
	return kill.Hit(ctx, KillAfterReducerUpdate)
}

func (repository *memoryDispatcherRepository) entryClaimAllowed(
	record SubmissionOutbox,
	now time.Time,
) bool {
	if record.Submission.Action != IntentEntry {
		return true
	}
	plan, exists := repository.plans[record.Submission.PlanID.String()]
	if !exists || !plan.Arm.Active(now) {
		return false
	}
	return plan.ExecutionExpiresAt == nil || now.Before(*plan.ExecutionExpiresAt)
}

// MarkUnknown quarantines an ambiguous transport outcome without releasing capacity.
func (repository *memoryDispatcherRepository) MarkUnknown(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	record, err := repository.claimed(id, fence)
	if err != nil {
		return err
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	if _, err := repository.reducers[record.Submission.OrderID.String()].Reduce(
		orderEvent(record.Submission, execution.OrderUnknown, "ambiguous_timeout", 6, now),
	); err != nil {
		return err
	}
	record.State, record.UpdatedAt = OutboxUnknown, now
	record.ClaimExpiresAt = time.Time{}
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	repository.outbox[id] = record
	return kill.Hit(ctx, KillAfterReducerUpdate)
}
