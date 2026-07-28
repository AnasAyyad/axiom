package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

// MarkCancelPending persists the cancel intent under the current account lease
// before any exchange network attempt. It deliberately does not require an
// active entry arm or healthy public collector.
func (store *V1CDispatcherStore) MarkCancelPending(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID, owner string,
	fence uint64,
	now time.Time,
	kill sandbox.KillPoint,
) (string, error) {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return "", err
	}
	var outboxID string
	err := store.pool.QueryRow(ctx, markV1CCancelPendingSQL,
		account,
		epoch,
		clientOrderID,
		owner,
		fence,
		now,
	).Scan(&outboxID)
	if err != nil {
		return "", fmt.Errorf("v1c_cancel_pending_failed")
	}
	if err = kill.Hit(ctx, sandbox.KillAfterReducerUpdate); err != nil {
		return "", err
	}
	return outboxID, nil
}

// MarkCancelUnknown quarantines an ambiguous cancel without releasing either
// the reservation or open-order capacity.
func (store *V1CDispatcherStore) MarkCancelUnknown(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return err
	}
	tag, err := store.pool.Exec(ctx, `
UPDATE v1c_submission_outbox
SET state='UNKNOWN',order_state='UNKNOWN',updated_at=$3
WHERE id=$1
  AND state IN ('ACKNOWLEDGED','UNKNOWN')
  AND order_state='CANCEL_PENDING'
  AND fencing_token=$2`, id, fence, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("v1c_cancel_unknown_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReducerUpdate)
}
