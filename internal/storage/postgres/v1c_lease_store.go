package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableOrderID(value domain.VirtualOrderID) any {
	if value.Value() == "" {
		return nil
	}
	return value.String()
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// AcquireAccountLease serializes one engine per account and monotonically
// advances the fencing token on takeover.
func (store *V1CDispatcherStore) AcquireAccountLease(
	ctx context.Context,
	account sandbox.AccountID,
	environment sandbox.Environment,
	owner string,
	now time.Time,
	ttl time.Duration,
	kill sandbox.KillPoint,
) (uint64, error) {
	if account == "" || owner == "" || now.IsZero() || now.Location() != time.UTC ||
		ttl <= 0 || ttl > 5*time.Minute {
		return 0, fmt.Errorf("v1c_lease_invalid")
	}
	if kill == nil {
		kill = sandbox.NoKillPoint{}
	}
	if err := kill.Hit(ctx, sandbox.KillBeforeLeaseTransition); err != nil {
		return 0, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return 0, fmt.Errorf("v1c_lease_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	next, err := writeV1CAccountLease(
		ctx, tx, account, environment, owner, now, ttl,
	)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("v1c_lease_commit_failed")
	}
	if err = kill.Hit(ctx, sandbox.KillAfterLeaseTransition); err != nil {
		return 0, err
	}
	return uint64(next), nil
}

func writeV1CAccountLease(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	environment sandbox.Environment,
	owner string,
	now time.Time,
	ttl time.Duration,
) (int64, error) {
	var accountEnvironment string
	if err := tx.QueryRow(ctx, `
SELECT environment FROM v1c_exchange_accounts WHERE id=$1 FOR UPDATE`,
		account).Scan(&accountEnvironment); err != nil ||
		accountEnvironment != string(environment) {
		return 0, fmt.Errorf("v1c_lease_environment_rejected")
	}
	priorOwner, priorToken, expires, err := readV1CLease(ctx, tx, account)
	if err != nil {
		return 0, err
	}
	if expires.After(now) && priorOwner != "" && priorOwner != owner {
		return 0, fmt.Errorf("v1c_lease_held")
	}
	next := priorToken + 1
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_account_leases(account_id,environment,owner,fencing_token,acquired_at,expires_at)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (account_id) DO UPDATE SET
 environment=EXCLUDED.environment,owner=EXCLUDED.owner,fencing_token=EXCLUDED.fencing_token,
 acquired_at=EXCLUDED.acquired_at,expires_at=EXCLUDED.expires_at`,
		account, environment, owner, next, now, now.Add(ttl)); err != nil {
		return 0, fmt.Errorf("v1c_lease_write_failed")
	}
	return next, nil
}

func readV1CLease(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
) (string, int64, time.Time, error) {
	var owner string
	var token int64
	var expires time.Time
	err := tx.QueryRow(ctx, `
SELECT owner,fencing_token,expires_at FROM v1c_account_leases
WHERE account_id=$1 FOR UPDATE`, account).Scan(&owner, &token, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, time.Time{}, nil
	}
	if err != nil {
		return "", 0, time.Time{}, fmt.Errorf("v1c_lease_read_failed")
	}
	return owner, token, expires, nil
}

var _ sandbox.DispatcherRepository = (*V1CDispatcherStore)(nil)
