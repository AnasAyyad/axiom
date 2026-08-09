package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const recordSandboxRuntimeEngineObservationSQL = `
INSERT INTO sandbox_runtime_engine_observations(
 account_id,account_epoch,exchange,startup_cycle,eligibility,
 private_stream_healthy,reconciliation_clean,evidence_healthy,observed_at
)
SELECT $1,$2,$3,$4,$5,true,true,true,$6
WHERE EXISTS(
 SELECT 1 FROM sandbox_runtime_exchange_accounts account
 JOIN sandbox_runtime_account_leases lease ON lease.account_id=account.id
 WHERE account.id=$1 AND account.current_epoch=$2 AND account.exchange=$3
   AND lease.fencing_token=$4 AND lease.expires_at>$6
)
ON CONFLICT (account_id) DO UPDATE
SET account_epoch=EXCLUDED.account_epoch,exchange=EXCLUDED.exchange,
 startup_cycle=EXCLUDED.startup_cycle,eligibility=EXCLUDED.eligibility,
 private_stream_healthy=true,reconciliation_clean=true,
 evidence_healthy=true,observed_at=EXCLUDED.observed_at
WHERE sandbox_runtime_engine_observations.startup_cycle<=EXCLUDED.startup_cycle
 AND sandbox_runtime_engine_observations.observed_at<=EXCLUDED.observed_at`

const recordSandboxRuntimeEngineMarketObservationSQL = `
INSERT INTO sandbox_runtime_engine_market_observations(
 account_id,account_epoch,exchange,instrument,startup_cycle,eligibility,observed_at
)
SELECT $1,$2,$3,$4,$5,$6,$7
WHERE EXISTS(
 SELECT 1 FROM sandbox_runtime_exchange_accounts account
 JOIN sandbox_runtime_account_leases lease ON lease.account_id=account.id
 WHERE account.id=$1 AND account.current_epoch=$2 AND account.exchange=$3
   AND lease.fencing_token=$5 AND lease.expires_at>$7
)
ON CONFLICT (account_id,instrument) DO UPDATE
SET account_epoch=EXCLUDED.account_epoch,exchange=EXCLUDED.exchange,
 startup_cycle=EXCLUDED.startup_cycle,eligibility=EXCLUDED.eligibility,
 observed_at=EXCLUDED.observed_at
WHERE sandbox_runtime_engine_market_observations.startup_cycle<=EXCLUDED.startup_cycle
 AND sandbox_runtime_engine_market_observations.observed_at<=EXCLUDED.observed_at`

// RecordEngineObservation publishes the latest complete, credential-owner
// admission snapshot. It contains public health booleans only, never private
// payloads, balances, credentials, signatures, prices, or quantities.
func (store *SandboxRuntimeDispatcherStore) RecordEngineObservation(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	eligibility exchangecontracts.CollectorHealthSnapshot,
) error {
	if account == "" || epoch == 0 || startupCycle == 0 ||
		(exchange != sandbox.ExchangeBinance &&
			exchange != sandbox.ExchangeBybit) ||
		!eligibility.Eligible ||
		eligibility.Exchange != string(exchange) ||
		eligibility.Instrument == "" ||
		eligibility.ObservedAt.IsZero() ||
		eligibility.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("sandbox_runtime_engine_observation_invalid")
	}
	encoded, err := json.Marshal(eligibility)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_engine_observation_encode_failed")
	}
	tag, err := store.pool.Exec(ctx, recordSandboxRuntimeEngineObservationSQL,
		account,
		epoch,
		exchange,
		startupCycle,
		encoded,
		eligibility.ObservedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_engine_observation_write_failed")
	}
	return nil
}

// RecordEngineObservations atomically publishes the latest account summary and
// one exact, fenced public-readiness record per supported instrument. The
// summary remains for legacy operational projections; admission selects the
// exact instrument record so BTC readiness can never authorize ETH.
func (store *SandboxRuntimeDispatcherStore) RecordEngineObservations(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	eligibilities []exchangecontracts.CollectorHealthSnapshot,
) error {
	if !validSandboxRuntimeEngineObservations(account, epoch, exchange, startupCycle, eligibilities) {
		return fmt.Errorf("sandbox_runtime_engine_observations_invalid")
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_engine_observations_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	first := eligibilities[0]
	encoded, err := json.Marshal(first)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_engine_observations_encode_failed")
	}
	tag, err := tx.Exec(ctx, recordSandboxRuntimeEngineObservationSQL,
		account, epoch, exchange, startupCycle, encoded, first.ObservedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_engine_observations_summary_write_failed")
	}
	if err = insertSandboxRuntimeEngineMarketObservations(ctx, tx, account, epoch, exchange,
		startupCycle, eligibilities); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_observations_commit_failed")
	}
	return nil
}

func validSandboxRuntimeEngineObservations(account sandbox.AccountID, epoch uint64, exchange sandbox.Exchange,
	startupCycle uint64, eligibilities []exchangecontracts.CollectorHealthSnapshot,
) bool {
	if account == "" || epoch == 0 || startupCycle == 0 || len(eligibilities) == 0 ||
		(exchange != sandbox.ExchangeBinance && exchange != sandbox.ExchangeBybit) {
		return false
	}
	seen := make(map[string]struct{}, len(eligibilities))
	for _, eligibility := range eligibilities {
		_, duplicate := seen[eligibility.Instrument]
		if duplicate || !eligibility.Eligible || eligibility.Exchange != string(exchange) ||
			(eligibility.Instrument != "BTCUSDT" && eligibility.Instrument != "ETHUSDT" &&
				eligibility.Instrument != "ETHBTC") ||
			eligibility.ObservedAt.IsZero() || eligibility.ObservedAt.Location() != time.UTC {
			return false
		}
		seen[eligibility.Instrument] = struct{}{}
	}
	return true
}

func insertSandboxRuntimeEngineMarketObservations(ctx context.Context, tx pgx.Tx, account sandbox.AccountID,
	epoch uint64, exchange sandbox.Exchange, startupCycle uint64,
	eligibilities []exchangecontracts.CollectorHealthSnapshot,
) error {
	for _, eligibility := range eligibilities {
		encoded, err := json.Marshal(eligibility)
		if err != nil {
			return fmt.Errorf("sandbox_runtime_engine_observations_encode_failed")
		}
		tag, err := tx.Exec(ctx, recordSandboxRuntimeEngineMarketObservationSQL,
			account, epoch, exchange, eligibility.Instrument, startupCycle, encoded, eligibility.ObservedAt)
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("sandbox_runtime_engine_observations_market_write_failed")
		}
	}
	return nil
}
