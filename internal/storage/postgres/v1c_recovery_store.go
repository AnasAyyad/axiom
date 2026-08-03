package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// ListUnknown returns a bounded account-epoch page only while the caller owns
// the exact active fencing lease.
func (store *V1CDispatcherStore) ListUnknown(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]sandbox.SubmissionOutbox, error) {
	if account == "" || epoch == 0 || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 32 {
		return nil, fmt.Errorf("v1c_unknown_list_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, fmt.Errorf("v1c_unknown_list_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = requireV1CRecoveryLease(ctx, tx, account, owner, fence, now); err != nil {
		return nil, err
	}
	ids, err := listV1CUnknownIDs(ctx, tx, account, epoch, limit)
	if err != nil {
		return nil, err
	}
	result, err := readV1CUnknownRecords(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("v1c_unknown_list_commit_failed")
	}
	return result, nil
}

func requireV1CRecoveryLease(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	owner string,
	fence uint64,
	now time.Time,
) error {
	var leaseOK bool
	err := tx.QueryRow(ctx, `
SELECT owner=$2 AND fencing_token=$3 AND expires_at>$4
FROM v1c_account_leases WHERE account_id=$1 FOR SHARE`,
		account, owner, fence, now).Scan(&leaseOK)
	if err != nil || !leaseOK {
		return fmt.Errorf("v1c_stale_fencing_token")
	}
	return nil
}

func listV1CUnknownIDs(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	epoch uint64,
	limit int,
) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT id FROM v1c_submission_outbox
WHERE account_id=$1 AND account_epoch=$2 AND state='UNKNOWN'
ORDER BY updated_at,id LIMIT $3`, account, epoch, limit)
	if err != nil {
		return nil, fmt.Errorf("v1c_unknown_list_failed")
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("v1c_unknown_list_failed")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, fmt.Errorf("v1c_unknown_list_failed")
	}
	return ids, nil
}

func readV1CUnknownRecords(
	ctx context.Context,
	tx pgx.Tx,
	ids []string,
) ([]sandbox.SubmissionOutbox, error) {
	result := make([]sandbox.SubmissionOutbox, 0, len(ids))
	for _, id := range ids {
		record, readErr := readV1COutbox(ctx, tx, id)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, record)
	}
	return result, nil
}

// RecordReconciliation durably records the authoritative result. Any critical
// difference atomically locks the account, revokes arms, degrades attached
// sessions, and quarantines unresolved order and reservation capacity.
func (store *V1CDispatcherStore) RecordReconciliation(
	ctx context.Context,
	result sandbox.ReconciliationResult,
) error {
	if err := result.Validate(); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_reconciliation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = insertV1CReconciliation(ctx, tx, result); err != nil {
		return err
	}
	if result.State == "quarantined" {
		if err = quarantineV1CReconciliation(ctx, tx, result); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_reconciliation_commit_failed")
	}
	return nil
}

// ResolveReconciledTerminal releases a zero-fill cancel/reject/expiry only
// after a clean persisted reconciliation for the exact account epoch. Until
// this transaction commits, the UNKNOWN outbox and reservation retain capacity.
func (store *V1CDispatcherStore) ResolveReconciledTerminal(
	ctx context.Context,
	outboxID string,
	fence uint64,
	reconciliationID string,
	now time.Time,
	kill sandbox.KillPoint,
) (bool, error) {
	if outboxID == "" || fence == 0 || reconciliationID == "" ||
		now.IsZero() || now.Location() != time.UTC {
		return false, fmt.Errorf("v1c_reconciled_terminal_invalid")
	}
	if kill == nil {
		kill = sandbox.NoKillPoint{}
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return false, fmt.Errorf("v1c_reconciled_terminal_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	record, err := loadV1CReconciledTerminal(
		ctx, tx, outboxID, fence, reconciliationID, now,
	)
	if err != nil {
		return false, err
	}
	resolved, err := resolveV1CReconciledTerminal(
		ctx, tx, outboxID, record, now, kill,
	)
	if err != nil || !resolved {
		return resolved, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("v1c_reconciled_terminal_commit_failed")
	}
	return true, nil
}

func resolveV1CReconciledTerminal(
	ctx context.Context,
	tx pgx.Tx,
	outboxID string,
	record v1CReconciledTerminal,
	now time.Time,
	kill sandbox.KillPoint,
) (bool, error) {
	if record.outboxState == "TERMINAL" {
		return false, nil
	}
	if record.outboxState != "UNKNOWN" {
		return false, fmt.Errorf("v1c_reconciled_terminal_rejected")
	}
	resolvedOrderState := reconciledV1CTerminalState(
		record.orderState,
		record.createAttempted,
	)
	if record.hasFill || resolvedOrderState == "" {
		return false, nil
	}
	if err := releaseV1CReconciledReservation(
		ctx, tx, record.orderID, resolvedOrderState, now, kill,
	); err != nil {
		return false, err
	}
	if err := finalizeV1CReconciledTerminal(
		ctx, tx, outboxID, record.planID, resolvedOrderState, now, kill,
	); err != nil {
		return false, err
	}
	return true, nil
}

type v1CReconciledTerminal struct {
	planID              string
	orderID             string
	orderState          string
	outboxState         string
	leaseValid          bool
	reconciliationClean bool
	hasFill             bool
	createAttempted     bool
}

const loadV1CReconciledTerminalSQL = `
SELECT outbox.plan_id,outbox.order_id,outbox.order_state,outbox.state,
       EXISTS(
         SELECT 1 FROM v1c_account_leases lease
         WHERE lease.account_id=outbox.account_id
           AND lease.fencing_token=$2 AND lease.expires_at>$4
       ),
       EXISTS(
         SELECT 1 FROM v1c_reconciliations reconciliation
         WHERE reconciliation.id=$3
           AND reconciliation.account_id=outbox.account_id
           AND reconciliation.account_epoch=outbox.account_epoch
           AND reconciliation.state='clean'
           AND reconciliation.reconciled_at>=outbox.updated_at
       ),
       EXISTS(
         SELECT 1 FROM v1c_exchange_fills fill
         WHERE fill.account_id=outbox.account_id
           AND fill.account_epoch=outbox.account_epoch
           AND fill.order_id=outbox.order_id
       ),
       EXISTS(
         SELECT 1 FROM v1c_authenticated_request_evidence evidence
         WHERE evidence.exchange=account.exchange
           AND evidence.method='POST'
           AND evidence.path=CASE account.exchange
             WHEN 'binance' THEN '/api/v3/order'
             WHEN 'bybit' THEN '/v5/order/create'
           END
           AND evidence.configuration_id=plan.configuration_id
           AND evidence.recorded_at>=plan.approved_at
           AND evidence.recorded_at<=$4
       )
FROM v1c_submission_outbox outbox
JOIN v1c_submission_plans plan ON plan.id=outbox.plan_id
JOIN v1c_exchange_accounts account ON account.id=outbox.account_id
WHERE outbox.id=$1
FOR UPDATE OF outbox`

func loadV1CReconciledTerminal(
	ctx context.Context,
	tx pgx.Tx,
	outboxID string,
	fence uint64,
	reconciliationID string,
	now time.Time,
) (v1CReconciledTerminal, error) {
	var record v1CReconciledTerminal
	err := tx.QueryRow(ctx, loadV1CReconciledTerminalSQL,
		outboxID,
		fence,
		reconciliationID,
		now,
	).Scan(
		&record.planID,
		&record.orderID,
		&record.orderState,
		&record.outboxState,
		&record.leaseValid,
		&record.reconciliationClean,
		&record.hasFill,
		&record.createAttempted,
	)
	if err != nil || !record.leaseValid || !record.reconciliationClean {
		return v1CReconciledTerminal{}, fmt.Errorf("v1c_reconciled_terminal_rejected")
	}
	return record, nil
}

func releasableV1CTerminalState(state string) bool {
	return state == "CANCELED" || state == "REJECTED" || state == "EXPIRED"
}

func reconciledV1CTerminalState(
	orderState string,
	createAttempted bool,
) string {
	if orderState == "UNKNOWN" && !createAttempted {
		// Authenticated request evidence is committed before network I/O. Its
		// absence proves that this locally claimed attempt never reached an
		// exchange create route, so clean reconciliation may close it as a
		// deterministic rejection rather than query it forever.
		return "REJECTED"
	}
	if releasableV1CTerminalState(orderState) {
		return orderState
	}
	return ""
}

func releaseV1CReconciledReservation(
	ctx context.Context,
	tx pgx.Tx,
	orderID, orderState string,
	now time.Time,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReservationRelease); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_reservations
SET state='RELEASED',released_at=$2,release_reason=$3
WHERE order_id=$1 AND state='ACTIVE'`,
		orderID,
		now,
		orderState,
	); err != nil {
		return fmt.Errorf("v1c_reconciled_terminal_reservation_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReservationRelease)
}

func finalizeV1CReconciledTerminal(
	ctx context.Context,
	tx pgx.Tx,
	outboxID, planID, orderState string,
	now time.Time,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE v1c_submission_outbox
SET state='TERMINAL',order_state=$3,updated_at=$2
WHERE id=$1 AND state='UNKNOWN'`,
		outboxID, now, orderState,
	); err != nil {
		return fmt.Errorf("v1c_reconciled_terminal_outbox_failed")
	}
	if err := updateV1CPlanState(ctx, tx, planID); err != nil {
		return err
	}
	return kill.Hit(ctx, sandbox.KillAfterReducerUpdate)
}

var _ sandbox.UnknownRecoveryRepository = (*V1CDispatcherStore)(nil)
