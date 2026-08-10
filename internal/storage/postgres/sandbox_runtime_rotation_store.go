package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// LockForCredentialRotation locks an account and quarantines its nonterminal orders.
func (store *SandboxRuntimeDispatcherStore) LockForCredentialRotation(
	ctx context.Context,
	command sandbox.CredentialRotationCommand,
) (sandbox.CredentialRotation, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	generation, fingerprint, priorState, err := lockSandboxRuntimeRotationAccount(ctx, tx, command)
	if err != nil {
		return sandbox.CredentialRotation{}, err
	}
	beforeHash := sandboxRuntimeRotationStateHash(
		string(command.AccountID), priorState, generation, fingerprint,
		command.ExpectedRevision,
	)
	if _, err = tx.Exec(ctx, `
UPDATE sandbox_runtime_exchange_accounts
SET state='LOCKED',revision=revision+1,updated_at=$2 WHERE id=$1`,
		command.AccountID, command.Now); err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_lock_failed")
	}
	quarantined, err := quarantineSandboxRuntimeOrders(ctx, tx, command.AccountID, command.Now)
	if err != nil {
		return sandbox.CredentialRotation{}, err
	}
	if err = insertSandboxRuntimeRotation(
		ctx, tx, command, generation, fingerprint, quarantined,
	); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	afterHash := sandboxRuntimeRotationStateHash(
		string(command.AccountID), "LOCKED", generation, fingerprint,
		command.ExpectedRevision+1,
	)
	if err = appendSandboxRuntimeRotationAudit(ctx, tx, command, beforeHash, afterHash); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_commit_failed")
	}
	return store.readCredentialRotation(ctx, command.ID)
}

func lockSandboxRuntimeRotationAccount(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.CredentialRotationCommand,
) (int64, string, string, error) {
	var generation int64
	var fingerprint, state string
	err := tx.QueryRow(ctx, `
SELECT account.credential_generation,generation.key_fingerprint,account.state
FROM sandbox_runtime_exchange_accounts account
JOIN sandbox_runtime_credential_generations generation
  ON generation.account_id=account.id AND generation.generation=account.credential_generation
WHERE account.id=$1 AND account.revision=$2 FOR UPDATE`,
		command.AccountID, command.ExpectedRevision,
	).Scan(&generation, &fingerprint, &state)
	if err != nil {
		return 0, "", "", fmt.Errorf("sandbox_runtime_rotation_account_rejected")
	}
	return generation, fingerprint, state, nil
}

func appendSandboxRuntimeRotationAudit(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.CredentialRotationCommand,
	beforeHash, afterHash string,
) error {
	return appendHighRiskAudit(ctx, tx, authentication.HighRiskAudit{
		ID:          command.ID + "-command-audit",
		ActorUserID: command.ActorUserID,
		SessionID:   command.ActorSessionID,
		Purpose:     authentication.PurposeCredentialRotate,
		Outcome:     "rotation_command_locked",
		SourceHash:  command.SourceHash,
		ReasonHash:  command.ReasonHash,
		Revision:    int64(command.ExpectedRevision + 1),
		BeforeHash:  beforeHash,
		AfterHash:   afterHash,
		OccurredAt:  command.Now,
	})
}

func quarantineSandboxRuntimeOrders(
	ctx context.Context,
	tx pgx.Tx,
	account sandbox.AccountID,
	now time.Time,
) (bool, error) {
	tag, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET state='UNKNOWN',order_state='RECOVERY_REQUIRED',updated_at=$2
WHERE account_id=$1 AND state IN ('PENDING','CLAIMED','ACKNOWLEDGED','UNKNOWN')`,
		account, now)
	if err != nil {
		return false, fmt.Errorf("sandbox_runtime_rotation_quarantine_failed")
	}
	waitingTag, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_outbox
SET order_state='RECOVERY_REQUIRED',updated_at=$2
WHERE account_id=$1 AND state='WAITING'`, account, now)
	if err != nil {
		return false, fmt.Errorf("sandbox_runtime_rotation_quarantine_failed")
	}
	if _, err = tx.Exec(ctx, `
UPDATE sandbox_runtime_submission_plans plan
SET state='RECOVERY_REQUIRED',final_disposition=NULL,
    saga_revision=saga_revision+1,revision=revision+1
WHERE EXISTS (
  SELECT 1 FROM sandbox_runtime_submission_outbox outbox
  WHERE outbox.plan_id=plan.id AND outbox.account_id=$1
    AND (
      outbox.state='UNKNOWN' OR
      (outbox.state='WAITING' AND outbox.order_state='RECOVERY_REQUIRED')
    )
) AND state NOT IN ('RECOVERY_REQUIRED','QUARANTINED')`, account); err != nil {
		return false, fmt.Errorf("sandbox_runtime_rotation_plan_quarantine_failed")
	}
	return tag.RowsAffected() > 0 || waitingTag.RowsAffected() > 0, nil
}

func insertSandboxRuntimeRotation(
	ctx context.Context,
	tx pgx.Tx,
	command sandbox.CredentialRotationCommand,
	generation int64,
	fingerprint string,
	quarantined bool,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_rotations(
 id,account_id,authorization_id,actor_user_id,actor_session_id,source_hash,reason_hash,
 stage,prior_generation,prior_fingerprint,nonterminal_quarantined,
 started_at,updated_at,revision
) VALUES ($1,$2,$3,$4,$5,$6,$7,'COMMAND_LOCKED',$8,$9,$10,$11,$11,1)`,
		command.ID, command.AccountID, command.AuthorizationID,
		command.ActorUserID, command.ActorSessionID, command.SourceHash,
		command.ReasonHash, generation, fingerprint, quarantined, command.Now); err != nil {
		return fmt.Errorf("sandbox_runtime_rotation_insert_failed")
	}
	return nil
}

func sandboxRuntimeRotationStateHash(
	account, state string,
	generation int64,
	fingerprint string,
	revision uint64,
) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		account,
		state,
		fmt.Sprint(generation),
		fingerprint,
		fmt.Sprint(revision),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// MarkExternalSecretReplacement records the operator-managed file replacement.
func (store *SandboxRuntimeDispatcherStore) MarkExternalSecretReplacement(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	now time.Time,
) (sandbox.CredentialRotation, error) {
	return store.advanceRotation(ctx, id, expectedRevision, "COMMAND_LOCKED",
		"SECRETS_REPLACED_EXTERNALLY", now)
}

// ValidateRotatedCredential persists the validated next account credential generation.
func (store *SandboxRuntimeDispatcherStore) ValidateRotatedCredential(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	identity sandbox.AccountIdentity,
	now time.Time,
) (sandbox.CredentialRotation, error) {
	if id == "" || expectedRevision == 0 || now.IsZero() || now.Location() != time.UTC ||
		identity.Validate() != nil || identity.ValidatedAt.After(now) {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_identity_rejected")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_validate_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	prior, err := lockSandboxRuntimeRotationForValidation(
		ctx, tx, id, expectedRevision,
	)
	if err != nil || prior.accountID != string(identity.AccountID) ||
		prior.exchange != string(identity.Exchange) ||
		prior.environment != string(identity.Environment) ||
		prior.accountIdentityHash != identity.AccountIdentityHash ||
		identity.CredentialGeneration != uint64(prior.generation+1) ||
		identity.KeyFingerprint == prior.fingerprint {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_identity_rejected")
	}
	if err = persistSandboxRuntimeRotatedCredential(
		ctx, tx, id, expectedRevision, prior.accountID, identity, now,
	); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("sandbox_runtime_rotation_validate_commit_failed")
	}
	return store.readCredentialRotation(ctx, id)
}

type sandboxRuntimeRotationValidationState struct {
	accountID           string
	exchange            string
	environment         string
	accountIdentityHash string
	generation          int64
	fingerprint         string
}

func lockSandboxRuntimeRotationForValidation(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	expectedRevision uint64,
) (sandboxRuntimeRotationValidationState, error) {
	var state sandboxRuntimeRotationValidationState
	err := tx.QueryRow(ctx, `
SELECT rotation.account_id,account.exchange,account.environment,
       account.native_account_hash,rotation.prior_generation,
       rotation.prior_fingerprint
FROM sandbox_runtime_credential_rotations rotation
JOIN sandbox_runtime_exchange_accounts account ON account.id=rotation.account_id
WHERE rotation.id=$1 AND rotation.revision=$2
  AND rotation.stage='SECRETS_REPLACED_EXTERNALLY'
  AND account.state='LOCKED'
FOR UPDATE OF rotation,account`, id, expectedRevision).Scan(
		&state.accountID,
		&state.exchange,
		&state.environment,
		&state.accountIdentityHash,
		&state.generation,
		&state.fingerprint,
	)
	return state, err
}

func persistSandboxRuntimeRotatedCredential(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	expectedRevision uint64,
	accountID string,
	identity sandbox.AccountIdentity,
	now time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,$2,$3,$4,$5)`,
		accountID, identity.CredentialGeneration, identity.KeyFingerprint,
		identity.AccountIdentityHash, identity.ValidatedAt); err != nil {
		return fmt.Errorf("sandbox_runtime_rotation_generation_insert_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_exchange_accounts
SET credential_generation=$2,revision=revision+1,updated_at=$3
WHERE id=$1 AND state='LOCKED'`,
		accountID, identity.CredentialGeneration, now); err != nil {
		return fmt.Errorf("sandbox_runtime_rotation_account_update_failed")
	}
	if _, err := tx.Exec(ctx, `
UPDATE sandbox_runtime_credential_rotations
SET stage='RESTART_VALIDATED',new_generation=$3,new_fingerprint=$4,
    updated_at=$5,revision=revision+1
WHERE id=$1 AND revision=$2`,
		id, expectedRevision, identity.CredentialGeneration, identity.KeyFingerprint, now); err != nil {
		return fmt.Errorf("sandbox_runtime_rotation_validate_failed")
	}
	return nil
}
