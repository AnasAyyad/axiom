package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// ActiveStrategySessionWork returns the currently runnable automatic strategy
// sessions attached to one exact account epoch. It is intentionally a
// read-only scheduling snapshot: callers must perform the full allocation,
// risk, arm, and dispatcher admission again immediately before creating an
// entry. This prevents a stale worker read from becoming an order authority.
func (store *V1CDispatcherStore) ActiveStrategySessionWork(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]sandbox.StrategySessionWork, error) {
	if account == "" || epoch == 0 || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 16 {
		return nil, fmt.Errorf("sandbox_strategy_session_work_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = validateEngineCommandLease(ctx, tx, account, owner, fence, now); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_fence_invalid")
	}
	rows, err := tx.Query(ctx, activeStrategySessionWorkSQL, account, int64(epoch), now, limit)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_query_failed")
	}
	defer rows.Close()
	items := make([]sandbox.StrategySessionWork, 0, limit)
	for rows.Next() {
		var item sandbox.StrategySessionWork
		var accountEpoch, sessionRevision, strategyRevision int64
		if err = rows.Scan(
			&item.SessionID, &item.Strategy, &item.Instrument,
			&item.ConfigurationID, &item.StrategySetHash,
			&accountEpoch, &item.Account.ID, &item.Account.Exchange,
			&sessionRevision, &strategyRevision, &item.StartedAt, &item.ArmExpiresAt,
		); err != nil || accountEpoch <= 0 || sessionRevision <= 0 || strategyRevision <= 0 {
			return nil, fmt.Errorf("sandbox_strategy_session_work_scan_failed")
		}
		item.Account.Epoch = uint64(accountEpoch)
		item.SessionRevision = uint64(sessionRevision)
		item.StrategyRevision = uint64(strategyRevision)
		if err = item.ValidAt(now); err != nil {
			return nil, fmt.Errorf("sandbox_strategy_session_work_invalid")
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_query_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("sandbox_strategy_session_work_commit_failed")
	}
	return items, nil
}

const activeStrategySessionWorkSQL = `
SELECT strategy.id,strategy.strategy_id,strategy.instrument,
       parent.configuration_id,parent.strategy_set_hash,
       membership.account_epoch,membership.account_id,membership.exchange,
       parent.revision,strategy.revision,strategy.started_at,arm.expires_at
FROM sandbox_strategy_sessions strategy
JOIN v1c_sandbox_sessions parent
  ON parent.id=strategy.sandbox_session_id
JOIN sandbox_strategy_session_accounts membership
  ON membership.strategy_session_id=strategy.id
JOIN v1c_sandbox_session_accounts parent_membership
  ON parent_membership.session_id=parent.id
 AND parent_membership.account_id=membership.account_id
 AND parent_membership.account_epoch=membership.account_epoch
JOIN v1c_exchange_accounts account
  ON account.id=membership.account_id
 AND account.current_epoch=membership.account_epoch
JOIN v1c_sandbox_arms arm
  ON arm.sandbox_session_id=parent.id
WHERE strategy.state='running'
  AND parent.state='ARMED'
  AND membership.account_id=$1
  AND membership.account_epoch=$2
  AND arm.revoked_at IS NULL
  AND arm.created_at <= $3
  AND arm.expires_at > $3
ORDER BY strategy.started_at,strategy.id
LIMIT $4`

var _ sandbox.StrategySessionWorkSource = (*V1CDispatcherStore)(nil)
