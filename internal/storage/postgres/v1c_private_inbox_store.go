package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// AppendPrivateEvent durably appends, deduplicates, and canonically reduces an event.
func (store *V1CDispatcherStore) AppendPrivateEvent(
	ctx context.Context,
	outboxID string,
	fence uint64,
	event sandbox.PrivateEvent,
	kill sandbox.KillPoint,
) error {
	if err := event.Validate(); err != nil {
		return err
	}
	if err := kill.Hit(ctx, sandbox.KillBeforeInboxAppend); err != nil {
		return err
	}
	payload, err := json.Marshal(event.OrderEvent)
	if err != nil {
		return fmt.Errorf("v1c_private_event_encode_failed")
	}
	alreadyReduced, err := store.persistV1CPrivateInbox(ctx, event, payload, kill)
	if err != nil || alreadyReduced {
		return err
	}
	return store.reduceV1CPrivateInbox(ctx, outboxID, fence, event, payload, kill)
}

func (store *V1CDispatcherStore) persistV1CPrivateInbox(
	ctx context.Context,
	event sandbox.PrivateEvent,
	payload []byte,
	kill sandbox.KillPoint,
) (bool, error) {
	inboxTx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("v1c_private_event_begin_failed")
	}
	defer func() { _ = inboxTx.Rollback(context.Background()) }()
	tag, err := inboxTx.Exec(ctx, `
INSERT INTO v1c_private_inbox(
  id,account_id,account_epoch,event_identity,event_kind,order_id,client_order_id,
  native_order_hash,native_fill_hash,balance_hash,canonical_event,occurred_at,received_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (account_id,account_epoch,event_identity) DO NOTHING`,
		event.Identity, event.AccountID, event.AccountEpoch, event.Identity, event.Kind,
		nullableOrderID(event.OrderID), nullableText(event.ClientOrderID),
		event.NativeOrderHash, nullableText(event.NativeFillHash), nullableText(event.BalanceHash),
		payload, event.OccurredAt, event.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("v1c_private_event_insert_failed")
	}
	alreadyReduced := false
	if tag.RowsAffected() == 0 {
		alreadyReduced, err = verifyV1CDuplicateInbox(ctx, inboxTx, event, payload)
		if err != nil {
			return false, err
		}
	}
	if err = kill.Hit(ctx, sandbox.KillAfterInboxAppend); err != nil {
		return false, err
	}
	if err = kill.Hit(ctx, sandbox.KillBeforeInboxCommit); err != nil {
		return false, err
	}
	if err = inboxTx.Commit(ctx); err != nil {
		return false, fmt.Errorf("v1c_private_event_inbox_commit_failed")
	}
	if err = kill.Hit(ctx, sandbox.KillAfterInboxCommit); err != nil {
		return false, err
	}
	return alreadyReduced, nil
}

func verifyV1CDuplicateInbox(
	ctx context.Context,
	tx pgx.Tx,
	event sandbox.PrivateEvent,
	payload []byte,
) (bool, error) {
	expectedOrderID := ""
	if event.OrderID.Value() != "" {
		expectedOrderID = event.OrderID.String()
	}
	var orderHash string
	var fillHash, balanceHash, orderID, clientOrderID *string
	var kind string
	var payloadMatches, alreadyReduced bool
	var occurredAt, receivedAt time.Time
	err := tx.QueryRow(ctx, `
SELECT native_order_hash,native_fill_hash,balance_hash,event_kind,
       canonical_event=$4::jsonb,order_id,client_order_id,occurred_at,received_at,
       reduced_at IS NOT NULL
FROM v1c_private_inbox
WHERE account_id=$1 AND account_epoch=$2 AND event_identity=$3`,
		event.AccountID, event.AccountEpoch, event.Identity, string(payload),
	).Scan(
		&orderHash, &fillHash, &balanceHash, &kind, &payloadMatches,
		&orderID, &clientOrderID, &occurredAt, &receivedAt, &alreadyReduced,
	)
	mismatch := err != nil || orderHash != event.NativeOrderHash ||
		valueOrEmpty(fillHash) != event.NativeFillHash ||
		valueOrEmpty(balanceHash) != event.BalanceHash || kind != string(event.Kind) ||
		!payloadMatches || valueOrEmpty(orderID) != expectedOrderID ||
		valueOrEmpty(clientOrderID) != event.ClientOrderID ||
		!occurredAt.Equal(event.OccurredAt) || !receivedAt.Equal(event.ReceivedAt)
	if mismatch {
		return false, fmt.Errorf("v1c_private_event_identity_conflict")
	}
	return alreadyReduced, nil
}

func (store *V1CDispatcherStore) reduceV1CPrivateInbox(
	ctx context.Context,
	outboxID string,
	fence uint64,
	event sandbox.PrivateEvent,
	payload []byte,
	kill sandbox.KillPoint,
) error {
	reductionTx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_private_event_reduce_begin_failed")
	}
	defer func() { _ = reductionTx.Rollback(context.Background()) }()
	var reducedAt *time.Time
	if err = reductionTx.QueryRow(ctx, `
SELECT reduced_at FROM v1c_private_inbox WHERE id=$1 FOR UPDATE`,
		event.Identity).Scan(&reducedAt); err != nil {
		return fmt.Errorf("v1c_private_event_missing")
	}
	if reducedAt != nil {
		return reductionTx.Commit(ctx)
	}
	if event.OrderEvent != nil {
		if err = reduceV1COrderEvent(
			ctx, reductionTx, outboxID, fence, event, payload, kill,
		); err != nil {
			return err
		}
	}
	if _, err = reductionTx.Exec(ctx, `
UPDATE v1c_private_inbox SET reduced_at=$2 WHERE id=$1`, event.Identity, event.ReceivedAt); err != nil {
		return fmt.Errorf("v1c_private_event_reduce_mark_failed")
	}
	if err = kill.Hit(ctx, sandbox.KillBeforeReductionCommit); err != nil {
		return err
	}
	if err = reductionTx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_private_event_commit_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReductionCommit)
}

func reduceV1COrderEvent(
	ctx context.Context,
	tx pgx.Tx,
	outboxID string,
	fence uint64,
	event sandbox.PrivateEvent,
	payload []byte,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return err
	}
	lockedID, record, err := lockV1CPrivateOutbox(ctx, tx, outboxID, fence, event)
	if err != nil {
		return err
	}
	orderEvents, err := readV1COrderHistory(ctx, tx, event)
	if err != nil {
		return err
	}
	order, err := sandbox.ReducePrivateOrderEvents(record.Submission, orderEvents)
	if err != nil {
		return fmt.Errorf("v1c_private_event_reduce_failed")
	}
	state, terminal := v1cOrderState(order.State)
	if err = advanceV1COutbox(ctx, tx, lockedID, fence, state, terminal, event.ReceivedAt); err != nil {
		return err
	}
	if err = kill.Hit(ctx, sandbox.KillAfterReducerUpdate); err != nil {
		return err
	}
	if event.Kind == sandbox.PrivateFillEvent {
		if err = postV1CFill(ctx, tx, event, payload, kill); err != nil {
			return err
		}
	}
	if terminal {
		if err = closeV1CReservation(
			ctx, tx, event, state, len(order.Fills) > 0, kill,
		); err != nil {
			return err
		}
	}
	return updateV1CPlanState(ctx, tx, record.Submission.PlanID.String())
}

func lockV1CPrivateOutbox(
	ctx context.Context,
	tx pgx.Tx,
	outboxID string,
	fence uint64,
	event sandbox.PrivateEvent,
) (string, sandbox.SubmissionOutbox, error) {
	if outboxID == "" {
		if err := tx.QueryRow(ctx, `
SELECT id FROM v1c_submission_outbox
WHERE account_id=$1 AND account_epoch=$2 AND order_id=$3 FOR UPDATE`,
			event.AccountID, event.AccountEpoch, event.OrderID.String()).Scan(&outboxID); err != nil {
			return "", sandbox.SubmissionOutbox{}, fmt.Errorf("v1c_private_event_order_missing")
		}
	} else {
		var lockedID string
		if err := tx.QueryRow(ctx, `
SELECT id FROM v1c_submission_outbox
WHERE id=$1 AND account_id=$2 AND account_epoch=$3 AND order_id=$4 FOR UPDATE`,
			outboxID, event.AccountID, event.AccountEpoch, event.OrderID.String(),
		).Scan(&lockedID); err != nil {
			return "", sandbox.SubmissionOutbox{}, fmt.Errorf("v1c_private_event_order_missing")
		}
	}
	var leaseValid bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(SELECT 1 FROM v1c_account_leases
WHERE account_id=$1 AND fencing_token=$2 AND expires_at>$3)`,
		event.AccountID, fence, event.ReceivedAt).Scan(&leaseValid); err != nil || !leaseValid {
		return "", sandbox.SubmissionOutbox{}, fmt.Errorf("v1c_stale_fencing_token")
	}
	record, err := readV1COutbox(ctx, tx, outboxID)
	return outboxID, record, err
}

func readV1COrderHistory(
	ctx context.Context,
	tx pgx.Tx,
	event sandbox.PrivateEvent,
) ([]execution.OrderEvent, error) {
	rows, err := tx.Query(ctx, `
SELECT canonical_event FROM v1c_private_inbox
WHERE account_id=$1 AND account_epoch=$2 AND order_id=$3
  AND canonical_event IS NOT NULL AND (reduced_at IS NOT NULL OR id=$4)
ORDER BY occurred_at,received_at,id`,
		event.AccountID, event.AccountEpoch, event.OrderID.String(), event.Identity)
	if err != nil {
		return nil, fmt.Errorf("v1c_private_event_history_failed")
	}
	defer rows.Close()
	orderEvents := make([]execution.OrderEvent, 0)
	for rows.Next() {
		var encoded []byte
		var orderEvent execution.OrderEvent
		if rows.Scan(&encoded) != nil || json.Unmarshal(encoded, &orderEvent) != nil {
			return nil, fmt.Errorf("v1c_private_event_history_invalid")
		}
		orderEvents = append(orderEvents, orderEvent)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("v1c_private_event_history_failed")
	}
	return orderEvents, nil
}
