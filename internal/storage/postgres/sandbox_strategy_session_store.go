package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// CreateStrategySession atomically creates the armable parent session and its
// automatic strategy child. It resolves current account epochs itself so an
// API caller cannot attach an arbitrary, stale, or already-active account.
// The method does not arm an account, submit an order, or contact an exchange.
func (store *V1CDispatcherStore) CreateStrategySession(
	ctx context.Context,
	command sandbox.StrategySessionCommand,
) (sandbox.StrategySession, error) {
	if err := command.Validate(); err != nil {
		return sandbox.StrategySession{}, fmt.Errorf("sandbox_strategy_session_invalid")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.StrategySession{}, fmt.Errorf("sandbox_strategy_session_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	session, err := selectStrategySessionAccounts(ctx, tx, command)
	if err != nil {
		return sandbox.StrategySession{}, err
	}
	if err = insertStrategySession(ctx, tx, command, session); err != nil {
		return sandbox.StrategySession{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.StrategySession{}, fmt.Errorf("sandbox_strategy_session_commit_failed")
	}
	return session, nil
}

func selectStrategySessionAccounts(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.StrategySessionCommand,
) (sandbox.StrategySession, error) {
	exchanges := append([]sandbox.Exchange(nil), command.Exchanges...)
	sort.Slice(exchanges, func(left, right int) bool { return exchanges[left] < exchanges[right] })
	accounts := make([]sandbox.StrategySessionAccount, 0, len(exchanges))
	for _, exchange := range exchanges {
		account, err := selectStrategySessionAccount(ctx, tx, exchange, command.Instrument, command.CreatedAt)
		if err != nil {
			return sandbox.StrategySession{}, err
		}
		accounts = append(accounts, account)
	}
	return sandbox.StrategySession{
		ID: command.ID, Strategy: command.Strategy, Accounts: accounts,
		State: sandbox.StrategySessionPrepared, CreatedAt: command.CreatedAt,
	}, nil
}

func selectStrategySessionAccount(
	ctx context.Context,
	tx pgx.Tx,
	exchange sandbox.Exchange,
	instrument string,
	createdAt time.Time,
) (sandbox.StrategySessionAccount, error) {
	var account sandbox.StrategySessionAccount
	var epoch, cycle int64
	var observedAt time.Time
	err := tx.QueryRow(ctx, strategySessionAccountSelectionSQL, exchange, instrument, createdAt).
		Scan(&account.ID, &epoch, &cycle, &observedAt)
	if err != nil || epoch <= 0 || cycle <= 0 || observedAt.After(createdAt) ||
		createdAt.Sub(observedAt) > 250*time.Millisecond {
		return sandbox.StrategySessionAccount{}, fmt.Errorf("sandbox_strategy_session_account_not_ready")
	}
	account.Epoch, account.Exchange = uint64(epoch), exchange
	return account, nil
}

const strategySessionAccountSelectionSQL = `
SELECT account.id,account.current_epoch,observation.startup_cycle,
       observation.observed_at
FROM v1c_exchange_accounts account
JOIN v1c_engine_observations observation
  ON observation.account_id=account.id
 AND observation.account_epoch=account.current_epoch
JOIN v1c_account_leases lease
  ON lease.account_id=account.id
 AND lease.fencing_token=observation.startup_cycle
WHERE account.exchange=$1
  AND ((account.exchange='binance' AND account.environment='spot_testnet')
       OR (account.exchange='bybit' AND account.environment='demo'))
  AND account.state='READY_PAUSED'
  AND observation.exchange=$1
  AND observation.private_stream_healthy
  AND observation.reconciliation_clean
  AND observation.evidence_healthy
  AND observation.eligibility->>'instrument'=$2
  AND (observation.eligibility->>'eligible')::boolean
  AND lease.expires_at>$3
  AND NOT EXISTS(
    SELECT 1
    FROM v1c_sandbox_session_accounts membership
    JOIN v1c_sandbox_sessions session ON session.id=membership.session_id
    WHERE membership.account_id=account.id
      AND membership.account_epoch=account.current_epoch
      AND session.state IN ('READY_PAUSED','ARMED','PAUSED')
  )
ORDER BY account.id
FOR UPDATE OF account
LIMIT 1`

func insertStrategySession(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.StrategySessionCommand,
	session sandbox.StrategySession,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,
 created_at,updated_at
) VALUES ($1,'READY_PAUSED',$2,$3,1,$4,$5,$5)`,
		command.ID, command.ConfigurationID, command.StrategySetHash,
		command.CreatedBy, command.CreatedAt); err != nil {
		return fmt.Errorf("sandbox_strategy_session_parent_insert_failed")
	}
	for _, account := range session.Accounts {
		if _, err := tx.Exec(ctx, `
INSERT INTO v1c_sandbox_session_accounts(session_id,account_id,account_epoch)
VALUES ($1,$2,$3)`, command.ID, account.ID, account.Epoch); err != nil {
			return fmt.Errorf("sandbox_strategy_session_membership_insert_failed")
		}
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_strategy_sessions(
 id,sandbox_session_id,strategy_id,state,created_by,created_at,revision
) VALUES ($1,$1,$2,'prepared',$3,$4,1)`,
		command.ID, command.Strategy, command.CreatedBy, command.CreatedAt); err != nil {
		return fmt.Errorf("sandbox_strategy_session_insert_failed")
	}
	for _, account := range session.Accounts {
		if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_strategy_session_accounts(
 strategy_session_id,account_id,account_epoch,exchange
) VALUES ($1,$2,$3,$4)`, command.ID, account.ID, account.Epoch, account.Exchange); err != nil {
			return fmt.Errorf("sandbox_strategy_session_account_insert_failed")
		}
	}
	return nil
}
