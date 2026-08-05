package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"
)

const blockExpiredSandboxStrategySessionsSQL = `
UPDATE sandbox_strategy_sessions strategy
SET state='blocked',blocking_reason='arm_expired_or_revoked',revision=strategy.revision+1
FROM v1c_sandbox_sessions parent
WHERE strategy.sandbox_session_id=parent.id
  AND strategy.state='running'
  AND EXISTS (
    SELECT 1
    FROM sandbox_strategy_session_accounts membership
    WHERE membership.strategy_session_id=strategy.id
      AND membership.account_id=$1
      AND membership.account_epoch=$2
  )
  AND NOT EXISTS (
    SELECT 1
    FROM v1c_sandbox_arms arm
    WHERE arm.sandbox_session_id=parent.id
      AND arm.revoked_at IS NULL
      AND arm.created_at <= $3
      AND arm.expires_at > $3
  )`

// BlockExpiredStrategySessions prevents new automatic entries as soon as an
// account's exact arm is no longer active. It intentionally does not alter
// the parent session, dispatcher, or existing orders: cancellation,
// reconciliation, and risk-reducing recovery must remain available.
func (store *V1CDispatcherStore) BlockExpiredStrategySessions(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	now time.Time,
) (int, error) {
	if account == "" || epoch == 0 || now.IsZero() || now.Location() != time.UTC {
		return 0, fmt.Errorf("sandbox_strategy_session_expiry_invalid")
	}
	tag, err := store.pool.Exec(ctx, blockExpiredSandboxStrategySessionsSQL,
		account, epoch, now)
	if err != nil {
		return 0, fmt.Errorf("sandbox_strategy_session_expiry_write_failed")
	}
	return int(tag.RowsAffected()), nil
}
