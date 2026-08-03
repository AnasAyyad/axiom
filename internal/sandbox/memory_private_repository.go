package sandbox

import (
	"context"
	"encoding/json"

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
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	return repository.commitMemoryReduction(ctx, event.Identity, id, record, kill)
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
		if reservation.OrderID == event.OrderID.String() && reservation.ReleasedAt == nil {
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
	terminal, filled, recovery, legs := 0, 0, false, 0
	for id, record := range repository.outbox {
		if id == replacedID {
			record = replacement
		}
		if record.Submission.PlanID.String() != planID {
			continue
		}
		legs++
		order := repository.reducers[record.Submission.OrderID.String()].Snapshot()
		if record.State == OutboxUnknown ||
			order.State == execution.OrderUnknown ||
			order.State == execution.OrderRecoveryRequired {
			recovery = true
		}
		if record.State == OutboxTerminal {
			terminal++
			if order.State == execution.OrderFilled {
				filled++
			}
		}
	}
	switch {
	case recovery || (terminal == legs && filled > 0 && filled < legs):
		repository.planStates[planID] = "RECOVERY_REQUIRED"
	case terminal == legs && filled == legs:
		repository.planStates[planID] = "COMPLETED"
	case terminal == legs:
		repository.planStates[planID] = "FAILED"
	default:
		repository.planStates[planID] = "ACTIVE"
	}
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
