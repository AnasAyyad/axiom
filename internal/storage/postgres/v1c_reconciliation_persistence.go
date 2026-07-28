package postgres

import (
	"context"
	"fmt"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func insertV1CReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if err := insertV1CReconciliationHeader(ctx, tx, result); err != nil {
		return err
	}
	for index, difference := range result.Differences {
		if err := insertV1CReconciliationDifference(
			ctx, tx, result, index, difference,
		); err != nil {
			return err
		}
	}
	return nil
}

func insertV1CReconciliationHeader(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	tag, err := tx.Exec(ctx, `
INSERT INTO v1c_reconciliations(
 id,account_id,account_epoch,state,evidence_hash,reconciled_at
) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (id) DO NOTHING`,
		result.ID, result.AccountID, result.AccountEpoch, result.State,
		result.EvidenceHash, result.ReconciledAt,
	)
	if err != nil {
		return fmt.Errorf("v1c_reconciliation_insert_failed")
	}
	if tag.RowsAffected() == 0 && !sameV1CReconciliation(ctx, tx, result) {
		return fmt.Errorf("v1c_reconciliation_identity_conflict")
	}
	return nil
}

func sameV1CReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) bool {
	var same bool
	err := tx.QueryRow(ctx, `
SELECT account_id=$2 AND account_epoch=$3 AND state=$4 AND
       evidence_hash=$5 AND reconciled_at=$6
FROM v1c_reconciliations WHERE id=$1`,
		result.ID, result.AccountID, result.AccountEpoch, result.State,
		result.EvidenceHash, result.ReconciledAt,
	).Scan(&same)
	return err == nil && same
}

func insertV1CReconciliationDifference(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
	index int,
	difference sandbox.Difference,
) error {
	id := fmt.Sprintf("%s-%03d", result.ID, index)
	var asset any
	var quantity any
	if difference.Asset != "" {
		asset = difference.Asset
		quantity = difference.Quantity.String()
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO v1c_reconciliation_differences(
 id,reconciliation_id,account_id,account_epoch,category,classification,
 expected_hash,actual_hash,asset_symbol,quantity,critical,state,recorded_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (id) DO NOTHING`,
		id, result.ID, result.AccountID, result.AccountEpoch,
		difference.Category, difference.Classification, difference.ExpectedHash,
		difference.ActualHash, asset, quantity, difference.Critical,
		reconciliationDifferenceState(difference), result.ReconciledAt,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_difference_insert_failed")
	}
	return nil
}

func reconciliationDifferenceState(difference sandbox.Difference) string {
	if difference.Critical {
		return "QUARANTINED"
	}
	return "OPEN"
}

func quarantineV1CReconciliation(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if err := quarantineV1CReconciliationCapacity(ctx, tx, result); err != nil {
		return err
	}
	return quarantineV1CReconciliationControl(ctx, tx, result)
}

func quarantineV1CReconciliationCapacity(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='QUARANTINED',revision=revision+1,updated_at=$3
WHERE id=$1 AND current_epoch=$2`,
		result.AccountID, result.AccountEpoch, result.ReconciledAt,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_account_quarantine_failed")
	}
	if err := quarantineV1CSubmissionCapacity(ctx, tx, result); err != nil {
		return err
	}
	return quarantineV1CPlanCapacity(ctx, tx, result)
}

func quarantineV1CSubmissionCapacity(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_submission_outbox
SET state='UNKNOWN',order_state='RECOVERY_REQUIRED',updated_at=$3
WHERE account_id=$1 AND account_epoch=$2 AND state<>'TERMINAL'`,
		result.AccountID, result.AccountEpoch, result.ReconciledAt,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_order_quarantine_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_reservations
SET state='QUARANTINED'
WHERE account_id=$1 AND account_epoch=$2 AND state='ACTIVE'`,
		result.AccountID, result.AccountEpoch,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_reservation_quarantine_failed")
	}
	return nil
}

func quarantineV1CPlanCapacity(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_submission_plans plan
SET state='QUARANTINED',final_disposition='unresolved_reconciliation',
    saga_revision=saga_revision+1,revision=revision+1
WHERE EXISTS (
  SELECT 1 FROM v1c_submission_outbox outbox
  WHERE outbox.plan_id=plan.id AND outbox.account_id=$1 AND outbox.account_epoch=$2
) AND state<>'QUARANTINED'`,
		result.AccountID, result.AccountEpoch,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_plan_quarantine_failed")
	}
	return nil
}

func quarantineV1CReconciliationControl(
	ctx context.Context,
	tx pgx.Tx,
	result sandbox.ReconciliationResult,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_arms arm
SET revoked_at=coalesce(revoked_at,$3),revision=revision+1
WHERE EXISTS (
  SELECT 1 FROM v1c_sandbox_session_accounts membership
  WHERE membership.session_id=arm.sandbox_session_id
    AND membership.account_id=$1 AND membership.account_epoch=$2
) AND revoked_at IS NULL`,
		result.AccountID, result.AccountEpoch, result.ReconciledAt,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_arm_revoke_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE v1c_sandbox_sessions session
SET state='DEGRADED',revision=revision+1,updated_at=$3
WHERE EXISTS (
  SELECT 1 FROM v1c_sandbox_session_accounts membership
  WHERE membership.session_id=session.id
    AND membership.account_id=$1 AND membership.account_epoch=$2
) AND state<>'STOPPED'`,
		result.AccountID, result.AccountEpoch, result.ReconciledAt,
	); err != nil {
		return fmt.Errorf("v1c_reconciliation_session_degrade_failed")
	}
	return nil
}
