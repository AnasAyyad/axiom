package postgres

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"axiom/internal/sandbox"
)

var v1cEngineRuntimeEventKinds = []string{
	"PRIVATE_RECONNECT",
	"UNKNOWN_RECOVERY",
	"RECONCILIATION",
}

const v1cInsertEngineRuntimeEventSQL = `
INSERT INTO v1c_engine_runtime_events(
 id,account_id,account_epoch,exchange,startup_cycle,kind,
 duration_ms,succeeded,evidence_hash,occurred_at
) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10
WHERE EXISTS(
 SELECT 1 FROM v1c_exchange_accounts account
 JOIN v1c_account_leases lease ON lease.account_id=account.id
 WHERE account.id=$2 AND account.current_epoch=$3
   AND account.exchange=$4 AND lease.fencing_token=$5
   AND lease.expires_at>$10
)`

// RecordEngineRuntimeEvent appends one bounded, redacted engine recovery fact.
// It deliberately stores no endpoint, request, account payload, or credential.
func (store *V1CDispatcherStore) RecordEngineRuntimeEvent(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	kind string,
	duration time.Duration,
	succeeded bool,
	occurredAt time.Time,
) error {
	if account == "" || epoch == 0 || startupCycle == 0 ||
		!slices.Contains(v1cEngineRuntimeEventKinds, kind) ||
		(exchange != sandbox.ExchangeBinance &&
			exchange != sandbox.ExchangeBybit) ||
		duration < 0 || occurredAt.IsZero() ||
		occurredAt.Location() != time.UTC {
		return fmt.Errorf("v1c_engine_runtime_event_invalid")
	}
	durationMillis := duration.Milliseconds()
	evidenceHash := stableV1CHash(
		string(account),
		strconv.FormatUint(epoch, 10),
		string(exchange),
		strconv.FormatUint(startupCycle, 10),
		kind,
		strconv.FormatInt(durationMillis, 10),
		strconv.FormatBool(succeeded),
		occurredAt.Format(time.RFC3339Nano),
	)
	tag, err := store.pool.Exec(ctx, v1cInsertEngineRuntimeEventSQL,
		evidenceHash,
		account,
		epoch,
		exchange,
		startupCycle,
		kind,
		durationMillis,
		succeeded,
		evidenceHash,
		occurredAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("v1c_engine_runtime_event_write_failed")
	}
	return nil
}
