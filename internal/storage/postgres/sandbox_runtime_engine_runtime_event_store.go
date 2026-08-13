package postgres

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
)

var sandboxRuntimeEngineRuntimeEventKinds = []string{
	"PRIVATE_STREAM",
	"PRIVATE_RECONNECT",
	"UNKNOWN_RECOVERY",
	"RECONCILIATION",
}

// RecordEngineRuntimeRecoveryEvent appends one classified, redacted failure
// or successful recovery event. It is limited to read-only reconciliation and
// private-stream recovery boundaries.
func (store *SandboxRuntimeDispatcherStore) RecordEngineRuntimeRecoveryEvent(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	kind string,
	duration time.Duration,
	succeeded bool,
	failureKind exchangecontracts.ErrorKind,
	causeCode string,
	occurredAt time.Time,
) error {
	if kind != "RECONCILIATION" && kind != "PRIVATE_STREAM" &&
		kind != "PRIVATE_RECONNECT" {
		return fmt.Errorf("sandbox_runtime_engine_runtime_event_invalid")
	}
	return store.recordEngineRuntimeEvent(
		ctx, account, epoch, exchange, startupCycle, kind, duration,
		succeeded, failureKind, causeCode, occurredAt,
	)
}

var sandboxRuntimeEngineRuntimeCausePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

const sandboxRuntimeInsertEngineRuntimeEventSQL = `
INSERT INTO sandbox_runtime_engine_runtime_events(
 id,account_id,account_epoch,exchange,startup_cycle,kind,
	duration_ms,succeeded,failure_kind,cause_code,evidence_hash,occurred_at
) SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12
WHERE EXISTS(
 SELECT 1 FROM sandbox_runtime_exchange_accounts account
 JOIN sandbox_runtime_account_leases lease ON lease.account_id=account.id
 WHERE account.id=$2 AND account.current_epoch=$3
   AND account.exchange=$4 AND lease.fencing_token=$5
   AND lease.expires_at>$12
)`

// RecordEngineRuntimeEvent appends one bounded, redacted engine recovery fact.
// It deliberately stores no endpoint, request, account payload, or credential.
func (store *SandboxRuntimeDispatcherStore) RecordEngineRuntimeEvent(
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
	return store.recordEngineRuntimeEvent(
		ctx, account, epoch, exchange, startupCycle, kind, duration,
		succeeded, "", "", occurredAt,
	)
}

// RecordEngineRuntimeReconciliationEvent appends one redacted reconciliation
// outcome with only the typed exchange class and sanitized cause code.
func (store *SandboxRuntimeDispatcherStore) RecordEngineRuntimeReconciliationEvent(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	duration time.Duration,
	succeeded bool,
	failureKind exchangecontracts.ErrorKind,
	causeCode string,
	occurredAt time.Time,
) error {
	return store.recordEngineRuntimeEvent(
		ctx, account, epoch, exchange, startupCycle, "RECONCILIATION",
		duration, succeeded, failureKind, causeCode, occurredAt,
	)
}

func (store *SandboxRuntimeDispatcherStore) recordEngineRuntimeEvent(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	kind string,
	duration time.Duration,
	succeeded bool,
	failureKind exchangecontracts.ErrorKind,
	causeCode string,
	occurredAt time.Time,
) error {
	if !validSandboxRuntimeEngineRuntimeEvent(
		account, epoch, exchange, startupCycle, kind, duration, succeeded,
		failureKind, causeCode, occurredAt,
	) {
		return fmt.Errorf("sandbox_runtime_engine_runtime_event_invalid")
	}
	durationMillis := duration.Milliseconds()
	evidenceHash := sandboxRuntimeEngineRuntimeEventHash(
		account, epoch, exchange, startupCycle, kind, durationMillis,
		succeeded, failureKind, causeCode, occurredAt,
	)
	tag, err := store.pool.Exec(ctx, sandboxRuntimeInsertEngineRuntimeEventSQL,
		evidenceHash,
		account,
		epoch,
		exchange,
		startupCycle,
		kind,
		durationMillis,
		succeeded,
		nullableRuntimeFailureKind(failureKind),
		nullableRuntimeCause(causeCode),
		evidenceHash,
		occurredAt,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_engine_runtime_event_write_failed")
	}
	return nil
}

func validSandboxRuntimeEngineRuntimeEvent(
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	kind string,
	duration time.Duration,
	succeeded bool,
	failureKind exchangecontracts.ErrorKind,
	causeCode string,
	occurredAt time.Time,
) bool {
	baseValid := account != "" && epoch > 0 && startupCycle > 0 &&
		slices.Contains(sandboxRuntimeEngineRuntimeEventKinds, kind) &&
		(exchange == sandbox.ExchangeBinance || exchange == sandbox.ExchangeBybit) &&
		duration >= 0 && !occurredAt.IsZero() && occurredAt.Location() == time.UTC
	if !baseValid || succeeded {
		return baseValid && failureKind == "" && causeCode == ""
	}
	return kind == "UNKNOWN_RECOVERY" ||
		(validRuntimeFailureKind(failureKind) &&
			sandboxRuntimeEngineRuntimeCausePattern.MatchString(causeCode))
}

func sandboxRuntimeEngineRuntimeEventHash(
	account sandbox.AccountID,
	epoch uint64,
	exchange sandbox.Exchange,
	startupCycle uint64,
	kind string,
	durationMillis int64,
	succeeded bool,
	failureKind exchangecontracts.ErrorKind,
	causeCode string,
	occurredAt time.Time,
) string {
	return stableSandboxRuntimeHash(
		string(account), strconv.FormatUint(epoch, 10), string(exchange),
		strconv.FormatUint(startupCycle, 10), kind,
		strconv.FormatInt(durationMillis, 10), strconv.FormatBool(succeeded),
		string(failureKind), causeCode, occurredAt.Format(time.RFC3339Nano),
	)
}

func validRuntimeFailureKind(kind exchangecontracts.ErrorKind) bool {
	switch kind {
	case exchangecontracts.ErrorCapability, exchangecontracts.ErrorRateLimit,
		exchangecontracts.ErrorTransient, exchangecontracts.ErrorTimestamp,
		exchangecontracts.ErrorFilter, exchangecontracts.ErrorInsufficientFunds,
		exchangecontracts.ErrorMaintenance, exchangecontracts.ErrorValidation,
		exchangecontracts.ErrorAmbiguousState, exchangecontracts.ErrorCanceled:
		return true
	default:
		return false
	}
}

func nullableRuntimeFailureKind(kind exchangecontracts.ErrorKind) *string {
	if kind == "" {
		return nil
	}
	value := string(kind)
	return &value
}

func nullableRuntimeCause(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
