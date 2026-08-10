package postgres

import (
	"context"
	"fmt"

	"axiom/internal/sandbox"
)

// VerifyEngineRecoveryState proves the current epoch is still LOCKED and every
// capacity-bearing outbox leg has exactly one capacity-bearing reservation.
func (store *SandboxRuntimeDispatcherStore) VerifyEngineRecoveryState(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) error {
	if account == "" || epoch == 0 {
		return fmt.Errorf("sandbox_runtime_engine_recovery_state_invalid")
	}
	if err := store.verifyLockedEngineEpoch(ctx, account, epoch); err != nil {
		return err
	}
	if err := store.verifyEngineRecoveryCapacity(ctx, account, epoch); err != nil {
		return err
	}
	return store.verifyEngineRecoveryReservations(ctx, account, epoch)
}

func (store *SandboxRuntimeDispatcherStore) verifyLockedEngineEpoch(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) error {
	var state string
	var currentEpoch int64
	if err := store.pool.QueryRow(ctx, `
SELECT state,current_epoch
FROM sandbox_runtime_exchange_accounts
WHERE id=$1`,
		account,
	).Scan(&state, &currentEpoch); err != nil ||
		state != string(sandbox.EngineLocked) ||
		currentEpoch <= 0 ||
		uint64(currentEpoch) != epoch {
		return fmt.Errorf("sandbox_runtime_engine_recovery_state_invalid")
	}
	return nil
}

func (store *SandboxRuntimeDispatcherStore) verifyEngineRecoveryCapacity(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) error {
	var accountActive, globalActive int
	if err := store.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE account_id=$1 AND account_epoch=$2),
  count(*)
FROM sandbox_runtime_submission_outbox
WHERE state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
		account,
		epoch,
	).Scan(&accountActive, &globalActive); err != nil ||
		accountActive > 1 || globalActive > 2 {
		return fmt.Errorf("sandbox_runtime_engine_recovery_capacity_invalid")
	}
	return nil
}

func (store *SandboxRuntimeDispatcherStore) verifyEngineRecoveryReservations(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) error {
	var mismatch bool
	if err := store.pool.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM sandbox_runtime_submission_outbox outbox
  LEFT JOIN sandbox_runtime_sandbox_reservations reservation
    ON reservation.plan_id=outbox.plan_id
   AND reservation.order_id=outbox.order_id
   AND reservation.account_id=outbox.account_id
   AND reservation.account_epoch=outbox.account_epoch
  WHERE outbox.account_id=$1
    AND outbox.account_epoch=$2
    AND outbox.state IN ('WAITING','PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')
    AND (
      reservation.id IS NULL OR
      (outbox.state='WAITING' AND
        reservation.state NOT IN ('WAITING','QUARANTINED')) OR
      (outbox.state<>'WAITING' AND
        reservation.state NOT IN ('ACTIVE','QUARANTINED'))
    )
  UNION ALL
  SELECT 1
  FROM sandbox_runtime_sandbox_reservations reservation
  LEFT JOIN sandbox_runtime_submission_outbox outbox
    ON outbox.plan_id=reservation.plan_id
   AND outbox.order_id=reservation.order_id
   AND outbox.account_id=reservation.account_id
   AND outbox.account_epoch=reservation.account_epoch
   AND outbox.state IN ('WAITING','PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')
  WHERE reservation.account_id=$1
    AND reservation.account_epoch=$2
    AND reservation.state IN ('WAITING','ACTIVE','QUARANTINED')
    AND outbox.id IS NULL
)`,
		account,
		epoch,
	).Scan(&mismatch); err != nil || mismatch {
		return fmt.Errorf("sandbox_runtime_engine_recovery_reservation_invalid")
	}
	return nil
}
