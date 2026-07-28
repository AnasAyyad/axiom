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

func assertV1CControlRecoveryAndReset(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	now := time.Date(2026, 7, 28, 0, 20, 0, 0, time.UTC)
	userID, actorSessionID := v1CQualificationOwner(t, ctx, pool)
	store, err := NewV1CDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	account := sandbox.AccountID("v1c-control-account")
	seedV1CControlAccount(t, ctx, pool, account, userID, now)
	assertV1CArmFlow(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	assertV1CRiskUnlockFlow(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	assertV1CAccountSnapshotAndReset(
		t, ctx, pool, store, account, now.Add(5*time.Second),
	)
	assertV1CCredentialRotationAuthorization(
		t, ctx, pool, store, userID, actorSessionID, now.Add(10*time.Second),
	)
	assertV1CRevokeAllAuthorization(
		t, ctx, pool, userID, actorSessionID, now.Add(20*time.Second),
	)
}

func assertV1CArmFlow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	seedV1CConsumedAuthorization(
		t,
		ctx,
		pool,
		"v1c-control-arm-auth",
		strings.Repeat("9", 64),
		userID,
		actorSessionID,
		"sandbox_arm",
		strings.Repeat("a", 64),
		strings.Repeat("b", 64),
		now,
	)
	arm := sandbox.Arm{
		ID: "v1c-control-arm", SessionID: "v1c-control-session",
		AccountIDs:        []sandbox.AccountID{account},
		AuthorizationHash: strings.Repeat("c", 64),
		ActorUserID:       userID, ActorSessionID: actorSessionID,
		ReasonHash: strings.Repeat("b", 64),
		CreatedAt:  now.Add(time.Second),
		ExpiresAt:  now.Add(time.Second).Add(sandbox.ArmLifetime),
		Revision:   1,
	}
	if _, err := store.CreateSandboxArm(ctx, sandbox.ArmCommand{
		Arm: arm, AuthorizationID: "v1c-control-arm-auth",
		SourceHash: strings.Repeat("a", 64), ExpectedSessionRevision: 1,
	}); err != nil {
		t.Fatalf("authorized arm failed: %v", err)
	}
	assertV1CArmState(t, ctx, pool, account)
}

func assertV1CRiskUnlockFlow(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE v1c_exchange_accounts
SET state='LOCKED',revision=revision+1,updated_at=$2
WHERE id=$1`, account, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	reconciliation := sandbox.ReconciliationResult{
		ID: "v1c-control-reconciliation", AccountID: account, AccountEpoch: 1,
		State: "clean", EvidenceHash: strings.Repeat("d", 64),
		ReconciledAt: now.Add(2 * time.Second),
	}
	if err := store.RecordReconciliation(ctx, reconciliation); err != nil {
		t.Fatal(err)
	}
	seedV1CConsumedAuthorization(
		t,
		ctx,
		pool,
		"v1c-control-risk-auth",
		strings.Repeat("8", 64),
		userID,
		actorSessionID,
		"risk_unlock",
		strings.Repeat("e", 64),
		strings.Repeat("f", 64),
		now.Add(3*time.Second),
	)
	if err := store.RiskUnlock(ctx, sandbox.RiskUnlockCommand{
		ID: "v1c-control-risk-unlock", AccountID: account, ExpectedRevision: 3,
		AuthorizationID: "v1c-control-risk-auth", ActorUserID: userID,
		ActorSessionID: actorSessionID, SourceHash: strings.Repeat("e", 64),
		ReasonHash:       strings.Repeat("f", 64),
		ReconciliationID: reconciliation.ID, ReconciliationEpoch: 1,
		Now: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatalf("authorized risk unlock failed: %v", err)
	}
	assertV1CRiskUnlockState(t, ctx, pool, account)
}

func seedV1CControlAccount(
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
	seedV1CControlAccountIdentity(t, ctx, pool, account, now)
	seedV1CControlSession(t, ctx, pool, account, userID, now)
}

func seedV1CControlAccountIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_accounts(
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
INSERT INTO v1c_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_credential_generations(
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

func seedV1CControlSession(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	userID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_sessions(
 id,state,configuration_id,strategy_set_hash,revision,created_by,created_at,updated_at
) VALUES ('v1c-control-session','READY_PAUSED','v1c-control-config',$1,1,$2,$3,$3)`,
		strings.Repeat("3", 64),
		userID,
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_session_accounts(session_id,account_id,account_epoch)
VALUES ('v1c-control-session',$1,1)`, account); err != nil {
		t.Fatal(err)
	}
}

func seedV1CConsumedAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id, tokenHash, userID, sessionID, purpose, sourceHash, reasonHash string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_authorizations(
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

func assertV1CArmState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
) {
	t.Helper()
	var accountState, sessionState string
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_exchange_accounts WHERE id=$1`, account).Scan(&accountState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_sessions WHERE id='v1c-control-session'`,
	).Scan(&sessionState); err != nil {
		t.Fatal(err)
	}
	if accountState != "ARMED" || sessionState != "ARMED" {
		t.Fatalf("arm state account=%s session=%s", accountState, sessionState)
	}
}

func assertV1CRiskUnlockState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
) {
	t.Helper()
	var state string
	var unlocks, audits int
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_exchange_accounts WHERE id=$1`, account).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_risk_unlocks WHERE account_id=$1`, account).Scan(&unlocks); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_high_risk_audit_events
WHERE outcome IN ('sandbox_armed','risk_unlocked_ready_paused')`,
	).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if state != "READY_PAUSED" || unlocks != 1 || audits != 2 {
		t.Fatalf("risk unlock state=%s rows=%d audits=%d", state, unlocks, audits)
	}
}

func assertV1CAccountSnapshotAndReset(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	recordV1CAccountSnapshot(t, ctx, store, account, now)
	recordV1CAccountReset(t, ctx, store, account, now)
	assertV1CResetState(t, ctx, pool, account)
	assertV1CNativeFillIdentityCrossEpoch(t, ctx, pool, account, now)
}

func recordV1CAccountSnapshot(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	available, _ := domain.ParseBalance("20")
	reserved, _ := domain.ParseBalance("0")
	if err := store.RecordAccountSnapshot(ctx, "v1c-control-snapshot", sandbox.AccountSnapshot{
		AccountID: account, Epoch: 1,
		Balances: []sandbox.Balance{{
			Asset: "USDT", Available: available, Reserved: reserved,
		}},
		OrdersHash:   strings.Repeat("1", 64),
		FillsHash:    strings.Repeat("2", 64),
		SnapshotHash: strings.Repeat("3", 64),
		ObservedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
}

func recordV1CAccountReset(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
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
		ID: "v1c-control-reset", AccountID: account, PriorEpoch: 1,
		EvidenceHash: strings.Repeat("4", 64), DetectedAt: now.Add(time.Second),
		Adjustments: []sandbox.ExternalAdjustment{{
			ID: "v1c-control-adjustment", Asset: "USDT", Quantity: "-20",
			AdjustmentHash: strings.Repeat("5", 64),
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func assertV1CResetState(
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
SELECT current_epoch,state FROM v1c_exchange_accounts WHERE id=$1`,
		account).Scan(&epoch, &state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT revoked_at IS NOT NULL FROM v1c_sandbox_arms WHERE id='v1c-control-arm'`,
	).Scan(&armRevoked); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_sessions WHERE id='v1c-control-session'`,
	).Scan(&sessionState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT pnl_effect FROM v1c_external_adjustments
WHERE id='v1c-control-adjustment'`).Scan(&pnlEffect); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_account_leases WHERE account_id=$1`,
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

func assertV1CNativeFillIdentityCrossEpoch(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	fillHash := strings.Repeat("6", 64)
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_fills(
 account_id,account_epoch,native_fill_id_hash,order_id,canonical_fill,occurred_at
) VALUES ($1,1,$2,'order-old','{}'::jsonb,$3)`,
		account,
		fillHash,
		now,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_fills(
 account_id,account_epoch,native_fill_id_hash,order_id,canonical_fill,occurred_at
) VALUES ($1,2,$2,'order-new','{}'::jsonb,$3)`,
		account,
		fillHash,
		now.Add(time.Second),
	); err == nil {
		t.Fatal("native fill identity reused across account epochs")
	}
}

func assertV1CCredentialRotationAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	account := sandbox.AccountID("v1c-rotation-account")
	seedV1CRotationAccount(t, ctx, pool, account, now)
	seedV1CConsumedAuthorization(
		t,
		ctx,
		pool,
		"v1c-rotation-auth",
		strings.Repeat("7", 64),
		userID,
		actorSessionID,
		"credential_rotation",
		strings.Repeat("6", 64),
		strings.Repeat("5", 64),
		now,
	)
	rotation := beginV1CCredentialRotation(
		t, ctx, pool, store, account, userID, actorSessionID, now,
	)
	rotation = validateV1CRotatedIdentity(t, ctx, store, account, rotation, now)
	completeV1CCredentialRotation(t, ctx, store, account, rotation, now)
	assertV1CRotationCompleted(t, ctx, pool, account, userID)
}

func beginV1CCredentialRotation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	userID, actorSessionID string,
	now time.Time,
) sandbox.CredentialRotation {
	t.Helper()
	rotation, err := store.LockForCredentialRotation(ctx, sandbox.CredentialRotationCommand{
		ID: "v1c-rotation", AccountID: account, ExpectedRevision: 1,
		AuthorizationID: "v1c-rotation-auth", ActorUserID: userID,
		ActorSessionID: actorSessionID, SourceHash: strings.Repeat("6", 64),
		ReasonHash: strings.Repeat("5", 64), Now: now.Add(time.Second),
	})
	if err != nil || rotation.Stage != sandbox.RotationCommanded {
		t.Fatalf("authorized rotation=%#v error=%v", rotation, err)
	}
	assertV1CRotationLocked(t, ctx, pool, account, userID)
	rotation, err = store.MarkExternalSecretReplacement(
		ctx, rotation.ID, rotation.Revision, now.Add(2*time.Second),
	)
	if err != nil || rotation.Stage != sandbox.RotationSecretsReplaced {
		t.Fatalf("external replacement=%#v error=%v", rotation, err)
	}
	return rotation
}

func validateV1CRotatedIdentity(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
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

func completeV1CCredentialRotation(
	t *testing.T,
	ctx context.Context,
	store *V1CDispatcherStore,
	account sandbox.AccountID,
	rotation sandbox.CredentialRotation,
	now time.Time,
) {
	t.Helper()
	reconciliation := sandbox.ReconciliationResult{
		ID: "v1c-rotation-reconciliation", AccountID: account, AccountEpoch: 1,
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

func seedV1CRotationAccount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	account sandbox.AccountID,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_exchange_accounts(
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
INSERT INTO v1c_account_epochs(account_id,epoch,reason,opened_at)
VALUES ($1,1,'initial',$2)`, account, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_credential_generations(
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

func assertV1CRotationLocked(
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
SELECT state FROM v1c_exchange_accounts WHERE id=$1`, account).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_high_risk_audit_events
WHERE outcome='rotation_command_locked' AND actor_user_id=$1`,
		userID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if state != "LOCKED" || audit != 1 {
		t.Fatalf("rotation state=%s audit=%d", state, audit)
	}
}

func assertV1CRotationCompleted(
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
FROM v1c_exchange_accounts WHERE id=$1`, account).Scan(&state, &generation); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE outcome='rotation_command_locked'),
       count(*) FILTER (WHERE outcome='rotation_completed_ready_paused')
FROM v1c_high_risk_audit_events
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
UPDATE v1c_credential_rotations
SET stage='COMMAND_LOCKED',revision=revision+1,updated_at=updated_at+interval '1 second'
WHERE id='v1c-rotation'`); err == nil {
		t.Fatal("credential rotation state regression was accepted")
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM v1c_credential_rotations WHERE id='v1c-rotation'`); err == nil {
		t.Fatal("credential rotation evidence deletion was accepted")
	}
}

func assertV1CRevokeAllAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	store, err := NewV1CAuthenticationStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	sourceHash := strings.Repeat("3", 64)
	reasonHash := strings.Repeat("2", 64)
	activeBefore := assertV1CRevokeAllRequiresAuthorization(
		t, ctx, pool, store, userID, actorSessionID, sourceHash, reasonHash, now,
	)
	seedV1CConsumedAuthorization(
		t,
		ctx,
		pool,
		"v1c-revoke-all-auth",
		strings.Repeat("4", 64),
		userID,
		actorSessionID,
		"revoke_all_sessions",
		sourceHash,
		reasonHash,
		now,
	)
	seedV1CRevokedMutationAuthorizations(
		t, ctx, pool, userID, actorSessionID, now.Add(100*time.Millisecond),
	)
	count, err := store.RevokeAllUserSessions(
		ctx,
		"v1c-revoke-all-auth",
		userID,
		actorSessionID,
		sourceHash,
		reasonHash,
		now.Add(time.Second),
	)
	if err != nil || count != int64(activeBefore) {
		t.Fatalf("authorized revoke-all count=%d want=%d error=%v", count, activeBefore, err)
	}
	assertV1CRevokeAllEvidence(t, ctx, pool, userID, sourceHash, reasonHash)
	assertV1CRevokedSessionCannotMutate(
		t, ctx, pool, userID, actorSessionID, now.Add(2*time.Second),
	)
}

func assertV1CRevokeAllRequiresAuthorization(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *V1CAuthenticationStore,
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

func assertV1CRevokeAllEvidence(
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
SELECT count(*) FROM v1c_session_control_events
WHERE authorization_id='v1c-revoke-all-auth'
  AND source_hash=$1 AND reason_hash=$2`,
		sourceHash,
		reasonHash,
	).Scan(&controls); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM v1c_high_risk_audit_events
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

func seedV1CRevokedMutationAuthorizations(
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
			id: "v1c-revoked-arm-auth", purpose: "sandbox_arm",
			token: strings.Repeat("c", 64), source: strings.Repeat("6", 64),
			reason: strings.Repeat("7", 64),
		},
		{
			id: "v1c-revoked-risk-auth", purpose: "risk_unlock",
			token: strings.Repeat("d", 64), source: strings.Repeat("7", 64),
			reason: strings.Repeat("8", 64),
		},
		{
			id: "v1c-revoked-rotation-auth", purpose: "credential_rotation",
			token: strings.Repeat("e", 64), source: strings.Repeat("8", 64),
			reason: strings.Repeat("9", 64),
		},
	} {
		seedV1CConsumedAuthorization(
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

func assertV1CRevokedSessionCannotMutate(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	assertV1CRevokedSessionCannotArm(t, ctx, pool, userID, actorSessionID, now)
	assertV1CRevokedSessionCannotUnlock(t, ctx, pool, userID, actorSessionID, now)
	assertV1CRevokedSessionCannotRotate(t, ctx, pool, userID, actorSessionID, now)
}

func assertV1CRevokedSessionCannotArm(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_sandbox_arms(
 id,sandbox_session_id,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,created_at,expires_at,revision
) VALUES (
 'v1c-revoked-session-arm','v1c-control-session','v1c-revoked-arm-auth',$1,$2,
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

func assertV1CRevokedSessionCannotUnlock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_risk_unlocks(
 id,account_id,account_epoch,authorization_id,actor_user_id,actor_session_id,
 source_hash,reason_hash,reconciliation_id,prior_state,resulting_state,unlocked_at,revision
) VALUES (
 'v1c-revoked-session-risk','v1c-control-account',1,'v1c-revoked-risk-auth',$1,$2,
 $3,$4,'v1c-control-reconciliation','LOCKED','READY_PAUSED',$5,1
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

func assertV1CRevokedSessionCannotRotate(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	userID, actorSessionID string,
	now time.Time,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO v1c_credential_rotations(
 id,account_id,authorization_id,actor_user_id,actor_session_id,source_hash,reason_hash,
 stage,prior_generation,prior_fingerprint,nonterminal_quarantined,started_at,updated_at,revision
) VALUES (
 'v1c-revoked-session-rotation','v1c-control-account','v1c-revoked-rotation-auth',$1,$2,
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
