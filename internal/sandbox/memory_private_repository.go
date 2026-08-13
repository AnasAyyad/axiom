package sandbox

import (
	"context"
	"encoding/json"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

// AppendPrivateEvent deduplicates and reduces one durable normalized event.
func (repository *memoryDispatcherRepository) AppendPrivateEvent(
	ctx context.Context,
	outboxID string,
	fence uint64,
	event PrivateEvent,
	kill KillPoint,
) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if err := kill.Hit(ctx, KillBeforeInboxAppend); err != nil {
		return err
	}
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	reduced, err := repository.appendMemoryInbox(ctx, event, kill)
	if err != nil || reduced {
		return err
	}
	if event.OrderEvent == nil {
		return repository.commitMemoryReduction(ctx, event.Identity, "", SubmissionOutbox{}, kill)
	}
	return repository.reduceMemoryOrderEvent(ctx, outboxID, fence, event, kill)
}

func (repository *memoryDispatcherRepository) appendMemoryInbox(
	ctx context.Context,
	event PrivateEvent,
	kill KillPoint,
) (bool, error) {
	if prior, exists := repository.privateEvents[event.Identity]; exists {
		priorJSON, priorErr := json.Marshal(prior)
		eventJSON, eventErr := json.Marshal(event)
		if priorErr != nil || eventErr != nil || string(priorJSON) != string(eventJSON) {
			return false, contractError("private_event_identity_conflict")
		}
		if repository.reducedEvents[event.Identity] {
			return true, nil
		}
	} else {
		repository.privateEvents[event.Identity] = event
		if err := kill.Hit(ctx, KillAfterInboxAppend); err != nil {
			delete(repository.privateEvents, event.Identity)
			return false, err
		}
		if err := kill.Hit(ctx, KillBeforeInboxCommit); err != nil {
			delete(repository.privateEvents, event.Identity)
			return false, err
		}
		if err := kill.Hit(ctx, KillAfterInboxCommit); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (repository *memoryDispatcherRepository) reduceMemoryOrderEvent(
	ctx context.Context,
	outboxID string,
	fence uint64,
	event PrivateEvent,
	kill KillPoint,
) error {
	id := outboxID
	if id == "" {
		id = repository.orderOutbox[event.OrderID.String()]
	}
	record, exists := repository.outbox[id]
	if !exists || record.FencingToken > fence {
		return contractError("stale_fencing_token")
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	reduction, err := repository.reducers[event.OrderID.String()].Reduce(*event.OrderEvent)
	if err != nil {
		return err
	}
	if err := kill.Hit(ctx, KillAfterReducerUpdate); err != nil {
		return err
	}
	record.State, record.UpdatedAt = OutboxAcknowledged, event.ReceivedAt
	if terminalState(reduction.Order.State) {
		if err := repository.applyMemoryTerminal(ctx, event, reduction, kill); err != nil {
			return err
		}
		record.State = OutboxTerminal
	} else if requiresTerminalReconciliation(reduction.Order.State) {
		record.State = OutboxUnknown
	}
	repository.outbox[id] = record
	if record.State == OutboxTerminal {
		repository.advanceMemorySequentialPlan(record, reduction.Order, event.ReceivedAt)
	}
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	return repository.commitMemoryReduction(ctx, event.Identity, id, record, kill)
}

func (repository *memoryDispatcherRepository) advanceMemorySequentialPlan(
	completed SubmissionOutbox,
	order execution.Order,
	now time.Time,
) {
	plan, exists := repository.plans[completed.Submission.PlanID.String()]
	policy, err := ValidatePlanSaga(plan)
	if !exists || err != nil || policy != execution.DispatchSequential ||
		len(plan.Submissions) == 1 {
		return
	}
	if order.State == execution.OrderFilled {
		nextIndex := completed.LegIndex + 1
		for id, record := range repository.outbox {
			if record.Submission.PlanID.String() != plan.ID ||
				record.LegIndex != nextIndex || record.State != OutboxWaiting ||
				record.DependsOn == nil || *record.DependsOn != completed.LegIndex {
				continue
			}
			executionActive := plan.ExecutionExpiresAt == nil || now.Before(*plan.ExecutionExpiresAt)
			output, outputErr := FilledSubmissionOutput(completed.Submission, order)
			reservationID, reservation, reservationOK := repository.memoryWaitingReservation(record)
			required, requiredErr := domain.ParseBalance(reservation.Quantity)
			if !plan.Arm.Active(now) || !executionActive || outputErr != nil ||
				!reservationOK || requiredErr != nil ||
				reservation.Asset != string(output.Asset) ||
				required.Compare(output.Quantity) > 0 {
				repository.quarantineMemoryWaitingReservations(plan.ID)
				return
			}
			reservation.State = ReservationActive
			repository.reservations[reservationID] = reservation
			record.State, record.UpdatedAt = OutboxPending, now
			repository.outbox[id] = record
			return
		}
		return
	}
	if order.State != execution.OrderCanceled && order.State != execution.OrderRejected &&
		order.State != execution.OrderExpired {
		return
	}
	repository.expireMemoryDependentLegs(plan.ID, completed.LegIndex, now)
}

func (repository *memoryDispatcherRepository) memoryWaitingReservation(
	record SubmissionOutbox,
) (string, DurableReservation, bool) {
	for id, reservation := range repository.reservations {
		if reservation.OrderID == record.Submission.OrderID.String() &&
			reservation.State == ReservationWaiting && reservation.ReleasedAt == nil {
			return id, reservation, true
		}
	}
	return "", DurableReservation{}, false
}

func (repository *memoryDispatcherRepository) expireMemoryDependentLegs(
	planID string,
	completedIndex uint32,
	now time.Time,
) {
	for id, record := range repository.outbox {
		if record.Submission.PlanID.String() != planID ||
			record.LegIndex <= completedIndex || record.State != OutboxWaiting {
			continue
		}
		reducer := repository.reducers[record.Submission.OrderID.String()]
		_, err := reducer.Reduce(orderEvent(
			record.Submission, execution.OrderExpired,
			"dependency_not_filled", 6, now,
		))
		if err != nil {
			record.State, record.UpdatedAt = OutboxWaiting, now
			repository.outbox[id] = record
			continue
		}
		record.State, record.UpdatedAt = OutboxTerminal, now
		repository.outbox[id] = record
		repository.releaseMemoryWaitingReservation(record, now, "dependency_not_filled")
	}
}

func (repository *memoryDispatcherRepository) releaseMemoryWaitingReservation(
	record SubmissionOutbox,
	now time.Time,
	reason string,
) {
	for id, reservation := range repository.reservations {
		if reservation.OrderID != record.Submission.OrderID.String() ||
			reservation.State != ReservationWaiting || reservation.ReleasedAt != nil {
			continue
		}
		releasedAt := now
		reservation.State, reservation.ReleasedAt = ReservationReleased, &releasedAt
		reservation.ReleaseReason = reason
		repository.reservations[id] = reservation
	}
}

func (repository *memoryDispatcherRepository) quarantineMemoryWaitingReservations(planID string) {
	for id, record := range repository.outbox {
		if record.Submission.PlanID.String() != planID || record.State != OutboxWaiting {
			continue
		}
		for reservationID, reservation := range repository.reservations {
			if reservation.OrderID == record.Submission.OrderID.String() &&
				(reservation.State == ReservationWaiting ||
					reservation.State == ReservationActive) && reservation.ReleasedAt == nil {
				reservation.State = ReservationQuarantined
				repository.reservations[reservationID] = reservation
			}
		}
		repository.outbox[id] = record
	}
}

func (repository *memoryDispatcherRepository) applyMemoryTerminal(
	ctx context.Context,
	event PrivateEvent,
	reduction execution.Reduction,
	kill KillPoint,
) error {
	if len(reduction.Order.Fills) > 0 {
		if err := kill.Hit(ctx, KillBeforeFillPosting); err != nil {
			return err
		}
		if err := kill.Hit(ctx, KillAfterFillPosting); err != nil {
			return err
		}
	}
	if err := kill.Hit(ctx, KillBeforeReservationRelease); err != nil {
		return err
	}
	for reservationID, reservation := range repository.reservations {
		if reservation.OrderID == event.OrderID.String() &&
			reservation.State == ReservationActive && reservation.ReleasedAt == nil {
			released := event.ReceivedAt
			reservation.ReleasedAt, reservation.ReleaseReason = &released, string(reduction.Order.State)
			reservation.State = ReservationReleased
			if len(reduction.Order.Fills) > 0 {
				reservation.State = ReservationConsumed
			}
			repository.reservations[reservationID] = reservation
		}
	}
	return kill.Hit(ctx, KillAfterReservationRelease)
}

func (repository *memoryDispatcherRepository) updateMemoryPlanState(
	planID, replacedID string,
	replacement SubmissionOutbox,
) {
	progress := repository.memoryPlanProgress(planID, replacedID, replacement)
	progress.recovery = progress.recovery || repository.hasQuarantinedPlanReservation(planID)
	plan := repository.plans[planID]
	waitingAfterExpiredArm := progress.filled > 0 && progress.waiting > 0 && !plan.Arm.Active(replacement.UpdatedAt)
	switch {
	case progress.recovery || waitingAfterExpiredArm ||
		(progress.terminal == progress.legs && progress.filled > 0 && progress.filled < progress.legs):
		repository.planStates[planID] = "RECOVERY_REQUIRED"
	case progress.terminal == progress.legs && progress.filled == progress.legs:
		repository.planStates[planID] = "COMPLETED"
	case progress.terminal == progress.legs:
		repository.planStates[planID] = "FAILED"
	default:
		repository.planStates[planID] = "ACTIVE"
	}
}

type memoryPlanProgress struct {
	terminal, filled, waiting, legs int
	recovery                        bool
}

func (repository *memoryDispatcherRepository) memoryPlanProgress(planID, replacedID string,
	replacement SubmissionOutbox,
) memoryPlanProgress {
	var progress memoryPlanProgress
	for id, record := range repository.outbox {
		if id == replacedID {
			record = replacement
		}
		if record.Submission.PlanID.String() != planID {
			continue
		}
		progress.legs++
		order := repository.reducers[record.Submission.OrderID.String()].Snapshot()
		if record.State == OutboxWaiting {
			progress.waiting++
		}
		if record.State == OutboxUnknown ||
			order.State == execution.OrderUnknown ||
			order.State == execution.OrderRecoveryRequired ||
			order.State == execution.OrderPartiallyFilled {
			progress.recovery = true
		}
		if record.State == OutboxTerminal {
			progress.terminal++
			if order.State == execution.OrderFilled {
				progress.filled++
			}
		}
	}
	return progress
}

func (repository *memoryDispatcherRepository) hasQuarantinedPlanReservation(planID string) bool {
	for _, reservation := range repository.reservations {
		if reservation.State != ReservationQuarantined {
			continue
		}
		for _, submission := range repository.plans[planID].Submissions {
			if reservation.OrderID == submission.OrderID.String() {
				return true
			}
		}
	}
	return false
}

func (repository *memoryDispatcherRepository) commitMemoryReduction(
	ctx context.Context,
	eventIdentity, outboxID string,
	record SubmissionOutbox,
	kill KillPoint,
) error {
	if err := kill.Hit(ctx, KillBeforeReductionCommit); err != nil {
		return err
	}
	if outboxID != "" {
		repository.outbox[outboxID] = record
	}
	repository.reducedEvents[eventIdentity] = true
	return kill.Hit(ctx, KillAfterReductionCommit)
}
