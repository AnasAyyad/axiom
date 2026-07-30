package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// StopCanarySession revokes the bounded arm and closes the canary session. A
// failed prepare also locks the account; successful post-restart verification
// leaves the engine's READY_PAUSED state unchanged.
func (store *V1CDispatcherStore) StopCanarySession(
	ctx context.Context,
	session sandbox.SessionID,
	account sandbox.AccountID,
	lockAccount bool,
	now time.Time,
) error {
	if session == "" || account == "" || now.IsZero() ||
		now.Location() != time.UTC {
		return fmt.Errorf("v1c_canary_stop_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_canary_stop_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = revokeCanaryArm(ctx, tx, session, account, now); err != nil {
		return err
	}
	if err = markCanarySessionStopped(ctx, tx, session, now); err != nil {
		return err
	}
	if lockAccount {
		if err = lockFailedCanaryAccount(ctx, tx, account, now); err != nil {
			return err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_canary_stop_commit_failed")
	}
	return nil
}

func revokeCanaryArm(
	ctx context.Context,
	tx pgx.Tx,
	session sandbox.SessionID,
	account sandbox.AccountID,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_arms
SET revoked_at=coalesce(revoked_at,$3),revision=revision+1
WHERE sandbox_session_id=$1
  AND EXISTS(
    SELECT 1 FROM v1c_sandbox_session_accounts membership
    WHERE membership.session_id=$1 AND membership.account_id=$2
  )
  AND revoked_at IS NULL`,
		session, account, now,
	); err != nil {
		return fmt.Errorf("v1c_canary_arm_revoke_failed")
	}
	return nil
}

func markCanarySessionStopped(
	ctx context.Context,
	tx pgx.Tx,
	session sandbox.SessionID,
	now time.Time,
) error {
	tag, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_sessions
SET state='STOPPED',revision=revision+1,updated_at=$2
WHERE id=$1 AND state<>'STOPPED'`,
		session, now,
	)
	if err != nil || tag.RowsAffected() > 1 {
		return fmt.Errorf("v1c_canary_session_stop_failed")
	}
	return nil
}

func lockFailedCanaryAccount(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='LOCKED',revision=revision+1,updated_at=$2
WHERE id=$1 AND state='ARMED'`,
		account, now,
	); err != nil {
		return fmt.Errorf("v1c_canary_account_lock_failed")
	}
	return nil
}
