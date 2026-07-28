package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryRotationStore struct{ rotation CredentialRotation }

func (store *memoryRotationStore) LockForCredentialRotation(
	_ context.Context,
	command CredentialRotationCommand,
) (CredentialRotation, error) {
	if command.ExpectedRevision != 1 {
		return CredentialRotation{}, errors.New("revision")
	}
	store.rotation = CredentialRotation{
		ID: command.ID, AccountID: command.AccountID, Stage: RotationCommanded,
		PriorGeneration: 1, PriorFingerprint: "00112233445566778899aabbccddeeff",
		NonterminalQuarantined: true, StartedAt: command.Now,
		UpdatedAt: command.Now, Revision: 2,
	}
	return store.rotation, nil
}

func (store *memoryRotationStore) MarkExternalSecretReplacement(
	_ context.Context, _ string, revision uint64, now time.Time,
) (CredentialRotation, error) {
	return store.advance(revision, RotationSecretsReplaced, now)
}

func (store *memoryRotationStore) ValidateRotatedCredential(
	_ context.Context, _ string, revision uint64, identity AccountIdentity, now time.Time,
) (CredentialRotation, error) {
	if identity.KeyFingerprint == store.rotation.PriorFingerprint ||
		identity.CredentialGeneration != store.rotation.PriorGeneration+1 {
		return CredentialRotation{}, errors.New("generation")
	}
	store.rotation.NewGeneration = identity.CredentialGeneration
	store.rotation.NewFingerprint = identity.KeyFingerprint
	return store.advance(revision, RotationRestartValidated, now)
}

func (store *memoryRotationStore) RecordRotationReconciliation(
	_ context.Context, _ string, revision uint64, _ ReconciliationResult, now time.Time,
) (CredentialRotation, error) {
	return store.advance(revision, RotationReconciled, now)
}

func (store *memoryRotationStore) CompleteCredentialRotation(
	_ context.Context, _ string, revision uint64, now time.Time,
) (CredentialRotation, error) {
	return store.advance(revision, RotationCompleted, now)
}

func (store *memoryRotationStore) advance(
	revision uint64,
	stage CredentialRotationStage,
	now time.Time,
) (CredentialRotation, error) {
	if revision != store.rotation.Revision {
		return CredentialRotation{}, errors.New("revision")
	}
	store.rotation.Stage, store.rotation.UpdatedAt = stage, now
	store.rotation.Revision++
	return store.rotation, nil
}

func TestCredentialRotationLocksThenValidatesGenerationAndReconciles(t *testing.T) {
	store := &memoryRotationStore{}
	service, _ := NewCredentialRotationService(store)
	at := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	rotation, err := service.Begin(context.Background(), CredentialRotationCommand{
		ID: "rotation-1", AccountID: "binance-testnet-a", ExpectedRevision: 1,
		AuthorizationID: "authorization-1", ActorUserID: "owner-1",
		ActorSessionID: "session-1",
		SourceHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReasonHash:     "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Now:            at,
	})
	if err != nil || rotation.Stage != RotationCommanded || !rotation.NonterminalQuarantined {
		t.Fatalf("begin = %#v %v", rotation, err)
	}
	rotation, _ = service.ConfirmExternalReplacement(context.Background(), rotation.ID, rotation.Revision, at)
	identity := AccountIdentity{
		AccountID: rotation.AccountID, Exchange: ExchangeBinance,
		Environment:          EnvironmentBinanceSpotTestnet,
		AccountIdentityHash:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		KeyFingerprint:       "11112222333344445555666677778888",
		CredentialGeneration: 2, OwnerAttested: true, ValidatedAt: at,
	}
	rotation, err = service.ValidateRestart(context.Background(), rotation.ID, rotation.Revision, identity, at)
	if err != nil || rotation.NewGeneration != 2 {
		t.Fatalf("restart validation = %#v %v", rotation, err)
	}
	result := ReconciliationResult{
		ID: "reconciliation-1", AccountID: rotation.AccountID, AccountEpoch: 1,
		State:        "clean",
		EvidenceHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		ReconciledAt: at,
	}
	rotation, err = service.Reconcile(context.Background(), rotation.ID, rotation.Revision, result, at)
	if err != nil {
		t.Fatal(err)
	}
	rotation, err = service.Complete(context.Background(), rotation.ID, rotation.Revision, at)
	if err != nil || rotation.Stage != RotationCompleted {
		t.Fatalf("completion = %#v %v", rotation, err)
	}
}
