package postgres

import (
	"context"
	"fmt"
	"time"

	"axiom/internal/authentication"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// RecordRotationReconciliation attaches the post-restart authoritative result.
func (store *V1CDispatcherStore) RecordRotationReconciliation(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	result sandbox.ReconciliationResult,
	now time.Time,
) (sandbox.CredentialRotation, error) {
	if err := result.Validate(); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_reconciliation_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var accountID string
	if err = tx.QueryRow(ctx, `
SELECT account_id FROM v1c_credential_rotations
WHERE id=$1 AND revision=$2 AND stage='RESTART_VALIDATED'
FOR UPDATE`, id, expectedRevision).Scan(&accountID); err != nil ||
		accountID != string(result.AccountID) {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_reconciliation_failed")
	}
	if err = insertV1CReconciliation(ctx, tx, result); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	if result.State == "quarantined" {
		if err = quarantineV1CReconciliation(ctx, tx, result); err != nil {
			return sandbox.CredentialRotation{}, err
		}
	} else {
		tag, updateErr := tx.Exec(ctx, `
UPDATE v1c_credential_rotations
SET stage='RECONCILED',reconciliation_id=$3,updated_at=$4,revision=revision+1
WHERE id=$1 AND revision=$2 AND stage='RESTART_VALIDATED' AND account_id=$5`,
			id, expectedRevision, result.ID, now, result.AccountID)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_reconciliation_failed")
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_reconciliation_commit_failed")
	}
	return store.readCredentialRotation(ctx, id)
}

// CompleteCredentialRotation returns a reconciled account to READY_PAUSED.
func (store *V1CDispatcherStore) CompleteCredentialRotation(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	now time.Time,
) (sandbox.CredentialRotation, error) {
	if id == "" || expectedRevision == 0 || now.IsZero() || now.Location() != time.UTC {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_complete_failed")
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_complete_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = completeV1CRotationTransaction(ctx, tx, id, expectedRevision, now); err != nil {
		return sandbox.CredentialRotation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_complete_commit_failed")
	}
	return store.readCredentialRotation(ctx, id)
}

func completeV1CRotationTransaction(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	expectedRevision uint64,
	now time.Time,
) error {
	completion, err := lockV1CRotationForCompletion(ctx, tx, id, expectedRevision)
	if err != nil {
		return fmt.Errorf("v1c_rotation_complete_failed")
	}
	tag, err := tx.Exec(ctx, `
UPDATE v1c_credential_rotations
SET stage='READY_PAUSED',updated_at=$3,revision=revision+1
WHERE id=$1 AND revision=$2 AND stage='RECONCILED'`,
		id, expectedRevision, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("v1c_rotation_complete_failed")
	}
	tag, err = tx.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='READY_PAUSED',revision=revision+1,updated_at=$2
WHERE id=$1 AND state='LOCKED' AND revision=$3`,
		completion.accountID, now, completion.accountRevision)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("v1c_rotation_ready_failed")
	}
	return appendV1CRotationCompletionAudit(ctx, tx, id, expectedRevision, completion, now)
}

func appendV1CRotationCompletionAudit(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	expectedRevision uint64,
	completion v1CRotationCompletionState,
	now time.Time,
) error {
	beforeHash := v1CRotationStateHash(
		completion.accountID,
		"LOCKED",
		completion.newGeneration,
		completion.newFingerprint,
		uint64(completion.accountRevision),
	)
	afterHash := v1CRotationStateHash(
		completion.accountID,
		"READY_PAUSED",
		completion.newGeneration,
		completion.newFingerprint,
		uint64(completion.accountRevision+1),
	)
	if err := appendHighRiskAudit(ctx, tx, authentication.HighRiskAudit{
		ID:          id + "-ready-audit",
		ActorUserID: completion.actorUserID,
		SessionID:   completion.actorSessionID,
		Purpose:     authentication.PurposeCredentialRotate,
		Outcome:     "rotation_completed_ready_paused",
		SourceHash:  completion.sourceHash,
		ReasonHash:  completion.reasonHash,
		Revision:    int64(expectedRevision + 1),
		BeforeHash:  beforeHash,
		AfterHash:   afterHash,
		OccurredAt:  now,
	}); err != nil {
		return err
	}
	return nil
}

type v1CRotationCompletionState struct {
	accountID       string
	actorUserID     string
	actorSessionID  string
	sourceHash      string
	reasonHash      string
	newFingerprint  string
	newGeneration   int64
	accountRevision int64
}

func lockV1CRotationForCompletion(
	ctx context.Context,
	tx pgx.Tx,
	id string,
	expectedRevision uint64,
) (v1CRotationCompletionState, error) {
	var state v1CRotationCompletionState
	err := tx.QueryRow(ctx, `
SELECT rotation.account_id,rotation.actor_user_id,rotation.actor_session_id,
       rotation.source_hash,rotation.reason_hash,rotation.new_generation,
       rotation.new_fingerprint,account.revision
FROM v1c_credential_rotations rotation
JOIN v1c_exchange_accounts account ON account.id=rotation.account_id
WHERE rotation.id=$1 AND rotation.revision=$2 AND rotation.stage='RECONCILED'
  AND rotation.new_generation=rotation.prior_generation+1
  AND rotation.new_fingerprint IS NOT NULL
  AND account.state='LOCKED'
  AND account.credential_generation=rotation.new_generation
FOR UPDATE OF rotation,account`,
		id, expectedRevision,
	).Scan(
		&state.accountID,
		&state.actorUserID,
		&state.actorSessionID,
		&state.sourceHash,
		&state.reasonHash,
		&state.newGeneration,
		&state.newFingerprint,
		&state.accountRevision,
	)
	return state, err
}

func (store *V1CDispatcherStore) advanceRotation(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	prior, next string,
	now time.Time,
) (sandbox.CredentialRotation, error) {
	tag, err := store.pool.Exec(ctx, `
UPDATE v1c_credential_rotations
SET stage=$4,updated_at=$3,revision=revision+1
WHERE id=$1 AND revision=$2 AND stage=$5`,
		id, expectedRevision, now, next, prior)
	if err != nil || tag.RowsAffected() != 1 {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_transition_failed")
	}
	return store.readCredentialRotation(ctx, id)
}

func (store *V1CDispatcherStore) readCredentialRotation(
	ctx context.Context,
	id string,
) (sandbox.CredentialRotation, error) {
	var value sandbox.CredentialRotation
	var account string
	var newGeneration *int64
	var newFingerprint *string
	err := store.pool.QueryRow(ctx, `
SELECT id,account_id,stage,prior_generation,new_generation,prior_fingerprint,new_fingerprint,
 nonterminal_quarantined,started_at,updated_at,revision
FROM v1c_credential_rotations WHERE id=$1`, id).Scan(
		&value.ID, &account, &value.Stage, &value.PriorGeneration, &newGeneration,
		&value.PriorFingerprint, &newFingerprint, &value.NonterminalQuarantined,
		&value.StartedAt, &value.UpdatedAt, &value.Revision)
	if err != nil {
		return sandbox.CredentialRotation{}, fmt.Errorf("v1c_rotation_read_failed")
	}
	value.AccountID = sandbox.AccountID(account)
	value.StartedAt = value.StartedAt.UTC()
	value.UpdatedAt = value.UpdatedAt.UTC()
	if newGeneration != nil {
		value.NewGeneration = uint64(*newGeneration)
	}
	if newFingerprint != nil {
		value.NewFingerprint = *newFingerprint
	}
	return value, nil
}

var _ sandbox.CredentialRotationStore = (*V1CDispatcherStore)(nil)
