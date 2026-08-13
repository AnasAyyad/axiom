package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/execution"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func advanceSandboxRuntimeOutbox(
	ctx context.Context,
	tx pgx.Tx,
	outboxID string,
	fence uint64,
	state string,
	terminal bool,
	receivedAt time.Time,
) error {
	nextOutbox := "ACKNOWLEDGED"
	if state == "UNKNOWN" || state == "RECOVERY_REQUIRED" ||
		state == "CANCELED" || state == "REJECTED" || state == "EXPIRED" {
		nextOutbox = "UNKNOWN"
	}
	if terminal {
		nextOutbox = "TERMINAL"
	}
	tag, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox SET state=$3,order_state=$4,updated_at=$5
WHERE id=$1 AND (fencing_token IS NULL OR fencing_token<=$2)`,
		outboxID, fence, nextOutbox, state, receivedAt)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_private_event_reduce_failed")
	}
	return nil
}

func postSandboxRuntimeFill(
	ctx context.Context,
	tx pgx.Tx,
	event sandbox.PrivateEvent,
	payload []byte,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeFillPosting); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_fills(
 account_id,account_epoch,native_fill_id_hash,order_id,canonical_fill,occurred_at
) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT DO NOTHING`,
		event.AccountID, event.AccountEpoch, event.NativeFillHash,
		event.OrderID.String(), string(payload), event.OccurredAt)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_fill_post_failed")
	}
	if tag.RowsAffected() == 0 && !sameSandboxRuntimeFill(ctx, tx, event, payload) {
		return fmt.Errorf("sandbox_runtime_fill_identity_conflict")
	}
	return kill.Hit(ctx, sandbox.KillAfterFillPosting)
}

func sameSandboxRuntimeFill(
	ctx context.Context,
	tx pgx.Tx,
	event sandbox.PrivateEvent,
	payload []byte,
) bool {
	var same bool
	err := tx.QueryRow(ctx, `
SELECT account_epoch=$2 AND order_id=$4 AND canonical_fill=$5::jsonb AND occurred_at=$6
FROM sandbox_runtime_exchange_fills
WHERE account_id=$1 AND native_fill_id_hash=$3`,
		event.AccountID, event.AccountEpoch, event.NativeFillHash,
		event.OrderID.String(), string(payload), event.OccurredAt).Scan(&same)
	return err == nil && same
}

func closeSandboxRuntimeReservation(
	ctx context.Context,
	tx pgx.Tx,
	event sandbox.PrivateEvent,
	state string,
	consumed bool,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReservationRelease); err != nil {
		return err
	}
	reservationState := "RELEASED"
	if consumed {
		reservationState = "CONSUMED"
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_reservations
SET state=$4,released_at=$2,release_reason=$3
WHERE order_id=$1 AND state='ACTIVE'`,
		event.OrderID.String(), event.ReceivedAt, state, reservationState); err != nil {
		return fmt.Errorf("sandbox_runtime_reservation_release_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReservationRelease)
}

func updateSandboxRuntimePlanState(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
) error {
	legs, terminal, filled, recovery, err := readSandboxRuntimePlanState(ctx, tx, planID)
	if err != nil || legs == 0 {
		return fmt.Errorf("sandbox_runtime_plan_state_read_failed")
	}
	next, disposition := deriveSandboxRuntimePlanState(legs, terminal, filled, recovery)
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_plans
SET state=$2,final_disposition=nullif($3,''),saga_revision=saga_revision+1,
    revision=revision+1
WHERE id=$1 AND (state<>$2 OR coalesce(final_disposition,'')<>$3)`,
		planID, next, disposition); err != nil {
		return fmt.Errorf("sandbox_runtime_plan_state_update_failed")
	}
	return nil
}

func readSandboxRuntimePlanState(
	ctx context.Context,
	tx pgx.Tx,
	planID string,
) (int, int, int, bool, error) {
	var legs, terminal, filled int
	var recovery bool
	err := tx.QueryRow(ctx, `
SELECT count(*),
       count(*) FILTER (WHERE state='TERMINAL'),
       count(*) FILTER (WHERE order_state='FILLED'),
       coalesce(bool_or(
         state='UNKNOWN' OR order_state IN ('PARTIALLY_FILLED','RECOVERY_REQUIRED')
       ),false)
FROM sandbox_runtime_submission_outbox
WHERE plan_id=$1`, planID).Scan(&legs, &terminal, &filled, &recovery)
	return legs, terminal, filled, recovery, err
}

func deriveSandboxRuntimePlanState(
	legs, terminal, filled int,
	recovery bool,
) (string, string) {
	switch {
	case recovery || (terminal == legs && filled > 0 && filled < legs):
		return "RECOVERY_REQUIRED", ""
	case terminal == legs && filled == legs:
		return "COMPLETED", "all_legs_filled"
	case terminal == legs:
		return "FAILED", "no_leg_filled"
	default:
		return "ACTIVE", ""
	}
}

func sandboxRuntimeOrderState(state execution.OrderState) (string, bool) {
	switch state {
	case execution.OrderAcknowledged:
		return "ACKNOWLEDGED", false
	case execution.OrderPartiallyFilled:
		return "PARTIALLY_FILLED", false
	case execution.OrderCancelPending:
		return "CANCEL_PENDING", false
	case execution.OrderFilled:
		return "FILLED", true
	case execution.OrderCanceled:
		return "CANCELED", false
	case execution.OrderRejected:
		return "REJECTED", false
	case execution.OrderExpired:
		return "EXPIRED", false
	case execution.OrderUnknown:
		return "UNKNOWN", false
	case execution.OrderRecoveryRequired, execution.OrderRecovered:
		return "RECOVERY_REQUIRED", false
	default:
		return "RECOVERY_REQUIRED", false
	}
}
