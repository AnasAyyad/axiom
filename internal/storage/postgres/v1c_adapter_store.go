package postgres

import (
	"context"
	"fmt"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// SubmissionByClientOrderID resolves the exact immutable adapter request
// behind one account-epoch deterministic client ID.
func (store *V1CDispatcherStore) SubmissionByClientOrderID(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.Submission, bool, error) {
	var id string
	err := store.pool.QueryRow(ctx, `
SELECT id FROM v1c_submission_outbox
WHERE account_id=$1 AND account_epoch=$2 AND client_order_id=$3`,
		account,
		epoch,
		clientOrderID,
	).Scan(&id)
	if err == pgx.ErrNoRows {
		return sandbox.Submission{}, false, nil
	}
	if err != nil {
		return sandbox.Submission{}, false, fmt.Errorf("v1c_submission_lookup_failed")
	}
	record, err := readV1COutbox(ctx, store.pool, id)
	if err != nil {
		return sandbox.Submission{}, false, err
	}
	return record.Submission, true, nil
}

// ActiveSubmissions returns the globally bounded nonterminal account set used
// for private-stream reconnect backfill.
func (store *V1CDispatcherStore) ActiveSubmissions(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) ([]sandbox.Submission, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id FROM v1c_submission_outbox
WHERE account_id=$1 AND account_epoch=$2
  AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')
ORDER BY approved_at,id
LIMIT 3`,
		account,
		epoch,
	)
	if err != nil {
		return nil, fmt.Errorf("v1c_active_submission_query_failed")
	}
	defer rows.Close()
	ids := make([]string, 0, 3)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("v1c_active_submission_scan_failed")
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("v1c_active_submission_scan_failed")
	}
	result := make([]sandbox.Submission, 0, len(ids))
	for _, id := range ids {
		record, readErr := readV1COutbox(ctx, store.pool, id)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, record.Submission)
	}
	return result, nil
}

// SnapshotExpectation returns the newest immutable account-epoch expectation.
func (store *V1CDispatcherStore) SnapshotExpectation(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) (sandbox.SnapshotExpectation, bool, error) {
	var expectation sandbox.SnapshotExpectation
	err := store.pool.QueryRow(ctx, `
SELECT snapshot_hash,orders_hash,fills_hash
FROM v1c_account_snapshots
WHERE account_id=$1 AND account_epoch=$2
ORDER BY observed_at DESC,id DESC
LIMIT 1`,
		account,
		epoch,
	).Scan(
		&expectation.SnapshotHash,
		&expectation.OrdersHash,
		&expectation.FillsHash,
	)
	if err == pgx.ErrNoRows {
		return sandbox.SnapshotExpectation{}, false, nil
	}
	if err != nil {
		return sandbox.SnapshotExpectation{}, false, fmt.Errorf("v1c_snapshot_expectation_failed")
	}
	return expectation, true, nil
}

var (
	_ sandbox.SubmissionRecoveryReader  = (*V1CDispatcherStore)(nil)
	_ sandbox.SnapshotExpectationReader = (*V1CDispatcherStore)(nil)
)
