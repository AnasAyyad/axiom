package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertSandboxRuntimeControlRecoveryAndReset(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 20, 0, 0, time.UTC)
	userID, actorSessionID := sandboxQualificationOwner(t, ctx, pool)
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	account := sandbox.AccountID("sandbox_runtime-control-account")
	seedSandboxRuntimeControlAccount(t, ctx, pool, account, userID, now)
	assertSandboxRuntimeArmFlow(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	assertSandboxRuntimeRiskUnlockFlow(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	assertSandboxRuntimeAccountSnapshotAndReset(
		t, ctx, pool, store, account, now.Add(5*time.Second),
	)
	assertSandboxRuntimeCredentialRotationAuthorization(
		t, ctx, pool, store, userID, actorSessionID, now.Add(10*time.Second),
	)
	assertSandboxRuntimeRevokeAllAuthorization(
		t, ctx, pool, userID, actorSessionID, now.Add(20*time.Second),
	)
}

func assertSandboxRuntimeArmFlow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	seedSandboxRuntimeArmAuthorization(
		t, ctx, pool, userID, actorSessionID, now,
	)
	arm := sandboxRuntimeControlArm(account, userID, actorSessionID, now)
	persistedArm, err := store.CreateSandboxArm(ctx, sandbox.ArmCommand{
		Arm: arm, AuthorizationID: "sandbox_runtime-control-arm-auth",
		SourceHash: strings.Repeat("a", 64), ExpectedSessionRevision: 1,
	})
	if err != nil {
		t.Fatalf("authorized arm failed: %v", err)
	}
	assertSandboxRuntimeArmTimestamps(t, ctx, pool, persistedArm)
	assertSandboxRuntimeArmState(t, ctx, pool, account)
}

func seedSandboxRuntimeArmAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	seedSandboxRuntimeConsumedAuthorization(
		t,
		ctx,
		pool,
		"sandbox_runtime-control-arm-auth",
		strings.Repeat("9", 64),
		userID,
		actorSessionID,
		"sandbox_arm",
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
		now,
	)
}

func sandboxRuntimeControlArm(
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) sandbox.Arm {
	createdAt := now.Add(time.Second + 721*time.Nanosecond)
	return sandbox.Arm{
		ID: "sandbox_runtime-control-arm", SessionID: "sandbox_runtime-control-session",
		AccountIDs:        []sandbox.AccountID{account},
		AuthorizationHash: strings.Repeat("c", 64),
		ActorUserID:       userID, ActorSessionID: actorSessionID,
		ReasonHash: strings.Repeat("b", 64),
		CreatedAt:  createdAt,
		ExpiresAt:  createdAt.Add(sandbox.ArmLifetime),
		Revision:   1,
	}
}

func assertSandboxRuntimeArmTimestamps(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	arm sandbox.Arm,
) {
	t.Helper()
	var storedCreatedAt, storedExpiresAt time.Time
	if err := pool.QueryRow(ctx, `
SELECT created_at,expires_at FROM sandbox_runtime_sandbox_arms WHERE id=$1`,
		arm.ID,
	).Scan(&storedCreatedAt, &storedExpiresAt); err != nil {
		t.Fatal(err)
	}
	if !arm.CreatedAt.Equal(storedCreatedAt) ||
		!arm.ExpiresAt.Equal(storedExpiresAt) {
		t.Fatalf(
			"returned arm timestamps differ from storage: returned=%s/%s stored=%s/%s",
			arm.CreatedAt.Format(time.RFC3339Nano),
			arm.ExpiresAt.Format(time.RFC3339Nano),
			storedCreatedAt.Format(time.RFC3339Nano),
			storedExpiresAt.Format(time.RFC3339Nano),
		)
	}
}

func assertSandboxRuntimeRiskUnlockFlow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_exchange_accounts
SET state='LOCKED',revision=revision+1,updated_at=$2
WHERE id=$1`, account, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	reconciliation := sandbox.ReconciliationResult{
		ID: "sandbox_runtime-control-reconciliation", AccountID: account, AccountEpoch: 1,
		State: "clean", EvidenceHash: strings.Repeat("d", 64),
		ReconciledAt: now.Add(2 * time.Second),
	}
	if err := store.RecordReconciliation(ctx, reconciliation); err != nil {
		t.Fatal(err)
	}
	seedSandboxRuntimeConsumedAuthorization(
		t,
		ctx,
		pool,
		"sandbox_runtime-control-risk-auth",
		strings.Repeat("8", 64),
		userID,
		actorSessionID,
		"risk_unlock",
		strings.Repeat("e", 64),
		strings.Repeat("f", 64),
		now.Add(3*time.Second),
	)
	if err := store.RiskUnlock(ctx, sandbox.RiskUnlockCommand{
		ID: "sandbox_runtime-control-risk-unlock", AccountID: account, ExpectedRevision: 3,
		AuthorizationID: "sandbox_runtime-control-risk-auth", ActorUserID: userID,
		ActorSessionID: actorSessionID, SourceHash: strings.Repeat("e", 64),
		ReasonHash:       strings.Repeat("f", 64),
		ReconciliationID: reconciliation.ID, ReconciliationEpoch: 1,
		Now: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("authorized risk unlock failed: %v", err)
	}
	assertSandboxRuntimeRiskUnlockState(t, ctx, pool, account)
}

func seedSandboxRuntimeControlAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	userID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO assets(symbol) VALUES ('BTC'),('USDT') ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	seedSandboxRuntimeControlAccountIdentity(t, ctx, pool, account, now)
	seedSandboxRuntimeControlSession(t, ctx, pool, account, userID, now)
}

func seedSandboxRuntimeControlAccountIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'binance','spot_testnet',$2,'READY_PAUSED',1,1,1,$3,$3)`,
		account,
		strings.Repeat("1", 64),
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,1,$2,$3,$4)`,
		account,
		strings.Repeat("2", 32),
		strings.Repeat("1", 64),
		now,
	); err != nil {
		t.Fatal(err)
	}
}

func seedSandboxRuntimeControlSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	userID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,created_at,updated_at
) VALUES ('sandbox_runtime-control-session','READY_PAUSED','sandbox_runtime-control-config',$1,1,$2,$3,$3)`,
		strings.Repeat("3", 64),
		userID,
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_session_accounts(session_id,account_id,account_epoch)
VALUES ('sandbox_runtime-control-session',$1,1)`, account); err != nil {
		t.Fatal(err)
	}
}

func seedSandboxRuntimeConsumedAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id, tokenHash, userID, sessionID, purpose, sourceHash, reasonHash string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,source_hash,reason_hash,
 created_at,expires_at,consumed_at
) VALUES (
 $1,$2,$3,$4,$5,1,(SELECT revision FROM sessions WHERE id=$4),
 $6,$7,$8,$9,$8
)`,
		id,
		tokenHash,
		userID,
		sessionID,
		purpose,
		sourceHash,
		reasonHash,
		now,
		now.Add(2*time.Minute),
	); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxRuntimeArmState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
) {
	t.Helper()
	var accountState, sessionState string
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_exchange_accounts WHERE id=$1`, account).Scan(&accountState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_sandbox_sessions WHERE id='sandbox_runtime-control-session'`,
	).Scan(&sessionState); err != nil {
		t.Fatal(err)
	}
	if accountState != "ARMED" || sessionState != "ARMED" {
		t.Fatalf("arm state account=%s session=%s", accountState, sessionState)
	}
}

func assertSandboxRuntimeRiskUnlockState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
) {
	t.Helper()
	var state string
	var unlocks, audits int
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_exchange_accounts WHERE id=$1`, account).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_risk_unlocks WHERE account_id=$1`, account).Scan(&unlocks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_high_risk_audit_events
WHERE (id='sandbox_runtime-control-arm-result-audit' AND outcome='sandbox_armed')
   OR (id='sandbox_runtime-control-risk-unlock-result-audit' AND outcome='risk_unlocked_ready_paused')`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if state != "READY_PAUSED" || unlocks != 1 || audits != 2 {
		t.Fatalf("risk unlock state=%s rows=%d audits=%d", state, unlocks, audits)
	}
}

func assertSandboxRuntimeAccountSnapshotAndReset(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	recordSandboxRuntimeAccountSnapshot(t, ctx, store, account, now)
	recordSandboxRuntimeAccountReset(t, ctx, store, account, now)
	assertSandboxRuntimeResetState(t, ctx, pool, account)
	assertSandboxRuntimeNativeFillIdentityCrossEpoch(t, ctx, pool, account, now)
}

func recordSandboxRuntimeAccountSnapshot(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	snapshot := sandboxRuntimeControlSnapshot(t, account, now)
	if err := store.RecordAccountSnapshot(
		ctx,
		"sandbox_runtime-control-snapshot",
		snapshot,
	); err != nil {
		t.Fatal(err)
	}
	recordRepeatedSandboxRuntimeAccountState(t, ctx, store, snapshot, now)
	assertSandboxRuntimeAccountSnapshotRoundTrip(t, ctx, store, snapshot)
	rejectConflictingSandboxRuntimeAccountSnapshot(t, ctx, store, snapshot, now)
}

func sandboxRuntimeControlSnapshot(
	t *testing.T,
	account sandbox.AccountID,
	now time.Time,
) sandbox.AccountSnapshot {
	t.Helper()
	available, _ := domain.ParseBalance("20")
	reserved, _ := domain.ParseBalance("0")
	return sandbox.AccountSnapshot{
		AccountID: account, Epoch: 1,
		Balances: []sandbox.Balance{{
			Asset: "USDT", Available: available, Reserved: reserved,
		}},
		OrdersHash:   strings.Repeat("1", 64),
		FillsHash:    strings.Repeat("2", 64),
		SnapshotHash: strings.Repeat("3", 64),
		ObservedAt:   now,
	}
}

func recordRepeatedSandboxRuntimeAccountState(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	snapshot sandbox.AccountSnapshot,
	now time.Time,
) {
	t.Helper()
	repeatedState := snapshot
	repeatedState.ObservedAt = now.Add(time.Second)
	if err := store.RecordAccountSnapshot(
		ctx,
		"sandbox_runtime-control-snapshot-repeated-state",
		repeatedState,
	); err != nil {
		t.Fatalf("repeated immutable account state was not idempotent: %v", err)
	}
	var snapshots int
	if err := store.pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_account_snapshots
WHERE account_id=$1 AND account_epoch=1`, snapshot.AccountID).Scan(&snapshots); err != nil ||
		snapshots != 1 {
		t.Fatalf("deduplicated account snapshots=%d error=%v", snapshots, err)
	}
}

func assertSandboxRuntimeAccountSnapshotRoundTrip(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	snapshot sandbox.AccountSnapshot,
) {
	t.Helper()
	loaded, found, err := store.LatestAccountSnapshot(
		ctx,
		snapshot.AccountID,
		snapshot.Epoch,
	)
	if err != nil || !found || loaded.SnapshotHash != snapshot.SnapshotHash ||
		loaded.ObservedAt.Location() != time.UTC {
		t.Fatalf(
			"account snapshot round trip found=%t snapshot=%#v error=%v",
			found,
			loaded,
			err,
		)
	}
}

func rejectConflictingSandboxRuntimeAccountSnapshot(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	snapshot sandbox.AccountSnapshot,
	now time.Time,
) {
	t.Helper()
	conflict := snapshot
	conflict.SnapshotHash = strings.Repeat("4", 64)
	conflict.ObservedAt = now.Add(2 * time.Second)
	if err := store.RecordAccountSnapshot(
		ctx,
		"sandbox_runtime-control-snapshot",
		conflict,
	); err == nil {
		t.Fatal("reused snapshot identity with a different state was accepted")
	}
}

func recordSandboxRuntimeAccountReset(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := store.AcquireAccountLease(
		ctx,
		account,
		sandbox.EnvironmentBinanceSpotTestnet,
		"reset-worker",
		now,
		time.Minute,
		sandbox.NoKillPoint{},
	); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAccountReset(ctx, sandbox.AccountResetIncident{
		ID: "sandbox_runtime-control-reset", AccountID: account, PriorEpoch: 1,
		EvidenceHash: strings.Repeat("4", 64), DetectedAt: now.Add(time.Second),
		Adjustments: []sandbox.ExternalAdjustment{{
			ID: "sandbox_runtime-control-adjustment", Asset: "USDT", Quantity: "-20",
			AdjustmentHash: strings.Repeat("5", 64),
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxRuntimeResetState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
) {
	t.Helper()
	var epoch int64
	var state, sessionState string
	var armRevoked, pnlEffect bool
	var leases int
	if err := pool.QueryRow(ctx, `
SELECT current_epoch,state FROM sandbox_runtime_exchange_accounts WHERE id=$1`,
		account).Scan(&epoch, &state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT revoked_at IS NOT NULL FROM sandbox_runtime_sandbox_arms WHERE id='sandbox_runtime-control-arm'`,
	).Scan(&armRevoked); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_sandbox_sessions WHERE id='sandbox_runtime-control-session'`,
	).Scan(&sessionState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT pnl_effect FROM sandbox_runtime_external_adjustments
WHERE id='sandbox_runtime-control-adjustment'`).Scan(&pnlEffect); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_account_leases WHERE account_id=$1`,
		account).Scan(&leases); err != nil {
		t.Fatal(err)
	}
	if epoch != 2 || state != "LOCKED" || !armRevoked ||
		sessionState != "DEGRADED" || pnlEffect || leases != 0 {
		t.Fatalf(
			"reset epoch=%d state=%s arm=%t session=%s pnl=%t leases=%d",
			epoch,
			state,
			armRevoked,
			sessionState,
			pnlEffect,
			leases,
		)
	}
}

func assertSandboxRuntimeNativeFillIdentityCrossEpoch(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	fillHash := strings.Repeat("6", 64)
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_fills(
 account_id,account_epoch,native_fill_id_hash,order_id,canonical_fill,occurred_at
) VALUES ($1,1,$2,'order-old','{}'::jsonb,$3)`,
		account,
		fillHash,
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_fills(
 account_id,account_epoch,native_fill_id_hash,order_id,canonical_fill,occurred_at
) VALUES ($1,2,$2,'order-new','{}'::jsonb,$3)`,
		account,
		fillHash,
		now.Add(time.Second),
	); err == nil {
		t.Fatal("native fill identity reused across account epochs")
	}
}

func assertSandboxRuntimeCredentialRotationAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	account := sandbox.AccountID("sandbox_runtime-rotation-account")
	seedSandboxRuntimeRotationAccount(t, ctx, pool, account, now)
	seedSandboxRuntimeConsumedAuthorization(
		t,
		ctx,
		pool,
		"sandbox_runtime-rotation-auth",
		strings.Repeat("7", 64),
		userID,
		actorSessionID,
		"credential_rotation",
		strings.Repeat("6", 64),
		strings.Repeat("5", 64),
		now,
	)
	rotation := beginSandboxRuntimeCredentialRotation(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	rotation = validateSandboxRuntimeRotatedIdentity(t, ctx, store, account, rotation, now)
	completeSandboxRuntimeCredentialRotation(t, ctx, store, account, rotation, now)
	assertSandboxRuntimeRotationCompleted(t, ctx, pool, account, userID)
}

func beginSandboxRuntimeCredentialRotation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) sandbox.CredentialRotation {
	t.Helper()
	rotation, err := store.LockForCredentialRotation(ctx, sandbox.CredentialRotationCommand{
		ID: "sandbox_runtime-rotation", AccountID: account, ExpectedRevision: 1,
		AuthorizationID: "sandbox_runtime-rotation-auth", ActorUserID: userID,
		ActorSessionID: actorSessionID, SourceHash: strings.Repeat("6", 64),
		ReasonHash: strings.Repeat("5", 64), Now: now.Add(time.Second),
	})
	if err != nil || rotation.Stage != sandbox.RotationCommanded {
		t.Fatalf("authorized rotation=%#v error=%v", rotation, err)
	}
	assertSandboxRuntimeRotationLocked(t, ctx, pool, account, userID)
	rotation, err = store.MarkExternalSecretReplacement(
		ctx, rotation.ID, rotation.Revision, now.Add(2*time.Second),
	)
	if err != nil || rotation.Stage != sandbox.RotationSecretsReplaced {
		t.Fatalf("external replacement=%#v error=%v", rotation, err)
	}
	return rotation
}

func validateSandboxRuntimeRotatedIdentity(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	rotation sandbox.CredentialRotation,
	now time.Time,
) sandbox.CredentialRotation {
	t.Helper()
	identity := sandbox.AccountIdentity{
		AccountID: account, Exchange: sandbox.ExchangeBybit,
		Environment:          sandbox.EnvironmentBybitDemo,
		AccountIdentityHash:  strings.Repeat("8", 64),
		KeyFingerprint:       strings.Repeat("8", 32),
		CredentialGeneration: 2,
		ValidatedAt:          now.Add(3 * time.Second),
	}
	if _, err := store.ValidateRotatedCredential(
		ctx, rotation.ID, rotation.Revision, identity, identity.ValidatedAt,
	); err == nil {
		t.Fatal("credential rotation accepted a different exchange account")
	}
	identity.AccountIdentityHash = strings.Repeat("7", 64)
	rotation, err := store.ValidateRotatedCredential(
		ctx, rotation.ID, rotation.Revision, identity, identity.ValidatedAt,
	)
	if err != nil || rotation.Stage != sandbox.RotationRestartValidated ||
		rotation.NewGeneration != 2 {
		t.Fatalf("rotation validation=%#v error=%v", rotation, err)
	}
	return rotation
}

func completeSandboxRuntimeCredentialRotation(
	t *testing.T,
	ctx context.Context,
	store *SandboxRuntimeDispatcherStore,
	account sandbox.AccountID,
	rotation sandbox.CredentialRotation,
	now time.Time,
) {
	t.Helper()
	reconciliation := sandbox.ReconciliationResult{
		ID: "sandbox_runtime-rotation-reconciliation", AccountID: account, AccountEpoch: 1,
		State: "clean", EvidenceHash: strings.Repeat("9", 64),
		ReconciledAt: now.Add(4 * time.Second),
	}
	rotation, err := store.RecordRotationReconciliation(
		ctx,
		rotation.ID,
		rotation.Revision,
		reconciliation,
		reconciliation.ReconciledAt,
	)
	if err != nil || rotation.Stage != sandbox.RotationReconciled {
		t.Fatalf("rotation reconciliation=%#v error=%v", rotation, err)
	}
	rotation, err = store.CompleteCredentialRotation(
		ctx, rotation.ID, rotation.Revision, now.Add(5*time.Second),
	)
	if err != nil || rotation.Stage != sandbox.RotationCompleted {
		t.Fatalf("rotation completion=%#v error=%v", rotation, err)
	}
}

func seedSandboxRuntimeRotationAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_exchange_accounts(
 id,exchange,environment,native_account_hash,state,current_epoch,
 credential_generation,revision,created_at,updated_at
) VALUES ($1,'bybit','demo',$2,'READY_PAUSED',1,1,1,$3,$3)`,
		account,
		strings.Repeat("7", 64),
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_generations(
 account_id,generation,key_fingerprint,account_identity_hash,validated_at
) VALUES ($1,1,$2,$3,$4)`,
		account,
		strings.Repeat("7", 32),
		strings.Repeat("7", 64),
		now,
	); err != nil {
		t.Fatal(err)
	}
}

func assertSandboxRuntimeRotationLocked(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	userID string,
) {
	t.Helper()
	var state string
	var audit int
	if err := pool.QueryRow(ctx, `
SELECT state FROM sandbox_runtime_exchange_accounts WHERE id=$1`, account).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_high_risk_audit_events
WHERE outcome='rotation_command_locked' AND actor_user_id=$1`,
		userID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if state != "LOCKED" || audit != 1 {
		t.Fatalf("rotation state=%s audit=%d", state, audit)
	}
}

func assertSandboxRuntimeRotationCompleted(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	userID string,
) {
	t.Helper()
	var state string
	var generation, commandAudits, completionAudits int
	if err := pool.QueryRow(ctx, `
SELECT state,credential_generation
FROM sandbox_runtime_exchange_accounts WHERE id=$1`, account).Scan(&state, &generation); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE outcome='rotation_command_locked'),
       count(*) FILTER (WHERE outcome='rotation_completed_ready_paused')
FROM sandbox_runtime_high_risk_audit_events
WHERE actor_user_id=$1 AND purpose='credential_rotation'`,
		userID,
	).Scan(&commandAudits, &completionAudits); err != nil {
		t.Fatal(err)
	}
	if state != "READY_PAUSED" || generation != 2 ||
		commandAudits != 1 || completionAudits != 1 {
		t.Fatalf(
			"rotation completion state=%s generation=%d audits=%d/%d",
			state,
			generation,
			commandAudits,
			completionAudits,
		)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_credential_rotations
SET stage='COMMAND_LOCKED',revision=revision+1,updated_at=updated_at+interval '1 second'
WHERE id='sandbox_runtime-rotation'`); err == nil {
		t.Fatal("credential rotation state regression was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM sandbox_runtime_credential_rotations WHERE id='sandbox_runtime-rotation'`); err == nil {
		t.Fatal("credential rotation evidence deletion was accepted")
	}
}

func assertSandboxRuntimeRevokeAllAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	store, err := NewSandboxRuntimeAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := strings.Repeat("3", 64)
	reasonHash := strings.Repeat("2", 64)
	activeBefore := assertSandboxRuntimeRevokeAllRequiresAuthorization(
		t, ctx, pool, store, userID, actorSessionID, sourceHash, reasonHash, now,
	)
	seedSandboxRuntimeConsumedAuthorization(
		t,
		ctx,
		pool,
		"sandbox_runtime-revoke-all-auth",
		strings.Repeat("4", 64),
		userID,
		actorSessionID,
		"revoke_all_sessions",
		sourceHash,
		reasonHash,
		now,
	)
	seedSandboxRuntimeRevokedMutationAuthorizations(
		t, ctx, pool, userID, actorSessionID, now.Add(100*time.Millisecond),
	)
	count, err := store.RevokeAllUserSessions(
		ctx,
		"sandbox_runtime-revoke-all-auth",
		userID,
		actorSessionID,
		sourceHash,
		reasonHash,
		now.Add(time.Second),
	)
	if err != nil || count != int64(activeBefore) {
		t.Fatalf("authorized revoke-all count=%d want=%d error=%v", count, activeBefore, err)
	}
	assertSandboxRuntimeRevokeAllEvidence(t, ctx, pool, userID, sourceHash, reasonHash)
	assertSandboxRuntimeRevokedSessionCannotMutate(
		t, ctx, pool, userID, actorSessionID, now.Add(2*time.Second),
	)
}

func assertSandboxRuntimeRevokeAllRequiresAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeAuthenticationStore,
	userID, actorSessionID, sourceHash, reasonHash string,
	now time.Time,
) int {
	t.Helper()
	if _, err := store.RevokeAllUserSessions(
		ctx, "missing-authorization", userID, actorSessionID, sourceHash, reasonHash, now,
	); err == nil {
		t.Fatal("revoke-all without consumed authorization was accepted")
	}
	var active int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, userID).
		Scan(&active); err != nil || active == 0 {
		t.Fatalf("revoke-all rollback active=%d error=%v", active, err)
	}
	return active
}

func assertSandboxRuntimeRevokeAllEvidence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, sourceHash, reasonHash string,
) {
	t.Helper()
	var activeAfter, controls, audits int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`,
		userID,
	).Scan(&activeAfter); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_session_control_events
WHERE authorization_id='sandbox_runtime-revoke-all-auth'
  AND source_hash=$1 AND reason_hash=$2`,
		sourceHash,
		reasonHash,
	).Scan(&controls); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_runtime_high_risk_audit_events
WHERE purpose='revoke_all_sessions' AND outcome='sessions_revoked'
  AND before_hash IS NOT NULL AND after_hash IS NOT NULL
  AND before_hash<>after_hash`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if activeAfter != 0 || controls != 1 || audits != 1 {
		t.Fatalf(
			"revoke-all active=%d controls=%d audits=%d",
			activeAfter,
			controls,
			audits,
		)
	}
}

func seedSandboxRuntimeRevokedMutationAuthorizations(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	for index, authorization := range []struct {
		id, purpose, token, source, reason string
	}{
		{
			id: "sandbox_runtime-revoked-arm-auth", purpose: "sandbox_arm",
			token: strings.Repeat("c", 64), source: strings.Repeat("6", 64),
			reason: strings.Repeat("7", 64),
		},
		{
			id: "sandbox_runtime-revoked-risk-auth", purpose: "risk_unlock",
			token: strings.Repeat("d", 64), source: strings.Repeat("7", 64),
			reason: strings.Repeat("8", 64),
		},
		{
			id: "sandbox_runtime-revoked-rotation-auth", purpose: "credential_rotation",
			token: strings.Repeat("e", 64), source: strings.Repeat("8", 64),
			reason: strings.Repeat("9", 64),
		},
	} {
		seedSandboxRuntimeConsumedAuthorization(
			t,
			ctx,
			pool,
			authorization.id,
			authorization.token,
			userID,
			actorSessionID,
			authorization.purpose,
			authorization.source,
			authorization.reason,
			now.Add(time.Duration(index)*time.Nanosecond),
		)
	}
}

func assertSandboxRuntimeRevokedSessionCannotMutate(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	assertSandboxRuntimeRevokedSessionCannotArm(t, ctx, pool, userID, actorSessionID, now)
	assertSandboxRuntimeRevokedSessionCannotUnlock(t, ctx, pool, userID, actorSessionID, now)
	assertSandboxRuntimeRevokedSessionCannotRotate(t, ctx, pool, userID, actorSessionID, now)
}

func assertSandboxRuntimeRevokedSessionCannotArm(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'sandbox_runtime-revoked-session-arm','sandbox_runtime-control-session','sandbox_runtime-revoked-arm-auth',$1,$2,
 $3,$4,$5,$6,1
)`,
		userID,
		actorSessionID,
		strings.Repeat("6", 64),
		strings.Repeat("7", 64),
		now,
		now.Add(sandbox.ArmLifetime),
	); err == nil {
		t.Fatal("revoked session created a sandbox arm")
	}
}

func assertSandboxRuntimeRevokedSessionCannotUnlock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_risk_unlocks(
 id,account_id,account_epoch,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,reconciliation_id,prior_state,resulting_state,unlocked_at,revision
) VALUES (
 'sandbox_runtime-revoked-session-risk','sandbox_runtime-control-account',1,'sandbox_runtime-revoked-risk-auth',$1,$2,
 $3,$4,'sandbox_runtime-control-reconciliation','LOCKED','READY_PAUSED',$5,1
)`,
		userID,
		actorSessionID,
		strings.Repeat("7", 64),
		strings.Repeat("8", 64),
		now,
	); err == nil {
		t.Fatal("revoked session unlocked sandbox risk")
	}
}

func assertSandboxRuntimeRevokedSessionCannotRotate(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_credential_rotations(
 id,account_id,authorization_id,actor_user_id,actor_session_id,source_hash,reason_hash,
 stage,prior_generation,prior_fingerprint,nonterminal_quarantined,started_at,updated_at,revision
) VALUES (
 'sandbox_runtime-revoked-session-rotation','sandbox_runtime-control-account','sandbox_runtime-revoked-rotation-auth',$1,$2,
 $3,$4,'COMMAND_LOCKED',1,$5,false,$6,$6,1
)`,
		userID,
		actorSessionID,
		strings.Repeat("8", 64),
		strings.Repeat("9", 64),
		strings.Repeat("2", 32),
		now,
	); err == nil {
		t.Fatal("revoked session started credential rotation")
	}
}
