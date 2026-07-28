package sandbox

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/execution"
)

// MarkCancelPending durably binds one cancel attempt to the current account
// lease without consulting entry arm or public eligibility.
func (repository *memoryDispatcherRepository) MarkCancelPending(
	ctx context.Context,
	account AccountID,
	epoch uint64,
	clientOrderID, _ string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) (string, error) {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	outboxID, record, reducer, err := repository.memoryCancelableOrder(
		account, epoch, clientOrderID, fence,
	)
	if err != nil {
		return "", err
	}
	snapshot := reducer.Snapshot()
	if !memoryCancelableState(snapshot.State) {
		return "", contractError("cancel_order_state_invalid")
	}
	if snapshot.State != execution.OrderCancelPending {
		if err = reduceMemoryCancelPending(ctx, reducer, clientOrderID, snapshot, now, kill); err != nil {
			return "", err
		}
	}
	record.FencingToken, record.UpdatedAt = fence, now
	repository.outbox[outboxID] = record
	if err := kill.Hit(ctx, KillAfterReducerUpdate); err != nil {
		return "", err
	}
	return outboxID, nil
}

func (repository *memoryDispatcherRepository) memoryCancelableOrder(
	account AccountID,
	epoch uint64,
	clientOrderID string,
	fence uint64,
) (string, SubmissionOutbox, *execution.OrderReducer, error) {
	identity := fmt.Sprintf("%s|%d|%s", account, epoch, clientOrderID)
	orderID, exists := repository.clientIDs[identity]
	if !exists {
		return "", SubmissionOutbox{}, nil, contractError("cancel_order_missing")
	}
	outboxID := repository.orderOutbox[orderID]
	record, exists := repository.outbox[outboxID]
	if !exists || record.State == OutboxTerminal || record.FencingToken > fence {
		return "", SubmissionOutbox{}, nil, contractError("stale_fencing_token")
	}
	return outboxID, record, repository.reducers[orderID], nil
}

func memoryCancelableState(state execution.OrderState) bool {
	switch state {
	case execution.OrderAcknowledged, execution.OrderPartiallyFilled,
		execution.OrderCancelPending, execution.OrderUnknown,
		execution.OrderRecoveryRequired:
		return true
	default:
		return false
	}
}

func reduceMemoryCancelPending(
	ctx context.Context,
	reducer *execution.OrderReducer,
	clientOrderID string,
	snapshot execution.Order,
	now time.Time,
	kill KillPoint,
) error {
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	event := execution.OrderEvent{
		ID:                 fmt.Sprintf("%s-cancel-pending-%d", clientOrderID, snapshot.Revision+1),
		OrderID:            snapshot.Identity.ID,
		ClientOrderID:      snapshot.Identity.ClientOrderID,
		State:              execution.OrderCancelPending,
		ExchangeStatus:     "CANCEL_PENDING",
		CumulativeQuantity: snapshot.CumulativeQuantity,
		Fees:               append([]execution.FeeFact(nil), snapshot.Fees...),
		Fills:              append([]execution.FillFact(nil), snapshot.Fills...),
		OccurredAt:         now,
		Ordinal:            snapshot.Revision + 1,
	}
	_, err := reducer.Reduce(event)
	return err
}

// MarkCancelUnknown quarantines an ambiguous cancellation while retaining all
// reservation and open-order capacity.
func (repository *memoryDispatcherRepository) MarkCancelUnknown(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill KillPoint,
) error {
	repository.mutex.Lock()
	defer repository.mutex.Unlock()
	record, exists := repository.outbox[id]
	if !exists || record.State == OutboxTerminal || record.FencingToken != fence {
		return contractError("stale_fencing_token")
	}
	if err := kill.Hit(ctx, KillBeforeReducerUpdate); err != nil {
		return err
	}
	snapshot := repository.reducers[record.Submission.OrderID.String()].Snapshot()
	event := execution.OrderEvent{
		ID:                 fmt.Sprintf("%s-cancel-unknown-%d", record.Submission.ClientOrderID, snapshot.Revision+1),
		OrderID:            snapshot.Identity.ID,
		ClientOrderID:      snapshot.Identity.ClientOrderID,
		State:              execution.OrderUnknown,
		ExchangeStatus:     "CANCEL_AMBIGUOUS",
		CumulativeQuantity: snapshot.CumulativeQuantity,
		Fees:               append([]execution.FeeFact(nil), snapshot.Fees...),
		Fills:              append([]execution.FillFact(nil), snapshot.Fills...),
		OccurredAt:         now,
		Ordinal:            snapshot.Revision + 1,
	}
	if _, err := repository.reducers[record.Submission.OrderID.String()].Reduce(event); err != nil {
		return err
	}
	record.State, record.UpdatedAt = OutboxUnknown, now
	repository.updateMemoryPlanState(record.Submission.PlanID.String(), id, record)
	repository.outbox[id] = record
	return kill.Hit(ctx, KillAfterReducerUpdate)
}
