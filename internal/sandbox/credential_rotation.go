package sandbox

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

// CredentialRotationStage is one lock-first operator-managed rotation state.
type CredentialRotationStage string

// Credential rotation stages form a closed forward-only workflow.
const (
	RotationCommanded        CredentialRotationStage = "COMMAND_LOCKED"
	RotationSecretsReplaced  CredentialRotationStage = "SECRETS_REPLACED_EXTERNALLY"
	RotationRestartValidated CredentialRotationStage = "RESTART_VALIDATED"
	RotationReconciled       CredentialRotationStage = "RECONCILED"
	RotationCompleted        CredentialRotationStage = "READY_PAUSED"
)

// CredentialRotation is the redacted persisted state of one credential change.
type CredentialRotation struct {
	ID                     string
	AccountID              AccountID
	Stage                  CredentialRotationStage
	PriorGeneration        uint64
	NewGeneration          uint64
	PriorFingerprint       string
	NewFingerprint         string
	NonterminalQuarantined bool
	StartedAt              time.Time
	UpdatedAt              time.Time
	Revision               uint64
}

// CredentialRotationCommand is the redacted, consumed high-risk authorization
// bound to one lock-first credential rotation attempt.
type CredentialRotationCommand struct {
	ID               string
	AccountID        AccountID
	ExpectedRevision uint64
	AuthorizationID  string
	ActorUserID      string
	ActorSessionID   string
	SourceHash       string
	ReasonHash       string
	Now              time.Time
}

// CredentialRotationStore persists forward-only rotation transitions.
type CredentialRotationStore interface {
	LockForCredentialRotation(context.Context, CredentialRotationCommand) (CredentialRotation, error)
	MarkExternalSecretReplacement(context.Context, string, uint64, time.Time) (CredentialRotation, error)
	ValidateRotatedCredential(context.Context, string, uint64, AccountIdentity, time.Time) (CredentialRotation, error)
	RecordRotationReconciliation(context.Context, string, uint64, ReconciliationResult, time.Time) (CredentialRotation, error)
	CompleteCredentialRotation(context.Context, string, uint64, time.Time) (CredentialRotation, error)
}

// CredentialRotationService exposes no secret-write method. Replacing files is
// an operator action outside the API and engine process.
type CredentialRotationService struct{ store CredentialRotationStore }

// NewCredentialRotationService constructs the secret-free rotation coordinator.
func NewCredentialRotationService(store CredentialRotationStore) (*CredentialRotationService, error) {
	if store == nil {
		return nil, contractError("credential_rotation_store_missing")
	}
	return &CredentialRotationService{store: store}, nil
}

// Begin locks the account and quarantines all nonterminal orders.
func (service *CredentialRotationService) Begin(
	ctx context.Context,
	command CredentialRotationCommand,
) (CredentialRotation, error) {
	if command.ID == "" || command.AccountID == "" || command.ExpectedRevision == 0 ||
		command.AuthorizationID == "" || command.ActorUserID == "" ||
		command.ActorSessionID == "" || !rotationHash(command.SourceHash) ||
		!rotationHash(command.ReasonHash) || command.Now.IsZero() ||
		command.Now.Location() != time.UTC {
		return CredentialRotation{}, contractError("credential_rotation_invalid")
	}
	return service.store.LockForCredentialRotation(ctx, command)
}

// ConfirmExternalReplacement records that an operator replaced files outside the API.
func (service *CredentialRotationService) ConfirmExternalReplacement(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	now time.Time,
) (CredentialRotation, error) {
	return service.store.MarkExternalSecretReplacement(ctx, id, expectedRevision, now)
}

// ValidateRestart persists the validated next credential generation.
func (service *CredentialRotationService) ValidateRestart(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	identity AccountIdentity,
	now time.Time,
) (CredentialRotation, error) {
	if err := identity.Validate(); err != nil {
		return CredentialRotation{}, err
	}
	return service.store.ValidateRotatedCredential(ctx, id, expectedRevision, identity, now)
}

// Reconcile attaches an exchange-authoritative post-restart result.
func (service *CredentialRotationService) Reconcile(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	result ReconciliationResult,
	now time.Time,
) (CredentialRotation, error) {
	if err := result.Validate(); err != nil {
		return CredentialRotation{}, err
	}
	return service.store.RecordRotationReconciliation(ctx, id, expectedRevision, result, now)
}

// Complete returns the locked account to READY_PAUSED.
func (service *CredentialRotationService) Complete(
	ctx context.Context,
	id string,
	expectedRevision uint64,
	now time.Time,
) (CredentialRotation, error) {
	return service.store.CompleteCredentialRotation(ctx, id, expectedRevision, now)
}

// ErrCredentialFilesAPIOperation prevents API-managed credential file writes.
var ErrCredentialFilesAPIOperation = errors.New("credential_files_are_operator_managed")

func rotationHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
