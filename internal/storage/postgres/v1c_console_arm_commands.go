package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

const (
	c6ArmForUpdateSQL = `
SELECT sandbox_session_id,actor_user_id,revision,revoked_at
FROM v1c_sandbox_arms WHERE id=$1 FOR UPDATE`
	c6RevokeArmSQL = `
UPDATE v1c_sandbox_arms
SET revoked_at=$2,revision=revision+1 WHERE id=$1`
	c6PauseArmSessionSQL = `
UPDATE v1c_sandbox_sessions
SET state='READY_PAUSED',revision=revision+1,updated_at=$2
WHERE id=$1 AND state='ARMED'`
	c6PauseArmAccountsSQL = `
UPDATE v1c_exchange_accounts account
SET state='READY_PAUSED',revision=account.revision+1,updated_at=$2
FROM v1c_sandbox_session_accounts membership
WHERE membership.session_id=$1 AND membership.account_id=account.id
  AND membership.account_epoch=account.current_epoch
  AND account.state='ARMED'`
	c6ReconciliationEpochSQL = `
SELECT account_epoch FROM v1c_reconciliations
WHERE id=$1 AND account_id=$2`
)

// CreateSandboxArm reuses the C2 arm state machine and adds API command
// idempotency. It never owns an exchange adapter or credential.
func (store *A11ConsoleStore) CreateSandboxArm(
	ctx context.Context,
	principal authentication.Principal,
	sessionID, key string,
	body generated.SandboxArmRequest,
	consumed authentication.ConsumedAuthorization,
) (generated.SandboxArm, error) {
	expected, err := c6ArmExpectedRevision(principal, consumed, body)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	pending, err := store.beginC6Command(
		ctx, principal, key, "sandbox.arm", "sandbox_session", sessionID,
		body.Reason, expected,
		c6ArmPayload(sessionID, body, consumed),
	)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	if !pending.created {
		return generated.SandboxArm{}, console.ErrConflict
	}
	now := store.clock.Now().UTC
	armID := c6StableIdentifier("arm", pending.accepted.Id)
	arm, err := store.v1c.CreateSandboxArm(
		ctx,
		c6ArmCommand(
			principal, sessionID, armID, body.AccountIds, consumed,
			uint64(*expected), now,
		),
	)
	if err != nil {
		store.rejectC6Command(ctx, pending, "sandbox_arm_rejected")
		return generated.SandboxArm{}, c6ConsoleError(err)
	}
	result := c6GeneratedArm(arm, body.AccountIds)
	resultMap, err := c6ResultMap(result)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	if _, err = store.completeC6Command(
		ctx, pending, principal, "sandbox.arm", arm.ID, resultMap,
	); err != nil {
		return generated.SandboxArm{}, err
	}
	return result, nil
}

func c6ArmPayload(
	sessionID string,
	body generated.SandboxArmRequest,
	consumed authentication.ConsumedAuthorization,
) map[string]any {
	return map[string]any{
		"session_id": sessionID, "account_ids": body.AccountIds,
		"expected_revision": body.ExpectedRevision,
		"authorization_id":  consumed.ID, "reason": body.Reason,
	}
}

func c6ArmExpectedRevision(
	principal authentication.Principal,
	consumed authentication.ConsumedAuthorization,
	body generated.SandboxArmRequest,
) (*int64, error) {
	expected, err := c6ExpectedRevision(body.ExpectedRevision)
	if err != nil || validateC6Consumed(
		principal, consumed, authentication.PurposeSandboxArm, body.Reason,
	) != nil {
		return nil, console.ErrPrecondition
	}
	return expected, nil
}

func c6ArmCommand(
	principal authentication.Principal,
	sessionID, armID string,
	accountIDs []string,
	consumed authentication.ConsumedAuthorization,
	expectedRevision uint64,
	now time.Time,
) sandbox.ArmCommand {
	accounts := make([]sandbox.AccountID, len(accountIDs))
	for index, accountID := range accountIDs {
		accounts[index] = sandbox.AccountID(accountID)
	}
	return sandbox.ArmCommand{
		Arm: sandbox.Arm{
			ID: armID, SessionID: sandbox.SessionID(sessionID),
			AccountIDs: accounts, AuthorizationHash: stableV1CHash(consumed.ID),
			ActorUserID: principal.UserID, ActorSessionID: principal.SessionID,
			ReasonHash: consumed.ReasonHash, CreatedAt: now,
			ExpiresAt: now.Add(sandbox.ArmLifetime), Revision: 1,
		},
		AuthorizationID: consumed.ID, SourceHash: consumed.SourceHash,
		ExpectedSessionRevision: expectedRevision,
	}
}

func c6GeneratedArm(arm sandbox.Arm, accountIDs []string) generated.SandboxArm {
	return generated.SandboxArm{
		Id: arm.ID, SessionId: string(arm.SessionID), AccountIds: accountIDs,
		State: generated.SandboxArmStateActive, CreatedAt: arm.CreatedAt,
		ExpiresAt: arm.ExpiresAt, Revision: strconv.FormatUint(arm.Revision, 10),
		AuditUrl: "/api/v1/audit-events?event_type=sandbox_arm",
	}
}

// RevokeSandboxArm atomically closes the arm and returns the session/accounts
// to READY_PAUSED without affecting cancel/query/reconciliation capability.
func (store *A11ConsoleStore) RevokeSandboxArm(
	ctx context.Context,
	principal authentication.Principal,
	armID, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	expected, err := c6ExpectedRevision(body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginC6Command(
		ctx, principal, key, "sandbox.arm_revoke", "sandbox_arm", armID,
		body.Reason, expected,
		map[string]any{"arm_id": armID, "body": body},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	if err = store.revokeC6ArmState(
		ctx, principal, armID, *expected, pending,
	); err != nil {
		return generated.CommandAccepted{}, err
	}
	return store.completeC6Command(
		ctx, pending, principal, "sandbox.arm_revoke", armID,
		map[string]any{"arm_id": armID, "state": "revoked"},
	)
}

func (store *A11ConsoleStore) revokeC6ArmState(
	ctx context.Context,
	principal authentication.Principal,
	armID string,
	expected int64,
	pending c6PendingCommand,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var sessionID, actor string
	var revision int64
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, c6ArmForUpdateSQL, armID).Scan(
		&sessionID, &actor, &revision, &revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		store.rejectC6Command(ctx, pending, "sandbox_arm_not_found")
		return console.ErrNotFound
	}
	if err != nil || actor != principal.UserID || revision != expected ||
		revokedAt != nil {
		store.rejectC6Command(ctx, pending, "sandbox_arm_revision_conflict")
		return console.ErrConflict
	}
	now := store.clock.Now().UTC
	if _, err = tx.Exec(ctx, c6RevokeArmSQL, armID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, c6PauseArmSessionSQL, sessionID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, c6PauseArmAccountsSQL, sessionID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UnlockSandboxAccount reuses the reconciliation-bound C2 risk unlock.
func (store *A11ConsoleStore) UnlockSandboxAccount(
	ctx context.Context,
	principal authentication.Principal,
	accountID, key string,
	body generated.SandboxUnlockRequest,
	consumed authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	expected, err := c6ExpectedRevision(body.ExpectedRevision)
	if err != nil || validateC6Consumed(
		principal, consumed, authentication.PurposeRiskUnlock, body.Reason,
	) != nil {
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	epoch, err := store.c6ReconciliationEpoch(
		ctx, body.ReconciliationId, accountID,
	)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginC6Command(
		ctx, principal, key, "sandbox.risk_unlock", "sandbox_account",
		accountID, body.Reason, expected,
		map[string]any{
			"account_id": accountID, "reconciliation_id": body.ReconciliationId,
			"expected_revision": body.ExpectedRevision,
			"authorization_id":  consumed.ID, "reason": body.Reason,
		},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	err = store.v1c.RiskUnlock(ctx, sandbox.RiskUnlockCommand{
		ID:               c6StableIdentifier("unlock", pending.accepted.Id),
		AccountID:        sandbox.AccountID(accountID),
		ExpectedRevision: uint64(*expected), AuthorizationID: consumed.ID,
		ActorUserID: principal.UserID, ActorSessionID: principal.SessionID,
		SourceHash: consumed.SourceHash, ReasonHash: consumed.ReasonHash,
		ReconciliationID:    body.ReconciliationId,
		ReconciliationEpoch: uint64(epoch), Now: store.clock.Now().UTC,
	})
	if err != nil {
		store.rejectC6Command(ctx, pending, "sandbox_risk_unlock_rejected")
		return generated.CommandAccepted{}, c6ConsoleError(err)
	}
	return store.completeC6Command(
		ctx, pending, principal, "sandbox.risk_unlock", accountID,
		map[string]any{"account_id": accountID, "state": "READY_PAUSED"},
	)
}

func (store *A11ConsoleStore) c6ReconciliationEpoch(
	ctx context.Context,
	reconciliationID, accountID string,
) (int64, error) {
	var epoch int64
	err := store.pool.QueryRow(
		ctx, c6ReconciliationEpochSQL, reconciliationID, accountID,
	).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, console.ErrPrecondition
	}
	return epoch, err
}
