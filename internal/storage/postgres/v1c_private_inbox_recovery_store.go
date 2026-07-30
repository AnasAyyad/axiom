package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

const maximumV1CInboxRecoveryPage = 1000

// RecoverPrivateInbox completes reducer work that was durably appended before
// a process stopped. It never asks an exchange to repeat an order operation.
func (store *V1CDispatcherStore) RecoverPrivateInbox(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	fence uint64,
) (int, error) {
	if account == "" || epoch == 0 || fence == 0 {
		return 0, fmt.Errorf("v1c_private_inbox_recovery_invalid")
	}
	records, err := store.readPrivateInboxRecovery(ctx, account, epoch)
	if err != nil {
		return 0, err
	}
	for index, record := range records {
		if err = store.reduceV1CPrivateInbox(
			ctx, "", fence, record.event, record.payload,
			sandbox.NoKillPoint{},
		); err != nil {
			return index, err
		}
	}
	return len(records), nil
}

func (store *V1CDispatcherStore) readPrivateInboxRecovery(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) ([]v1CInboxRecoveryRecord, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,event_kind,order_id,client_order_id,native_order_hash,
       native_fill_hash,balance_hash,canonical_event::text,
       occurred_at,received_at
FROM v1c_private_inbox
WHERE account_id=$1 AND account_epoch=$2 AND reduced_at IS NULL
ORDER BY occurred_at,received_at,id
LIMIT $3`,
		account,
		epoch,
		maximumV1CInboxRecoveryPage+1,
	)
	if err != nil {
		return nil, fmt.Errorf("v1c_private_inbox_recovery_query_failed")
	}
	records := make([]v1CInboxRecoveryRecord, 0)
	for rows.Next() {
		record, scanErr := scanV1CInboxRecovery(rows, account, epoch)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		records = append(records, record)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return nil, fmt.Errorf("v1c_private_inbox_recovery_query_failed")
	}
	if len(records) > maximumV1CInboxRecoveryPage {
		return nil, fmt.Errorf("v1c_private_inbox_recovery_overflow")
	}
	return records, nil
}

type v1CInboxRecoveryScanner interface {
	Scan(...any) error
}

type v1CInboxRecoveryRecord struct {
	event   sandbox.PrivateEvent
	payload []byte
}

func scanV1CInboxRecovery(
	row v1CInboxRecoveryScanner,
	account sandbox.AccountID,
	epoch uint64,
) (v1CInboxRecoveryRecord, error) {
	var identity, kind, nativeOrderHash, payload string
	var orderID, clientOrderID, nativeFillHash, balanceHash *string
	var occurredAt, receivedAt time.Time
	if err := row.Scan(
		&identity,
		&kind,
		&orderID,
		&clientOrderID,
		&nativeOrderHash,
		&nativeFillHash,
		&balanceHash,
		&payload,
		&occurredAt,
		&receivedAt,
	); err != nil {
		return v1CInboxRecoveryRecord{},
			fmt.Errorf("v1c_private_inbox_recovery_scan_failed")
	}
	event := sandbox.PrivateEvent{
		Identity:        identity,
		AccountID:       account,
		AccountEpoch:    epoch,
		Kind:            sandbox.PrivateEventKind(kind),
		ClientOrderID:   valueOrEmpty(clientOrderID),
		NativeOrderHash: nativeOrderHash,
		NativeFillHash:  valueOrEmpty(nativeFillHash),
		BalanceHash:     valueOrEmpty(balanceHash),
		OccurredAt:      occurredAt.UTC(),
		ReceivedAt:      receivedAt.UTC(),
	}
	if err := decodeRecoveredPrivateEvent(&event, payload, orderID); err != nil {
		return v1CInboxRecoveryRecord{}, err
	}
	return v1CInboxRecoveryRecord{
		event: event, payload: []byte(payload),
	}, nil
}

func decodeRecoveredPrivateEvent(
	event *sandbox.PrivateEvent,
	payload string,
	orderID *string,
) error {
	if orderID != nil {
		if err := event.OrderID.UnmarshalText([]byte(*orderID)); err != nil {
			return fmt.Errorf("v1c_private_inbox_recovery_invalid")
		}
	}
	if event.Kind != sandbox.PrivateBalanceEvent {
		var orderEvent execution.OrderEvent
		if json.Unmarshal([]byte(payload), &orderEvent) != nil {
			return fmt.Errorf("v1c_private_inbox_recovery_invalid")
		}
		event.OrderEvent = &orderEvent
	}
	if event.Validate() != nil {
		return fmt.Errorf("v1c_private_inbox_recovery_invalid")
	}
	return nil
}
