package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5"
)

// SandboxRuntimeEngineAccount is the exact durable account generation an engine owns.
type SandboxRuntimeEngineAccount struct {
	AccountID            sandbox.AccountID
	Exchange             sandbox.Exchange
	Environment          sandbox.Environment
	AccountIdentityHash  string
	Epoch                uint64
	CredentialGeneration uint64
	State                sandbox.EngineState
}

// EnsureAttestedAccount creates only the owner-attested, still-LOCKED account
// shell required before the first fencing lease can be acquired.
func (store *SandboxRuntimeDispatcherStore) EnsureAttestedAccount(
	ctx context.Context,
	account SandboxRuntimeEngineAccount,
	registeredAt time.Time,
) (SandboxRuntimeEngineAccount, error) {
	if validateEngineAccountRegistration(account, registeredAt) != nil {
		return SandboxRuntimeEngineAccount{}, fmt.Errorf("sandbox_runtime_engine_account_invalid")
	}
	tx, err := store.pool.BeginTx(
		ctx,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
	)
	if err != nil {
		return SandboxRuntimeEngineAccount{}, fmt.Errorf("sandbox_runtime_engine_account_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = insertAttestedEngineAccount(ctx, tx, account, registeredAt); err != nil {
		return SandboxRuntimeEngineAccount{}, err
	}
	current, err := readSandboxRuntimeEngineAccount(ctx, tx, account.AccountID)
	if err != nil ||
		current.Exchange != account.Exchange ||
		current.Environment != account.Environment ||
		current.AccountIdentityHash != account.AccountIdentityHash ||
		current.CredentialGeneration != account.CredentialGeneration {
		return SandboxRuntimeEngineAccount{}, fmt.Errorf("sandbox_runtime_engine_account_conflict")
	}
	if err = tx.Commit(ctx); err != nil {
		return SandboxRuntimeEngineAccount{}, fmt.Errorf("sandbox_runtime_engine_account_commit_failed")
	}
	return current, nil
}

func insertAttestedEngineAccount(
	ctx context.Context,
	tx pgx.Tx,
	account SandboxRuntimeEngineAccount,
	registeredAt time.Time,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,$2,$3,$4,'LOCKED',1,$5,1,$6,$6)
ON CONFLICT DO NOTHING`,
		account.AccountID,
		account.Exchange,
		account.Environment,
		account.AccountIdentityHash,
		account.CredentialGeneration,
		registeredAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_account_insert_failed")
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)
ON CONFLICT DO NOTHING`,
		account.AccountID,
		registeredAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_epoch_insert_failed")
	}
	return nil
}

// RecordValidatedEngineIdentity accepts only the live exchange response bound
// to the already-attested account and immutable credential generation.
func (store *SandboxRuntimeDispatcherStore) RecordValidatedEngineIdentity(
	ctx context.Context,
	identity sandbox.AccountIdentity,
) error {
	if identity.Validate() != nil {
		return fmt.Errorf("sandbox_runtime_engine_identity_invalid")
	}
	tx, err := store.pool.BeginTx(
		ctx,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
	)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_engine_identity_begin_failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err = verifyEngineIdentityAccount(ctx, tx, identity); err != nil {
		return err
	}
	if err = persistValidatedEngineIdentity(ctx, tx, identity); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_identity_commit_failed")
	}
	return nil
}

func verifyEngineIdentityAccount(
	ctx context.Context,
	tx pgx.Tx,
	identity sandbox.AccountIdentity,
) error {
	current, err := readSandboxRuntimeEngineAccount(ctx, tx, identity.AccountID)
	if err != nil ||
		current.Exchange != identity.Exchange ||
		current.Environment != identity.Environment ||
		current.AccountIdentityHash != identity.AccountIdentityHash ||
		current.CredentialGeneration != identity.CredentialGeneration {
		return fmt.Errorf("sandbox_runtime_engine_identity_conflict")
	}
	return nil
}

func persistValidatedEngineIdentity(
	ctx context.Context,
	tx pgx.Tx,
	identity sandbox.AccountIdentity,
) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT DO NOTHING`,
		identity.AccountID,
		identity.CredentialGeneration,
		identity.KeyFingerprint,
		identity.AccountIdentityHash,
		identity.ValidatedAt,
	); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_identity_insert_failed")
	}
	var same bool
	if err := tx.QueryRow(ctx, `
SELECT key_fingerprint=$3 AND account_identity_hash=$4
FROM sandbox_runtime_credential_generations
WHERE account_id=$1 AND generation=$2`,
		identity.AccountID,
		identity.CredentialGeneration,
		identity.KeyFingerprint,
		identity.AccountIdentityHash,
	).Scan(&same); err != nil || !same {
		return fmt.Errorf("sandbox_runtime_engine_identity_conflict")
	}
	return nil
}

// SetEngineAccountState persists the fail-closed engine readiness state.
func (store *SandboxRuntimeDispatcherStore) SetEngineAccountState(
	ctx context.Context,
	account sandbox.AccountID,
	state sandbox.EngineState,
	now time.Time,
) error {
	if account == "" ||
		(state != sandbox.EngineLocked && state != sandbox.EngineReadyPaused &&
			state != sandbox.EngineDegraded) ||
		now.IsZero() || now.Location() != time.UTC {
		return fmt.Errorf("sandbox_runtime_engine_state_invalid")
	}
	tag, err := store.pool.Exec(ctx, `
UPDATE sandbox_runtime_exchange_accounts
SET state=$2,revision=revision+1,updated_at=$3
WHERE id=$1`,
		account,
		state,
		now,
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_engine_state_update_failed")
	}
	return nil
}

// RenewAccountLease extends only the exact live owner/fencing token.
func (store *SandboxRuntimeDispatcherStore) RenewAccountLease(
	ctx context.Context,
	account sandbox.AccountID,
	owner string,
	fence uint64,
	now time.Time,
	ttl time.Duration,
) error {
	if account == "" || owner == "" || fence == 0 ||
		now.IsZero() || now.Location() != time.UTC ||
		ttl <= 0 || ttl > 5*time.Minute {
		return fmt.Errorf("sandbox_runtime_lease_renew_invalid")
	}
	tag, err := store.pool.Exec(ctx, `
UPDATE sandbox_runtime_account_leases
SET expires_at=$5
WHERE account_id=$1 AND owner=$2 AND fencing_token=$3
  AND expires_at>$4`,
		account,
		owner,
		fence,
		now,
		now.Add(ttl),
	)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("sandbox_runtime_lease_renew_failed")
	}
	return nil
}

func validateEngineAccountRegistration(
	account SandboxRuntimeEngineAccount,
	registeredAt time.Time,
) error {
	if account.AccountID == "" ||
		(account.Exchange != sandbox.ExchangeBinance &&
			account.Exchange != sandbox.ExchangeBybit) ||
		account.Environment == "" ||
		len(account.AccountIdentityHash) != 64 ||
		account.CredentialGeneration == 0 ||
		registeredAt.IsZero() || registeredAt.Location() != time.UTC {
		return fmt.Errorf("sandbox_runtime_engine_account_invalid")
	}
	if (account.Exchange == sandbox.ExchangeBinance &&
		account.Environment != sandbox.EnvironmentBinanceSpotTestnet) ||
		(account.Exchange == sandbox.ExchangeBybit &&
			account.Environment != sandbox.EnvironmentBybitDemo) {
		return fmt.Errorf("sandbox_runtime_engine_account_invalid")
	}
	if _, err := hex.DecodeString(account.AccountIdentityHash); err != nil {
		return fmt.Errorf("sandbox_runtime_engine_account_invalid")
	}
	return nil
}

type sandboxRuntimeEngineAccountRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readSandboxRuntimeEngineAccount(
	ctx context.Context,
	rower sandboxRuntimeEngineAccountRower,
	account sandbox.AccountID,
) (SandboxRuntimeEngineAccount, error) {
	var result SandboxRuntimeEngineAccount
	var epoch, generation int64
	err := rower.QueryRow(ctx, `
SELECT id,exchange,environment,native_account_hash,current_epoch,
       credential_generation,state
FROM sandbox_runtime_exchange_accounts
WHERE id=$1 FOR UPDATE`,
		account,
	).Scan(
		&result.AccountID,
		&result.Exchange,
		&result.Environment,
		&result.AccountIdentityHash,
		&epoch,
		&generation,
		&result.State,
	)
	if err != nil || epoch <= 0 || generation <= 0 {
		return SandboxRuntimeEngineAccount{}, fmt.Errorf("sandbox_runtime_engine_account_read_failed")
	}
	result.Epoch = uint64(epoch)
	result.CredentialGeneration = uint64(generation)
	return result, nil
}

// SandboxRuntimeEngineStartupEvidenceSink durably appends one exact startup cycle.
type SandboxRuntimeEngineStartupEvidenceSink struct {
	store    *SandboxRuntimeDispatcherStore
	account  sandbox.AccountID
	exchange sandbox.Exchange
	cycle    uint64
}

// NewSandboxRuntimeEngineStartupEvidenceSink binds one immutable evidence sink to an
// account, exchange, and fencing cycle.
func NewSandboxRuntimeEngineStartupEvidenceSink(
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	exchange sandbox.Exchange,
	cycle uint64,
) (*SandboxRuntimeEngineStartupEvidenceSink, error) {
	if store == nil || account == "" || cycle == 0 ||
		(exchange != sandbox.ExchangeBinance &&
			exchange != sandbox.ExchangeBybit) {
		return nil, fmt.Errorf("sandbox_runtime_startup_evidence_sink_invalid")
	}
	return &SandboxRuntimeEngineStartupEvidenceSink{
		store:    store,
		account:  account,
		exchange: exchange,
		cycle:    cycle,
	}, nil
}

// AppendCollectorLifecycle persists one hash-bound startup stage.
func (sink *SandboxRuntimeEngineStartupEvidenceSink) AppendCollectorLifecycle(
	event exchangecontracts.CollectorLifecycleEvidence,
) error {
	if event.Exchange != string(sink.exchange) ||
		event.Cycle != sink.cycle ||
		event.Phase != "sandbox_startup" ||
		event.Stage == "" ||
		event.ObservedAt.IsZero() ||
		event.ObservedAt.Location() != time.UTC {
		return fmt.Errorf("sandbox_runtime_startup_evidence_invalid")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sandbox_runtime_startup_evidence_encode_failed")
	}
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	id := "sandbox_runtime-startup-" + hash[:32]
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	inserted, err := sink.insertStartupEvidence(ctx, event, id, hash)
	if err != nil || inserted {
		return err
	}
	return sink.verifyStartupEvidence(ctx, event, id, hash)
}

func (sink *SandboxRuntimeEngineStartupEvidenceSink) insertStartupEvidence(
	ctx context.Context,
	event exchangecontracts.CollectorLifecycleEvidence,
	id, hash string,
) (bool, error) {
	tag, err := sink.store.pool.Exec(ctx, `
INSERT INTO sandbox_runtime_engine_startup_evidence(
 id,account_id,exchange,startup_cycle,stage,reached_healthy,
 evidence_hash,observed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT DO NOTHING`,
		id,
		sink.account,
		sink.exchange,
		sink.cycle,
		event.Stage,
		event.ReachedHealthy,
		hash,
		event.ObservedAt,
	)
	if err != nil {
		return false, fmt.Errorf("sandbox_runtime_startup_evidence_insert_failed")
	}
	return tag.RowsAffected() == 1, nil
}

func (sink *SandboxRuntimeEngineStartupEvidenceSink) verifyStartupEvidence(
	ctx context.Context,
	event exchangecontracts.CollectorLifecycleEvidence,
	id, hash string,
) error {
	var same bool
	err := sink.store.pool.QueryRow(ctx, `
SELECT id=$1 AND exchange=$3 AND reached_healthy=$6 AND
       evidence_hash=$7 AND observed_at=$8
FROM sandbox_runtime_engine_startup_evidence
WHERE account_id=$2 AND startup_cycle=$4 AND stage=$5`,
		id,
		sink.account,
		sink.exchange,
		sink.cycle,
		event.Stage,
		event.ReachedHealthy,
		hash,
		event.ObservedAt,
	).Scan(&same)
	if err != nil || !same {
		return fmt.Errorf("sandbox_runtime_startup_evidence_conflict")
	}
	return nil
}

var _ exchangecontracts.LifecycleEvidenceSink = (*SandboxRuntimeEngineStartupEvidenceSink)(nil)
