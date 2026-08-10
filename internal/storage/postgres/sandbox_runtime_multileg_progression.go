package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// advanceSandboxRuntimeSequentialPlan exposes exactly one dependent leg after a fully
// filled predecessor. The caller invokes it only after the fill, accounting
// journal, and reservation transition are durable in the same transaction.
// An expired candidate or arm leaves every future leg non-dispatchable and
// quarantines its claims for explicit recovery.
func advanceSandboxRuntimeSequentialPlan(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
	completedLeg uint32,
	orderState string,
	output *sandbox.FilledOutput,
	now time.Time,
) error {
	var policy string
	var legCount int
	if err := tx.QueryRow(ctx, `
SELECT dispatch_policy,leg_count
FROM sandbox_runtime_submission_plans WHERE id=$1 FOR UPDATE`, planID,
	).Scan(&policy, &legCount); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_plan_read_failed")
	}
	if policy != "sequential" || legCount == 1 || int(completedLeg)+1 >= legCount {
		return nil
	}
	switch orderState {
	case "FILLED":
		if output == nil {
			return quarantineSandboxRuntimeWaitingLegs(ctx, tx, planID, completedLeg, now)
		}
		return promoteSandboxRuntimeSequentialLeg(ctx, tx, planID, completedLeg, *output, now)
	case "CANCELED", "REJECTED", "EXPIRED":
		return closeSandboxRuntimeUnsentDependentLegs(ctx, tx, planID, completedLeg, now)
	default:
		return nil
	}
}

func promoteSandboxRuntimeSequentialLeg(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
	completedLeg uint32,
	output sandbox.FilledOutput,
	now time.Time,
) error {
	outboxID, reservationID, assetText, quantityText, err := loadSandboxRuntimeSequentialLeg(ctx, tx, planID, completedLeg)
	if err != nil {
		return err
	}
	asset, assetErr := domain.ParseAssetSymbol(assetText)
	required, quantityErr := domain.ParseBalance(quantityText)
	zero, _ := domain.ParseBalance("0")
	if assetErr != nil || quantityErr != nil || output.Asset == "" ||
		output.Quantity.Compare(zero) <= 0 || asset != output.Asset || required.Compare(output.Quantity) > 0 {
		return quarantineSandboxRuntimeWaitingLegs(ctx, tx, planID, completedLeg, now)
	}
	eligible, err := sandboxRuntimeSequentialLegEligible(ctx, tx, planID, outboxID, now)
	if err != nil {
		return err
	}
	if !eligible {
		return quarantineSandboxRuntimeWaitingLegs(ctx, tx, planID, completedLeg, now)
	}
	tag, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_reservations
SET state='ACTIVE'
WHERE id=$1 AND state='WAITING'`, reservationID)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_multileg_reservation_activate_failed")
	}
	tag, err = tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET state='PENDING',updated_at=$2
WHERE id=$1 AND state='WAITING'`, outboxID, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_multileg_dependency_promote_failed")
	}
	return nil
}

func loadSandboxRuntimeSequentialLeg(ctx context.Context, tx pgx.Tx, planID string,
	completedLeg uint32,
) (string, string, string, string, error) {
	var outboxID, reservationID, asset, quantity string
	err := tx.QueryRow(ctx, `
SELECT next.id,reservation.id,reservation.asset_symbol,reservation.quantity::text
FROM sandbox_runtime_submission_outbox next
JOIN sandbox_runtime_sandbox_reservations reservation
  ON reservation.plan_id=next.plan_id AND reservation.order_id=next.order_id
WHERE next.plan_id=$1 AND next.leg_index=$2
  AND next.depends_on_leg_index=$3 AND next.state='WAITING'
  AND reservation.state='WAITING'
FOR UPDATE OF next,reservation`,
		planID, completedLeg+1, completedLeg,
	).Scan(&outboxID, &reservationID, &asset, &quantity)
	if err != nil {
		err = fmt.Errorf("sandbox_runtime_multileg_dependency_read_failed")
	}
	return outboxID, reservationID, asset, quantity, err
}

func sandboxRuntimeSequentialLegEligible(ctx context.Context, tx pgx.Tx, planID, outboxID string,
	now time.Time,
) (bool, error) {
	var eligible bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM sandbox_runtime_submission_plans plan
    JOIN sandbox_runtime_sandbox_arms arm ON arm.id=plan.arm_id
    JOIN sandbox_runtime_sandbox_sessions sandbox_session
      ON sandbox_session.id=plan.sandbox_session_id
    JOIN sandbox_runtime_submission_outbox next ON next.plan_id=plan.id
    JOIN sandbox_runtime_exchange_accounts account ON account.id=next.account_id
    JOIN sandbox_runtime_plan_entry_safety safety
      ON safety.plan_id=plan.id
     AND safety.account_id=next.account_id
     AND safety.account_epoch=next.account_epoch
    JOIN sandbox_runtime_account_leases lease ON lease.account_id=next.account_id
    JOIN sessions actor_session ON actor_session.id=arm.actor_session_id
    JOIN users actor ON actor.id=actor_session.user_id
    WHERE plan.id=$1 AND next.id=$2
      AND plan.execution_expires_at>$3
      AND arm.revoked_at IS NULL
      AND arm.expires_at>$3
      AND sandbox_session.state='ARMED'
      AND account.state='ARMED'
      AND account.current_epoch=next.account_epoch
      AND lease.expires_at>$3
      AND actor.id=arm.actor_user_id
      AND actor.status='active'
      AND actor_session.revoked_at IS NULL
      AND actor_session.expires_at>$3
		AND actor_session.idle_expires_at>$3
	)`, planID, outboxID, now).Scan(&eligible); err != nil {
		return false, fmt.Errorf("sandbox_runtime_multileg_dependency_promote_failed")
	}
	return eligible, nil
}

func quarantineSandboxRuntimeWaitingLegs(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
	completedLeg uint32,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_reservations reservation
SET state='QUARANTINED'
WHERE reservation.plan_id=$1
  AND reservation.state IN ('WAITING','ACTIVE')
  AND EXISTS (
    SELECT 1 FROM sandbox_runtime_submission_outbox outbox
    WHERE outbox.plan_id=reservation.plan_id
      AND outbox.order_id=reservation.order_id
      AND outbox.leg_index>$2
      AND outbox.state='WAITING'
  )`, planID, completedLeg); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_waiting_reservation_quarantine_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET order_state='RECOVERY_REQUIRED',updated_at=$3
WHERE plan_id=$1 AND leg_index>$2 AND state='WAITING'`,
		planID, completedLeg, now); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_waiting_quarantine_failed")
	}
	return nil
}

func closeSandboxRuntimeUnsentDependentLegs(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
	completedLeg uint32,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_reservations reservation
SET state='RELEASED',released_at=$3,release_reason='dependency_not_filled'
WHERE reservation.plan_id=$1
  AND reservation.state='WAITING'
  AND EXISTS (
    SELECT 1 FROM sandbox_runtime_submission_outbox outbox
    WHERE outbox.plan_id=reservation.plan_id
      AND outbox.order_id=reservation.order_id
      AND outbox.leg_index>$2
      AND outbox.state='WAITING'
  )`, planID, completedLeg, now); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_waiting_reservation_release_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET state='TERMINAL',order_state='EXPIRED',updated_at=$3
WHERE plan_id=$1 AND leg_index>$2 AND state='WAITING'`,
		planID, completedLeg, now); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_waiting_close_failed")
	}
	return nil
}

func quarantineSandboxRuntimeWaitingAccountLegs(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	epoch uint64,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET order_state='RECOVERY_REQUIRED',updated_at=$3
WHERE account_id=$1 AND account_epoch=$2 AND state='WAITING'`,
		account, epoch, now); err != nil {
		return fmt.Errorf("sandbox_runtime_multileg_account_waiting_quarantine_failed")
	}
	return nil
}
