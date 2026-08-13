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
	sandboxQualificationArmForUpdateSQL = `
SELECT sandbox_session_id,actor_user_id,revision,revoked_at
FROM sandbox_runtime_sandbox_arms WHERE id=$1 FOR UPDATE`
	sandboxQualificationRevokeArmSQL = `
UPDATE sandbox_runtime_sandbox_arms
SET revoked_at=$2,revision=revision+1 WHERE id=$1`
	sandboxQualificationPauseArmSessionSQL = `
UPDATE sandbox_runtime_sandbox_sessions
SET state='READY_PAUSED',revision=revision+1,updated_at=$2
WHERE id=$1 AND state='ARMED'`
	sandboxQualificationPauseArmAccountsSQL = `
UPDATE sandbox_runtime_exchange_accounts account
SET state='READY_PAUSED',revision=account.revision+1,updated_at=$2
FROM sandbox_runtime_sandbox_session_accounts membership
WHERE membership.session_id=$1 AND membership.account_id=account.id
  AND membership.account_epoch=account.current_epoch
  AND account.state='ARMED'`
	sandboxQualificationReconciliationEpochSQL = `
SELECT account_epoch FROM sandbox_runtime_reconciliations
WHERE id=$1 AND account_id=$2`
)

// CreateSandboxArm reuses the authentication control arm state machine and adds API command
// idempotency. It never owns an exchange adapter or credential.
func (store *OwnerConsoleStore) CreateSandboxArm(
	ctx context.Context,
	principal authentication.Principal,
	sessionID, key string,
	body generated.SandboxArmRequest,
	consumed authentication.ConsumedAuthorization,
) (generated.SandboxArm, error) {
	expected, err := sandboxQualificationArmExpectedRevision(principal, consumed, body)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.arm", "sandbox_session", sessionID,
		body.Reason, expected,
		sandboxQualificationArmPayload(sessionID, body, consumed),
	)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	if !pending.created {
		return generated.SandboxArm{}, console.ErrConflict
	}
	now := store.clock.Now().UTC
	armID := sandboxQualificationStableIdentifier("arm", pending.accepted.Id)
	arm, err := store.sandbox_runtime.CreateSandboxArm(
		ctx,
		sandboxQualificationArmCommand(
			principal, sessionID, armID, body.AccountIds, consumed,
			uint64(*expected), now,
		),
	)
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_arm_rejected")
		return generated.SandboxArm{}, sandboxQualificationConsoleError(err)
	}
	result := sandboxQualificationGeneratedArm(arm, body.AccountIds)
	resultMap, err := sandboxQualificationResultMap(result)
	if err != nil {
		return generated.SandboxArm{}, err
	}
	if _, err = store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.arm", arm.ID, resultMap,
	); err != nil {
		return generated.SandboxArm{}, err
	}
	return result, nil
}

func sandboxQualificationArmPayload(
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

func sandboxQualificationArmExpectedRevision(
	principal authentication.Principal,
	consumed authentication.ConsumedAuthorization,
	body generated.SandboxArmRequest,
) (*int64, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil || validateSandboxQualificationConsumed(
		principal, consumed, authentication.PurposeSandboxArm, body.Reason,
	) != nil {
		return nil, console.ErrPrecondition
	}
	return expected, nil
}

func sandboxQualificationArmCommand(
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
			AccountIDs: accounts, AuthorizationHash: stableSandboxRuntimeHash(consumed.ID),
			ActorUserID: principal.UserID, ActorSessionID: principal.SessionID,
			ReasonHash: consumed.ReasonHash, CreatedAt: now,
			ExpiresAt: now.Add(sandbox.ArmLifetime), Revision: 1,
		},
		AuthorizationID: consumed.ID, SourceHash: consumed.SourceHash,
		ExpectedSessionRevision: expectedRevision,
	}
}

func sandboxQualificationGeneratedArm(arm sandbox.Arm, accountIDs []string) generated.SandboxArm {
	return generated.SandboxArm{
		Id: arm.ID, SessionId: string(arm.SessionID), AccountIds: accountIDs,
		State: generated.SandboxArmStateActive, CreatedAt: arm.CreatedAt,
		ExpiresAt: arm.ExpiresAt, Revision: strconv.FormatUint(arm.Revision, 10),
		AuditUrl: "/api/v1/audit-events?event_type=sandbox_arm",
	}
}

// RevokeSandboxArm atomically closes the arm and returns the session/accounts
// to READY_PAUSED without affecting cancel/query/reconciliation capability.
func (store *OwnerConsoleStore) RevokeSandboxArm(
	ctx context.Context,
	principal authentication.Principal,
	armID, key string,
	body generated.RevisionCommandRequest,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(
		ctx, principal, key, "sandbox.arm_revoke", "sandbox_arm", armID,
		body.Reason, expected,
		map[string]any{"arm_id": armID, "body": body},
	)
	if err != nil || !pending.created {
		return pending.accepted, err
	}
	if err = store.revokeSandboxQualificationArmState(
		ctx, principal, armID, *expected, pending,
	); err != nil {
		return generated.CommandAccepted{}, err
	}
	return store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.arm_revoke", armID,
		map[string]any{"arm_id": armID, "state": "revoked"},
	)
}

func (store *OwnerConsoleStore) revokeSandboxQualificationArmState(
	ctx context.Context,
	principal authentication.Principal,
	armID string,
	expected int64,
	pending sandboxQualificationPendingCommand,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var sessionID, actor string
	var revision int64
	var revokedAt *time.Time
	err = tx.QueryRow(ctx, sandboxQualificationArmForUpdateSQL, armID).Scan(
		&sessionID, &actor, &revision, &revokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_arm_not_found")
		return console.ErrNotFound
	}
	if err != nil || actor != principal.UserID || revision != expected ||
		revokedAt != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_arm_revision_conflict")
		return console.ErrConflict
	}
	now := store.clock.Now().UTC
	if _, err = tx.Exec(ctx, sandboxQualificationRevokeArmSQL, armID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, sandboxQualificationPauseArmSessionSQL, sessionID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, sandboxQualificationPauseArmAccountsSQL, sessionID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UnlockSandboxAccount reuses the reconciliation-bound authentication control risk unlock.
func (store *OwnerConsoleStore) UnlockSandboxAccount(
	ctx context.Context,
	principal authentication.Principal,
	accountID, key string,
	body generated.SandboxUnlockRequest,
	consumed authentication.ConsumedAuthorization,
) (generated.CommandAccepted, error) {
	expected, err := sandboxQualificationExpectedRevision(body.ExpectedRevision)
	if err != nil || validateSandboxQualificationConsumed(
		principal, consumed, authentication.PurposeRiskUnlock, body.Reason,
	) != nil {
		return generated.CommandAccepted{}, console.ErrPrecondition
	}
	epoch, err := store.sandboxQualificationReconciliationEpoch(
		ctx, body.ReconciliationId, accountID,
	)
	if err != nil {
		return generated.CommandAccepted{}, err
	}
	pending, err := store.beginSandboxQualificationCommand(
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
	err = store.sandbox_runtime.RiskUnlock(ctx, sandbox.RiskUnlockCommand{
		ID:               sandboxQualificationStableIdentifier("unlock", pending.accepted.Id),
		AccountID:        sandbox.AccountID(accountID),
		ExpectedRevision: uint64(*expected), AuthorizationID: consumed.ID,
		ActorUserID: principal.UserID, ActorSessionID: principal.SessionID,
		SourceHash: consumed.SourceHash, ReasonHash: consumed.ReasonHash,
		ReconciliationID:    body.ReconciliationId,
		ReconciliationEpoch: uint64(epoch), Now: store.clock.Now().UTC,
	})
	if err != nil {
		store.rejectSandboxQualificationCommand(ctx, pending, "sandbox_risk_unlock_rejected")
		return generated.CommandAccepted{}, sandboxQualificationConsoleError(err)
	}
	return store.completeSandboxQualificationCommand(
		ctx, pending, principal, "sandbox.risk_unlock", accountID,
		map[string]any{"account_id": accountID, "state": "READY_PAUSED"},
	)
}

func (store *OwnerConsoleStore) sandboxQualificationReconciliationEpoch(
	ctx context.Context,
	reconciliationID, accountID string,
) (int64, error) {
	var epoch int64
	err := store.pool.QueryRow(
		ctx, sandboxQualificationReconciliationEpochSQL, reconciliationID, accountID,
	).Scan(&epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, console.ErrPrecondition
	}
	return epoch, err
}
