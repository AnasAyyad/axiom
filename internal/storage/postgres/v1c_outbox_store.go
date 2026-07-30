package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const selectV1CClaimSQL = `
SELECT outbox.id FROM v1c_submission_outbox outbox
WHERE outbox.account_id=$1 AND outbox.account_epoch=$2 AND (
  outbox.state='PENDING' OR
  (outbox.state='CLAIMED' AND outbox.claim_expires_at<=$3 AND outbox.fencing_token<$4)
) AND (
  outbox.intent_action<>'ENTRY' OR EXISTS (
    SELECT 1
    FROM v1c_submission_plans plan
    JOIN v1c_sandbox_arms arm ON arm.id=plan.arm_id
    JOIN v1c_sandbox_sessions sandbox_session
      ON sandbox_session.id=plan.sandbox_session_id
    JOIN v1c_exchange_accounts account ON account.id=outbox.account_id
    JOIN v1c_plan_entry_safety safety
      ON safety.plan_id=plan.id
     AND safety.account_id=outbox.account_id
     AND safety.account_epoch=outbox.account_epoch
    JOIN sessions actor_session ON actor_session.id=arm.actor_session_id
    JOIN users actor ON actor.id=actor_session.user_id
    WHERE plan.id=outbox.plan_id
      AND arm.revoked_at IS NULL
      AND arm.expires_at>$3
      AND sandbox_session.state='ARMED'
      AND account.state='ARMED'
      AND account.current_epoch=outbox.account_epoch
      AND actor.id=arm.actor_user_id
      AND actor.status='active'
      AND actor_session.revoked_at IS NULL
      AND actor_session.expires_at>$3
      AND actor_session.idle_expires_at>$3
  )
)
ORDER BY outbox.approved_at,outbox.id
FOR UPDATE OF outbox SKIP LOCKED LIMIT $5`

const markV1CSubmittingSQL = `
UPDATE v1c_submission_outbox outbox
SET order_state='SUBMITTING',updated_at=$3
WHERE outbox.id=$1 AND outbox.state='CLAIMED' AND outbox.fencing_token=$2
  AND outbox.order_state IN ('APPROVED','SUBMITTING')
  AND (
    outbox.intent_action<>'ENTRY' OR EXISTS (
      SELECT 1
      FROM v1c_submission_plans plan
      JOIN v1c_sandbox_arms arm ON arm.id=plan.arm_id
      JOIN v1c_sandbox_sessions sandbox_session
        ON sandbox_session.id=plan.sandbox_session_id
      JOIN v1c_exchange_accounts account ON account.id=outbox.account_id
      JOIN v1c_plan_entry_safety safety
        ON safety.plan_id=plan.id
       AND safety.account_id=outbox.account_id
       AND safety.account_epoch=outbox.account_epoch
      JOIN sessions actor_session ON actor_session.id=arm.actor_session_id
      JOIN users actor ON actor.id=actor_session.user_id
      WHERE plan.id=outbox.plan_id
        AND arm.revoked_at IS NULL
        AND arm.expires_at>$3
        AND sandbox_session.state='ARMED'
        AND account.state='ARMED'
        AND account.current_epoch=outbox.account_epoch
        AND actor.id=arm.actor_user_id
        AND actor.status='active'
        AND actor_session.revoked_at IS NULL
        AND actor_session.expires_at>$3
        AND actor_session.idle_expires_at>$3
    )
  )
RETURNING plan_id`

const markV1CCancelPendingSQL = `
UPDATE v1c_submission_outbox outbox
SET order_state='CANCEL_PENDING',
    fencing_token=GREATEST(coalesce(outbox.fencing_token,0),$5),
    updated_at=$6
WHERE outbox.account_id=$1
  AND outbox.account_epoch=$2
  AND outbox.client_order_id=$3
  AND outbox.state IN ('ACKNOWLEDGED','UNKNOWN')
  AND outbox.order_state IN (
    'ACKNOWLEDGED','PARTIALLY_FILLED','CANCEL_PENDING','UNKNOWN','RECOVERY_REQUIRED'
  )
  AND EXISTS (
    SELECT 1
    FROM v1c_account_leases lease
    JOIN v1c_exchange_accounts exchange_account
      ON exchange_account.id=lease.account_id
     AND exchange_account.environment=lease.environment
    WHERE lease.account_id=$1
      AND lease.owner=$4
      AND lease.fencing_token=$5
      AND lease.expires_at>$6
  )
RETURNING outbox.id`

// ClaimOutbox claims a bounded account page under the active fencing lease.
func (store *V1CDispatcherStore) ClaimOutbox(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	ttl time.Duration,
	limit int,
	kill sandbox.KillPoint,
) ([]sandbox.SubmissionOutbox, error) {
	if err := kill.Hit(ctx, sandbox.KillBeforeLeaseTransition); err != nil {
		return nil, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("v1c_claim_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	ids, err := selectV1CClaimIDs(ctx, tx, account, epoch, owner, fence, now, limit)
	if err != nil {
		return nil, err
	}
	result, err := claimV1COutboxIDs(ctx, tx, ids, owner, fence, now, ttl)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("v1c_claim_commit_failed")
	}
	if err = kill.Hit(ctx, sandbox.KillAfterLeaseTransition); err != nil {
		return nil, err
	}
	return result, nil
}

func selectV1CClaimIDs(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	epoch uint64,
	owner string,
	fence uint64,
	now time.Time,
	limit int,
) ([]string, error) {
	var leaseOK bool
	if err := tx.QueryRow(ctx, `
SELECT owner=$2 AND fencing_token=$3 AND expires_at>$4
FROM v1c_account_leases WHERE account_id=$1 FOR UPDATE`,
		account, owner, fence, now).Scan(&leaseOK); err != nil || !leaseOK {
		return nil, fmt.Errorf("v1c_stale_fencing_token")
	}
	rows, err := tx.Query(ctx, selectV1CClaimSQL, account, epoch, now, fence, limit)
	if err != nil {
		return nil, fmt.Errorf("v1c_claim_select_failed")
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("v1c_claim_scan_failed")
		}
		ids = append(ids, id)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, fmt.Errorf("v1c_claim_scan_failed")
	}
	return ids, nil
}

func claimV1COutboxIDs(
	ctx context.Context,
	tx pgx.Tx,
	ids []string,
	owner string,
	fence uint64,
	now time.Time,
	ttl time.Duration,
) ([]sandbox.SubmissionOutbox, error) {
	result := make([]sandbox.SubmissionOutbox, 0, len(ids))
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `
UPDATE v1c_submission_outbox
SET state='CLAIMED',claim_owner=$2,fencing_token=$3,claim_expires_at=$4,
    attempt=attempt+1,updated_at=$5
WHERE id=$1`, id, owner, fence, now.Add(ttl), now); err != nil {
			return nil, fmt.Errorf("v1c_claim_update_failed")
		}
		record, readErr := readV1COutbox(ctx, tx, id)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, record)
	}
	return result, nil
}

type v1CQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readV1COutbox(ctx context.Context, tx v1CQueryRower, id string) (sandbox.SubmissionOutbox, error) {
	var record sandbox.SubmissionOutbox
	var planID, orderID, strategyID, instrument, side string
	var quantity, price, notional string
	var style, action string
	var claimOwner *string
	var fencingToken *int64
	var claimExpiresAt *time.Time
	err := tx.QueryRow(ctx, `
SELECT id,plan_id,account_id,account_epoch,order_id,client_order_id,strategy_id,
 instrument,side,quantity::text,limit_price::text,reserved_notional::text,
 order_style,intent_action,request_hash,policy_hash,approved_at,
 state,claim_owner,fencing_token,claim_expires_at,attempt,updated_at
FROM v1c_submission_outbox WHERE id=$1`, id).Scan(
		&record.ID, &planID, &record.Submission.AccountID, &record.Submission.AccountEpoch,
		&orderID, &record.Submission.ClientOrderID, &strategyID, &instrument, &side,
		&quantity, &price, &notional, &style, &action, &record.Submission.RequestHash,
		&record.Submission.PolicyHash, &record.Submission.ApprovedAt, &record.State,
		&claimOwner, &fencingToken, &claimExpiresAt, &record.Attempt, &record.UpdatedAt)
	if err != nil {
		return sandbox.SubmissionOutbox{}, fmt.Errorf("v1c_outbox_read_failed")
	}
	return decodeV1COutbox(
		record, planID, orderID, strategyID, instrument, side,
		quantity, price, notional, sandbox.OrderStyle(style), sandbox.IntentAction(action),
		claimOwner, fencingToken, claimExpiresAt,
	)
}

func decodeV1COutbox(
	record sandbox.SubmissionOutbox,
	planID, orderID, strategyID, instrument, side string,
	quantity, price, notional string,
	style sandbox.OrderStyle,
	action sandbox.IntentAction,
	claimOwner *string,
	fencingToken *int64,
	claimExpiresAt *time.Time,
) (sandbox.SubmissionOutbox, error) {
	var err error
	record.Submission.ApprovedAt = record.Submission.ApprovedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	setV1CClaimMetadata(&record, claimOwner, fencingToken, claimExpiresAt)
	if err = record.Submission.PlanID.UnmarshalText([]byte(planID)); err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	if err = record.Submission.OrderID.UnmarshalText([]byte(orderID)); err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	if err = record.Submission.StrategyID.UnmarshalText([]byte(strategyID)); err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	base, quote, ok := splitInstrument(instrument)
	if !ok {
		return sandbox.SubmissionOutbox{}, fmt.Errorf("v1c_instrument_invalid")
	}
	record.Submission.Instrument, err = domain.NewSpotInstrument(base, quote)
	if err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	record.Submission.Side = domain.Side(side)
	record.Submission.Quantity, err = domain.ParseQuantity(quantity)
	if err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	record.Submission.LimitPrice, err = domain.ParsePrice(price)
	if err != nil {
		return sandbox.SubmissionOutbox{}, err
	}
	record.Submission.Notional, err = domain.ParseNotional(notional)
	record.Submission.Style, record.Submission.Action = style, action
	return record, err
}

func setV1CClaimMetadata(
	record *sandbox.SubmissionOutbox,
	claimOwner *string,
	fencingToken *int64,
	claimExpiresAt *time.Time,
) {
	if claimOwner != nil {
		record.ClaimOwner = *claimOwner
	}
	if fencingToken != nil {
		record.FencingToken = uint64(*fencingToken)
	}
	if claimExpiresAt != nil {
		record.ClaimExpiresAt = claimExpiresAt.UTC()
	}
}

func splitInstrument(value string) (domain.AssetSymbol, domain.AssetSymbol, bool) {
	for _, quote := range []string{"USDT", "BTC"} {
		if len(value) > len(quote) && value[len(value)-len(quote):] == quote {
			base, leftErr := domain.ParseAssetSymbol(value[:len(value)-len(quote)])
			quoted, rightErr := domain.ParseAssetSymbol(quote)
			return base, quoted, leftErr == nil && rightErr == nil
		}
	}
	return "", "", false
}

// MarkSubmitting records the pre-network canonical transition.
func (store *V1CDispatcherStore) MarkSubmitting(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_submitting_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var planID string
	err = tx.QueryRow(ctx, markV1CSubmittingSQL, id, fence, now).Scan(&planID)
	if err != nil {
		return fmt.Errorf("v1c_stale_fencing_token")
	}
	if err = updateV1CPlanState(ctx, tx, planID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_submitting_commit_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReducerUpdate)
}

// MarkUnknown quarantines an ambiguous result without releasing capacity.
func (store *V1CDispatcherStore) MarkUnknown(
	ctx context.Context,
	id string,
	fence uint64,
	now time.Time,
	kill sandbox.KillPoint,
) error {
	if err := kill.Hit(ctx, sandbox.KillBeforeReducerUpdate); err != nil {
		return err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("v1c_unknown_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var planID string
	err = tx.QueryRow(ctx, `
UPDATE v1c_submission_outbox
SET state='UNKNOWN',order_state='UNKNOWN',updated_at=$3
WHERE id=$1 AND state='CLAIMED' AND fencing_token=$2
RETURNING plan_id`, id, fence, now).Scan(&planID)
	if err != nil {
		return fmt.Errorf("v1c_unknown_transition_failed")
	}
	if err = updateV1CPlanState(ctx, tx, planID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("v1c_unknown_commit_failed")
	}
	return kill.Hit(ctx, sandbox.KillAfterReducerUpdate)
}
