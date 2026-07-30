package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

const recordV1CEngineObservationSQL = `
INSERT INTO v1c_engine_observations(
 account_id,account_epoch,exchange,startup_cycle,eligibility,
 private_stream_healthy,reconciliation_clean,evidence_healthy,observed_at
)
SELECT $1,$2,$3,$4,$5,true,true,true,$6
WHERE EXISTS(
 SELECT 1 FROM v1c_exchange_accounts account
 JOIN v1c_account_leases lease ON lease.account_id=account.id
 WHERE account.id=$1 AND account.current_epoch=$2 AND account.exchange=$3
   AND lease.fencing_token=$4 AND lease.expires_at>$6
)
ON CONFLICT (account_id) DO UPDATE
SET account_epoch=EXCLUDED.account_epoch,exchange=EXCLUDED.exchange,
 startup_cycle=EXCLUDED.startup_cycle,eligibility=EXCLUDED.eligibility,
 private_stream_healthy=true,reconciliation_clean=true,
 evidence_healthy=true,observed_at=EXCLUDED.observed_at
WHERE v1c_engine_observations.startup_cycle<=EXCLUDED.startup_cycle
 AND v1c_engine_observations.observed_at<=EXCLUDED.observed_at`

// RecordEngineObservation publishes the latest complete, credential-owner
// admission snapshot. It contains public health booleans only, never private
// payloads, balances, credentials, signatures, prices, or quantities.
func (store *V1CDispatcherStore) RecordEngineObservation(
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
		return fmt.Errorf("v1c_engine_observation_invalid")
	}
	encoded, err := json.Marshal(eligibility)
	if err != nil {
		return fmt.Errorf("v1c_engine_observation_encode_failed")
	}
	tag, err := store.pool.Exec(ctx, recordV1CEngineObservationSQL,
		account,
		epoch,
		exchange,
		startupCycle,
		encoded,
		eligibility.ObservedAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("v1c_engine_observation_write_failed")
	}
	return nil
}
