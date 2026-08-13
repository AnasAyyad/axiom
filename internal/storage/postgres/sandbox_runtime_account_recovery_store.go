package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// RecordAccountSnapshot appends one immutable exchange-authoritative account
// view. Reusing an identity with another payload fails closed.
func (store *SandboxRuntimeDispatcherStore) RecordAccountSnapshot(
	ctx context.Context,
	id string,
	snapshot sandbox.AccountSnapshot,
) error {
	if id == "" {
		return fmt.Errorf("sandbox_runtime_account_snapshot_invalid")
	}
	if err := snapshot.Validate(); err != nil {
		return err
	}
	balances, err := json.Marshal(snapshot.Balances)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_account_snapshot_encode_failed")
	}
	tag, err := store.pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_snapshots(
 id,account_id,account_epoch,balances_payload,orders_hash,fills_hash,
 snapshot_hash,observed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT DO NOTHING`,
		id,
		snapshot.AccountID,
		snapshot.Epoch,
		string(balances),
		snapshot.OrdersHash,
		snapshot.FillsHash,
		snapshot.SnapshotHash,
		snapshot.ObservedAt,
	)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_account_snapshot_insert_failed")
	}
	if tag.RowsAffected() == 0 &&
		!store.sameSandboxRuntimeAccountSnapshot(ctx, id, snapshot, string(balances)) {
		return fmt.Errorf("sandbox_runtime_account_snapshot_identity_conflict")
	}
	return nil
}

func (store *SandboxRuntimeDispatcherStore) sameSandboxRuntimeAccountSnapshot(
	ctx context.Context,
	id string,
	snapshot sandbox.AccountSnapshot,
	balances string,
) bool {
	var same bool
	err := store.pool.QueryRow(ctx, `
SELECT account_id=$2 AND account_epoch=$3 AND balances_payload=$4::jsonb AND
       orders_hash=$5 AND fills_hash=$6 AND snapshot_hash=$7 AND observed_at=$8
FROM sandbox_runtime_account_snapshots WHERE id=$1`,
		id,
		snapshot.AccountID,
		snapshot.Epoch,
		balances,
		snapshot.OrdersHash,
		snapshot.FillsHash,
		snapshot.SnapshotHash,
		snapshot.ObservedAt,
	).Scan(&same)
	if err == nil {
		return same
	}
	if err != pgx.ErrNoRows {
		return false
	}
	err = store.pool.QueryRow(ctx, `
SELECT balances_payload=$3::jsonb AND orders_hash=$4 AND fills_hash=$5
FROM sandbox_runtime_account_snapshots
WHERE account_id=$1 AND account_epoch=$2 AND snapshot_hash=$6`,
		snapshot.AccountID,
		snapshot.Epoch,
		balances,
		snapshot.OrdersHash,
		snapshot.FillsHash,
		snapshot.SnapshotHash,
	).Scan(&same)
	return err == nil && same
}

// LatestAccountSnapshot returns the newest immutable full account view for
// coherent reset detection.
func (store *SandboxRuntimeDispatcherStore) LatestAccountSnapshot(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
) (sandbox.AccountSnapshot, bool, error) {
	var snapshot sandbox.AccountSnapshot
	var balances []byte
	err := store.pool.QueryRow(ctx, `
SELECT account_id,account_epoch,balances_payload::text,orders_hash,fills_hash,
       snapshot_hash,observed_at
FROM sandbox_runtime_account_snapshots
WHERE account_id=$1 AND account_epoch=$2
ORDER BY observed_at DESC,id DESC
LIMIT 1`,
		account,
		epoch,
	).Scan(
		&snapshot.AccountID,
		&snapshot.Epoch,
		&balances,
		&snapshot.OrdersHash,
		&snapshot.FillsHash,
		&snapshot.SnapshotHash,
		&snapshot.ObservedAt,
	)
	if err == pgx.ErrNoRows {
		return sandbox.AccountSnapshot{}, false, nil
	}
	if err != nil {
		return sandbox.AccountSnapshot{}, false, fmt.Errorf(
			"sandbox_runtime_account_snapshot_read_failed",
		)
	}
	snapshot.ObservedAt = snapshot.ObservedAt.UTC()
	if json.Unmarshal(balances, &snapshot.Balances) != nil ||
		snapshot.Validate() != nil {
		return sandbox.AccountSnapshot{}, false, fmt.Errorf(
			"sandbox_runtime_account_snapshot_read_failed",
		)
	}
	return snapshot, true, nil
}

// RecordAccountReset atomically opens a new account epoch, locks entry,
// quarantines prior-epoch capacity, revokes every affected arm, and records
// explicit non-PnL external adjustments.
func (store *SandboxRuntimeDispatcherStore) RecordAccountReset(
	ctx context.Context,
	incident sandbox.AccountResetIncident,
) error {
	if err := incident.Validate(); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	newEpoch, err := advanceSandboxRuntimeResetEpoch(ctx, tx, incident)
	if err != nil {
		return err
	}
	if err = quarantineSandboxRuntimeResetState(ctx, tx, incident); err != nil {
		return err
	}
	if err = insertSandboxRuntimeResetIncident(ctx, tx, incident, newEpoch); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `
DELETE FROM sandbox_runtime_account_leases WHERE account_id=$1`, incident.AccountID); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_lease_release_failed")
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_commit_failed")
	}
	return nil
}

var _ sandbox.AccountRecoveryStore = (*SandboxRuntimeDispatcherStore)(nil)

func advanceSandboxRuntimeResetEpoch(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
) (uint64, error) {
	var currentEpoch int64
	if err := tx.QueryRow(ctx, `
SELECT current_epoch FROM sandbox_runtime_exchange_accounts
WHERE id=$1 FOR UPDATE`, incident.AccountID).Scan(&currentEpoch); err != nil ||
		uint64(currentEpoch) != incident.PriorEpoch {
		return 0, fmt.Errorf("sandbox_runtime_account_reset_epoch_rejected")
	}
	newEpoch := incident.PriorEpoch + 1
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_account_epochs SET closed_at=$3
WHERE account_id=$1 AND epoch=$2 AND closed_at IS NULL`,
		incident.AccountID, incident.PriorEpoch, incident.DetectedAt,
	); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_account_reset_close_epoch_failed")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,$2,'exchange_reset',$3)`,
		incident.AccountID, newEpoch, incident.DetectedAt,
	); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_account_reset_open_epoch_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_exchange_accounts
SET current_epoch=$2,state='LOCKED',revision=revision+1,updated_at=$3
WHERE id=$1`, incident.AccountID, newEpoch, incident.DetectedAt); err != nil {
		return 0, fmt.Errorf("sandbox_runtime_account_reset_lock_failed")
	}
	return newEpoch, nil
}

func insertSandboxRuntimeResetIncident(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
	newEpoch uint64,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_reset_incidents(
 id,account_id,prior_epoch,new_epoch,evidence_hash,state,detected_at
) VALUES ($1,$2,$3,$4,$5,'RECONCILING',$6)`,
		incident.ID, incident.AccountID, incident.PriorEpoch, newEpoch,
		incident.EvidenceHash, incident.DetectedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_incident_insert_failed")
	}
	for _, adjustment := range incident.Adjustments {
		if err := insertSandboxRuntimeExternalAdjustment(ctx, tx, incident, adjustment); err != nil {
			return err
		}
	}
	return nil
}

func insertSandboxRuntimeExternalAdjustment(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
	adjustment sandbox.ExternalAdjustment,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_external_adjustments(
 id,reset_incident_id,account_id,asset_symbol,quantity,adjustment_hash,
 pnl_effect,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,false,$7)`,
		adjustment.ID, incident.ID, incident.AccountID, adjustment.Asset,
		adjustment.Quantity, adjustment.AdjustmentHash, incident.DetectedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_adjustment_insert_failed")
	}
	return nil
}

func quarantineSandboxRuntimeResetState(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
) error {
	if err := quarantineSandboxRuntimeResetCapacity(ctx, tx, incident); err != nil {
		return err
	}
	return quarantineSandboxRuntimeResetControl(ctx, tx, incident)
}

func quarantineSandboxRuntimeResetCapacity(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET state='UNKNOWN',order_state='RECOVERY_REQUIRED',updated_at=$3
WHERE account_id=$1 AND account_epoch=$2
  AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
		incident.AccountID,
		incident.PriorEpoch,
		incident.DetectedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_order_quarantine_failed")
	}
	if err := quarantineSandboxRuntimeWaitingAccountLegs(
		ctx, tx, incident.AccountID, incident.PriorEpoch, incident.DetectedAt,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_reservations
SET state='QUARANTINED'
WHERE account_id=$1 AND account_epoch=$2 AND state IN ('WAITING','ACTIVE')`,
		incident.AccountID,
		incident.PriorEpoch,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_reservation_quarantine_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_plans plan
SET state='QUARANTINED',final_disposition='exchange_reset',
    saga_revision=saga_revision+1,revision=revision+1
WHERE EXISTS (
  SELECT 1 FROM sandbox_runtime_submission_outbox outbox
  WHERE outbox.plan_id=plan.id AND outbox.account_id=$1
    AND outbox.account_epoch=$2
) AND state<>'QUARANTINED'`,
		incident.AccountID,
		incident.PriorEpoch,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_plan_quarantine_failed")
	}
	return nil
}

func quarantineSandboxRuntimeResetControl(
	ctx context.Context,
	tx pgx.Tx,
	incident sandbox.AccountResetIncident,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_arms arm
SET revoked_at=coalesce(revoked_at,$3),revision=revision+1
WHERE EXISTS (
  SELECT 1 FROM sandbox_runtime_sandbox_session_accounts membership
  WHERE membership.session_id=arm.sandbox_session_id
    AND membership.account_id=$1 AND membership.account_epoch=$2
) AND revoked_at IS NULL`,
		incident.AccountID,
		incident.PriorEpoch,
		incident.DetectedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_arm_revoke_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_sessions session
SET state='DEGRADED',revision=revision+1,updated_at=$3
WHERE EXISTS (
  SELECT 1 FROM sandbox_runtime_sandbox_session_accounts membership
  WHERE membership.session_id=session.id
    AND membership.account_id=$1 AND membership.account_epoch=$2
) AND state<>'STOPPED'`,
		incident.AccountID,
		incident.PriorEpoch,
		incident.DetectedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_account_reset_session_degrade_failed")
	}
	return nil
}

var _ sandbox.AccountRecoveryRepository = (*SandboxRuntimeDispatcherStore)(nil)
