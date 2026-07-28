package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

func validateV1CPlanReferences(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
) error {
	entry, err := sandbox.RequiresEntryArm(plan)
	if err != nil {
		return fmt.Errorf("v1c_plan_action_rejected")
	}
	actorUser, actorSession, err := validateV1CSessionAndArm(ctx, tx, plan, entry)
	if err != nil {
		return err
	}
	if entry && validateV1CArmActor(ctx, tx, actorUser, actorSession, plan.ApprovedAt) != nil {
		return fmt.Errorf("v1c_arm_session_invalid")
	}
	exchanges, err := validateV1CPlanAccounts(ctx, tx, plan, entry)
	if err != nil {
		return err
	}
	if err = sandbox.ValidateSubmissionTopology(plan.Submissions, exchanges); err != nil {
		return fmt.Errorf("v1c_plan_topology_rejected")
	}
	return nil
}

func validateV1CSessionAndArm(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	entry bool,
) (string, string, error) {
	var sessionState string
	if err := tx.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_sessions WHERE id=$1 FOR UPDATE`, plan.SessionID).Scan(&sessionState); err != nil ||
		sessionState == "STOPPED" || (entry && sessionState != "ARMED") {
		return "", "", fmt.Errorf("v1c_session_not_ready")
	}
	var armCreated, armExpires time.Time
	var revoked *time.Time
	var actorUser, actorSession, reasonHash string
	var armRevision int64
	if err := tx.QueryRow(ctx, `
SELECT created_at,expires_at,revoked_at,actor_user_id,actor_session_id,
       reason_hash,revision
FROM v1c_sandbox_arms
WHERE id=$1 AND sandbox_session_id=$2 FOR UPDATE`,
		plan.Arm.ID,
		plan.SessionID,
	).Scan(
		&armCreated,
		&armExpires,
		&revoked,
		&actorUser,
		&actorSession,
		&reasonHash,
		&armRevision,
	); err != nil ||
		!armCreated.Equal(plan.Arm.CreatedAt) ||
		!armExpires.Equal(plan.Arm.ExpiresAt) ||
		actorUser != plan.Arm.ActorUserID ||
		actorSession != plan.Arm.ActorSessionID ||
		reasonHash != plan.Arm.ReasonHash ||
		uint64(armRevision) != plan.Arm.Revision ||
		(entry && (revoked != nil || !plan.ApprovedAt.Before(armExpires))) {
		return "", "", fmt.Errorf("v1c_arm_not_active")
	}
	return actorUser, actorSession, nil
}

func validateV1CArmActor(
	ctx context.Context,
	tx pgx.Tx,
	actorUser, actorSession string,
	at time.Time,
) error {
	var actorActive bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1
  FROM sessions session
  JOIN users actor ON actor.id=session.user_id
  WHERE session.id=$1 AND session.user_id=$2
    AND actor.status='active'
    AND session.revoked_at IS NULL
    AND session.expires_at>$3
    AND session.idle_expires_at>$3
	)`, actorSession, actorUser, at).Scan(&actorActive); err != nil || !actorActive {
		return fmt.Errorf("v1c_arm_session_invalid")
	}
	return nil
}

func validateV1CPlanAccounts(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	entry bool,
) (map[sandbox.AccountID]sandbox.Exchange, error) {
	exchanges := make(map[sandbox.AccountID]sandbox.Exchange, len(plan.Submissions))
	for _, submission := range plan.Submissions {
		exchange, err := validateV1CPlanAccount(ctx, tx, plan, submission, entry)
		if err != nil {
			return nil, err
		}
		exchanges[submission.AccountID] = exchange
	}
	return exchanges, nil
}

func validateV1CPlanAccount(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
	entry bool,
) (sandbox.Exchange, error) {
	var exchange, accountState string
	var epoch int64
	if err := tx.QueryRow(ctx, `
SELECT account.exchange,account.current_epoch,account.state
FROM v1c_exchange_accounts account
JOIN v1c_sandbox_session_accounts membership
  ON membership.account_id=account.id AND membership.account_epoch=account.current_epoch
WHERE account.id=$1 AND membership.session_id=$2 AND membership.account_epoch=$3
FOR SHARE OF account,membership`,
		submission.AccountID, plan.SessionID, submission.AccountEpoch,
	).Scan(&exchange, &epoch, &accountState); err != nil ||
		uint64(epoch) != submission.AccountEpoch ||
		(entry && accountState != "ARMED") {
		return "", fmt.Errorf("v1c_account_epoch_rejected")
	}
	exchangeName := sandbox.Exchange(exchange)
	if entry {
		if err := validateV1CAccountEntry(ctx, tx, plan, submission, exchangeName); err != nil {
			return "", err
		}
	}
	return exchangeName, nil
}

func validateV1CAccountEntry(
	ctx context.Context,
	tx pgx.Tx,
	plan sandbox.ApprovedSandboxPlan,
	submission sandbox.Submission,
	exchange sandbox.Exchange,
) error {
	snapshot, exists := plan.Eligibility[exchange]
	if !exists || !snapshot.Eligible || snapshot.Exchange != string(exchange) ||
		snapshot.Instrument != submission.Instrument.Symbol() ||
		snapshot.ObservedAt.IsZero() ||
		snapshot.ObservedAt.Location() != time.UTC ||
		snapshot.ObservedAt.After(plan.ApprovedAt) ||
		plan.ApprovedAt.Sub(snapshot.ObservedAt) > 250*time.Millisecond {
		return fmt.Errorf("v1c_public_ineligible")
	}
	safety, exists := plan.EntrySafety[submission.AccountID]
	if !exists || safety.ValidateFor(submission, exchange, plan.ApprovedAt) != nil {
		return fmt.Errorf("v1c_entry_safety_rejected")
	}
	var leaseHeld bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS(
  SELECT 1 FROM v1c_account_leases
  WHERE account_id=$1 AND environment=$2 AND expires_at>$3
)`,
		submission.AccountID,
		environmentForV1CExchange(exchange),
		plan.ApprovedAt,
	).Scan(&leaseHeld); err != nil || !leaseHeld {
		return fmt.Errorf("v1c_entry_lease_rejected")
	}
	return nil
}

func environmentForV1CExchange(exchange sandbox.Exchange) sandbox.Environment {
	if exchange == sandbox.ExchangeBinance {
		return sandbox.EnvironmentBinanceSpotTestnet
	}
	return sandbox.EnvironmentBybitDemo
}
