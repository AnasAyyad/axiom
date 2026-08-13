package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const countCanaryCreateEvidenceSQL = `
SELECT count(*)
FROM sandbox_runtime_authenticated_request_evidence
WHERE exchange=$1 AND method='POST' AND path=$2
  AND recorded_at>=$3 AND recorded_at<=$4`

// ReadCanaryOrderStatus returns no private order values.
func (store *SandboxRuntimeDispatcherStore) ReadCanaryOrderStatus(
	ctx context.Context,
	exchange sandbox.Exchange,
	canaryID string,
) (sandbox.CanaryOrderStatus, error) {
	if canaryID == "" ||
		(exchange != sandbox.ExchangeBinance &&
			exchange != sandbox.ExchangeBybit) {
		return sandbox.CanaryOrderStatus{}, fmt.Errorf("sandbox_runtime_canary_status_invalid")
	}
	var result sandbox.CanaryOrderStatus
	var epoch, attempt int64
	err := store.pool.QueryRow(ctx, `
SELECT plan.id,plan.sandbox_session_id,plan.configuration_id,
       account.exchange,outbox.account_id,
       outbox.account_epoch,outbox.client_order_id,outbox.state,
       outbox.order_state,outbox.attempt,plan.approved_at
FROM sandbox_runtime_submission_plans plan
JOIN sandbox_runtime_submission_outbox outbox ON outbox.plan_id=plan.id
JOIN sandbox_runtime_exchange_accounts account ON account.id=outbox.account_id
WHERE plan.id=$1 AND plan.intent_kind='CANARY' AND plan.leg_count=1
  AND account.exchange=$2`,
		canaryID,
		exchange,
	).Scan(
		&result.PlanID,
		&result.SessionID,
		&result.ConfigurationID,
		&result.Exchange,
		&result.AccountID,
		&epoch,
		&result.ClientOrderID,
		&result.OutboxState,
		&result.OrderState,
		&attempt,
		&result.ApprovedAt,
	)
	if err != nil || epoch <= 0 || attempt < 0 {
		return sandbox.CanaryOrderStatus{}, fmt.Errorf("sandbox_runtime_canary_status_unavailable")
	}
	result.CanaryID = result.PlanID
	result.AccountEpoch = uint64(epoch)
	result.Attempt = uint32(attempt)
	result.ApprovedAt = result.ApprovedAt.UTC()
	return result, nil
}

// ReadEngineCommandResult returns the closed state and redacted evidence hash.
func (store *SandboxRuntimeDispatcherStore) ReadEngineCommandResult(
	ctx context.Context,
	id string,
) (sandbox.EngineCommandState, string, error) {
	if id == "" {
		return "", "", fmt.Errorf("sandbox_runtime_engine_command_result_invalid")
	}
	var state sandbox.EngineCommandState
	var evidence *string
	if err := store.pool.QueryRow(ctx, `
SELECT state,evidence_hash
FROM sandbox_runtime_engine_commands
WHERE id=$1`,
		id,
	).Scan(&state, &evidence); err != nil {
		return "", "", fmt.Errorf("sandbox_runtime_engine_command_result_unavailable")
	}
	if evidence == nil {
		return state, "", nil
	}
	return state, *evidence, nil
}

// ReadCanaryEvidence returns an ordered immutable export projection.
func (store *SandboxRuntimeDispatcherStore) ReadCanaryEvidence(
	ctx context.Context,
	exchange sandbox.Exchange,
	canaryID string,
) ([]sandbox.CanaryEvidenceRecord, error) {
	if canaryID == "" ||
		(exchange != sandbox.ExchangeBinance &&
			exchange != sandbox.ExchangeBybit) {
		return nil, fmt.Errorf("sandbox_runtime_canary_evidence_read_invalid")
	}
	rows, err := store.pool.Query(ctx, `
SELECT id,canary_id,exchange,account_id,account_epoch,
       sandbox_session_id,plan_id,stage,startup_cycle,
       evidence_hash,observed_at
FROM sandbox_runtime_canary_evidence
WHERE exchange=$1 AND canary_id=$2
ORDER BY observed_at,stage`,
		exchange,
		canaryID,
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox_runtime_canary_evidence_read_failed")
	}
	defer rows.Close()
	result := make([]sandbox.CanaryEvidenceRecord, 0, 5)
	for rows.Next() {
		record, scanErr := scanCanaryEvidenceRecord(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, record)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("sandbox_runtime_canary_evidence_read_failed")
	}
	return result, nil
}

func scanCanaryEvidenceRecord(
	rows pgx.Rows,
) (sandbox.CanaryEvidenceRecord, error) {
	var record sandbox.CanaryEvidenceRecord
	var epoch, cycle int64
	err := rows.Scan(
		&record.ID, &record.CanaryID, &record.Exchange, &record.AccountID,
		&epoch, &record.SessionID, &record.PlanID, &record.Stage, &cycle,
		&record.EvidenceHash, &record.ObservedAt,
	)
	if err != nil || epoch <= 0 || cycle <= 0 {
		return sandbox.CanaryEvidenceRecord{},
			fmt.Errorf("sandbox_runtime_canary_evidence_read_failed")
	}
	record.AccountEpoch = uint64(epoch)
	record.StartupCycle = uint64(cycle)
	record.ObservedAt = record.ObservedAt.UTC()
	return record, nil
}

// ReadyCanaryRestartCycle proves a newer startup reached READY_PAUSED under a
// current lease after the controlled restart.
func (store *SandboxRuntimeDispatcherStore) ReadyCanaryRestartCycle(
	ctx context.Context,
	account sandbox.AccountID,
	epoch, priorCycle uint64,
	now time.Time,
) (uint64, error) {
	if account == "" || epoch == 0 || priorCycle == 0 ||
		now.IsZero() || now.Location() != time.UTC {
		return 0, fmt.Errorf("sandbox_runtime_canary_restart_invalid")
	}
	var cycle int64
	err := store.pool.QueryRow(ctx, `
SELECT observation.startup_cycle
FROM sandbox_runtime_engine_observations observation
JOIN sandbox_runtime_exchange_accounts account ON account.id=observation.account_id
JOIN sandbox_runtime_account_leases lease
  ON lease.account_id=account.id
 AND lease.fencing_token=observation.startup_cycle
WHERE account.id=$1
  AND account.current_epoch=$2
  AND account.state='READY_PAUSED'
  AND observation.account_epoch=$2
  AND observation.startup_cycle>$3
  AND observation.private_stream_healthy
  AND observation.reconciliation_clean
  AND observation.evidence_healthy
  AND observation.observed_at<=$4
  AND $4-observation.observed_at<=interval '2 seconds'
  AND lease.expires_at>$4
  AND EXISTS(
    SELECT 1 FROM sandbox_runtime_engine_startup_evidence evidence
    WHERE evidence.account_id=account.id
      AND evidence.startup_cycle=observation.startup_cycle
      AND evidence.stage='enter_ready_paused'
      AND evidence.reached_healthy
  )`,
		account,
		epoch,
		priorCycle,
		now,
	).Scan(&cycle)
	if err != nil || cycle <= 0 {
		return 0, fmt.Errorf("sandbox_runtime_canary_restart_not_ready")
	}
	return uint64(cycle), nil
}

// CountCanaryCreateEvidence counts only the exchange order-create route in the
// canary interval. A controlled restart must leave this count exactly one.
func (store *SandboxRuntimeDispatcherStore) CountCanaryCreateEvidence(
	ctx context.Context,
	exchange sandbox.Exchange,
	from, through time.Time,
) (int64, error) {
	if (exchange != sandbox.ExchangeBinance &&
		exchange != sandbox.ExchangeBybit) ||
		from.IsZero() || through.Before(from) {
		return 0, fmt.Errorf("sandbox_runtime_canary_create_count_invalid")
	}
	path := "/api/v3/order"
	if exchange == sandbox.ExchangeBybit {
		path = "/v5/order/create"
	}
	var count int64
	if err := store.pool.QueryRow(ctx, countCanaryCreateEvidenceSQL,
		exchange,
		path,
		from,
		through,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_canary_create_count_failed")
	}
	return count, nil
}
