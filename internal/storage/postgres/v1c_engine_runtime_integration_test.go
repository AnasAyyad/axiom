package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertV1CEngineRuntimePersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newV1CEngineRuntimeFixture(t, ctx, pool)
	assertV1CEngineStartupEvidence(t, fixture)
	assertV1CEngineObservationFencing(t, fixture)
	assertV1CPrivateStreamRuntimeEvidence(t, fixture)
	assertV1CEngineCommandFencing(t, fixture)
}

func assertV1CPrivateStreamRuntimeEvidence(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	occurredAt := fixture.now.Add(10 * time.Second)
	err := fixture.store.RecordEngineRuntimeRecoveryEvent(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		fixture.account.Exchange, fixture.fence, "PRIVATE_STREAM",
		25*time.Millisecond, false, exchangecontracts.ErrorTransient,
		"private_stream_receive_failed", occurredAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	var kind, failureKind, causeCode, evidenceHash string
	var succeeded bool
	if err = fixture.pool.QueryRow(fixture.ctx, `
SELECT kind,succeeded,failure_kind,cause_code,evidence_hash
FROM v1c_engine_runtime_events
WHERE account_id=$1 AND occurred_at=$2`, fixture.account.AccountID, occurredAt).
		Scan(&kind, &succeeded, &failureKind, &causeCode, &evidenceHash); err != nil ||
		kind != "PRIVATE_STREAM" || succeeded ||
		failureKind != "transient_outage" ||
		causeCode != "private_stream_receive_failed" || len(evidenceHash) != 64 {
		t.Fatalf("private stream evidence=%s/%t/%s/%s/%s error=%v",
			kind, succeeded, failureKind, causeCode, evidenceHash, err)
	}
	if _, err = fixture.pool.Exec(fixture.ctx, `
UPDATE v1c_engine_runtime_events SET cause_code='mutated'
WHERE account_id=$1 AND occurred_at=$2`, fixture.account.AccountID, occurredAt); err == nil {
		t.Fatal("private stream runtime evidence mutation was accepted")
	}
}

type v1cEngineRuntimeFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *V1CDispatcherStore
	now     time.Time
	account V1CEngineAccount
	created V1CEngineAccount
	fence   uint64
}

func newV1CEngineRuntimeFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) v1cEngineRuntimeFixture {
	t.Helper()
	store, err := NewV1CDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 28, 0, 5, 0, 0, time.UTC)
	account := v1cRuntimeAccount()
	created, err := store.EnsureAttestedAccount(ctx, account, now)
	if err != nil || created.Epoch != 1 ||
		created.State != sandbox.EngineLocked {
		t.Fatalf("engine account=%#v error=%v", created, err)
	}
	identity := v1cRuntimeIdentity(account, now)
	if err = store.RecordValidatedEngineIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	fence, err := store.AcquireAccountLease(
		ctx, account.AccountID, account.Environment,
		"v1c-engine-runtime-worker", now.Add(2*time.Second),
		time.Minute, sandbox.NoKillPoint{},
	)
	if err != nil || fence == 0 {
		t.Fatalf("engine lease=%d error=%v", fence, err)
	}
	return v1cEngineRuntimeFixture{
		ctx: ctx, pool: pool, store: store, now: now,
		account: account, created: created, fence: fence,
	}
}

func v1cRuntimeAccount() V1CEngineAccount {
	return V1CEngineAccount{
		AccountID:            "v1c-engine-runtime-account",
		Exchange:             sandbox.ExchangeBinance,
		Environment:          sandbox.EnvironmentBinanceSpotTestnet,
		AccountIdentityHash:  strings.Repeat("a", 64),
		CredentialGeneration: 1,
		State:                sandbox.EngineLocked,
	}
}

func v1cRuntimeIdentity(
	account V1CEngineAccount,
	now time.Time,
) sandbox.AccountIdentity {
	return sandbox.AccountIdentity{
		AccountID: account.AccountID, Exchange: account.Exchange,
		Environment:          account.Environment,
		AccountIdentityHash:  account.AccountIdentityHash,
		KeyFingerprint:       strings.Repeat("b", 32),
		CredentialGeneration: 1, OwnerAttested: true,
		ValidatedAt: now.Add(time.Second),
	}
}

func assertV1CEngineStartupEvidence(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	sink, err := NewV1CEngineStartupEvidenceSink(
		fixture.store, fixture.account.AccountID,
		fixture.account.Exchange, fixture.fence,
	)
	if err != nil {
		t.Fatal(err)
	}
	evidence := exchangecontracts.CollectorLifecycleEvidence{
		ObservedAt:  fixture.now.Add(3 * time.Second),
		Exchange:    string(fixture.account.Exchange),
		Instrument:  "account",
		Cycle:       fixture.fence,
		Attempt:     1,
		Phase:       "sandbox_startup",
		Stage:       string(sandbox.StartupAcquireLease),
		Action:      exchangecontracts.RecoveryReconnect,
		Attribution: exchangecontracts.AttributionRecovered,
	}
	if err = sink.AppendCollectorLifecycle(evidence); err != nil {
		t.Fatal(err)
	}
	if err = sink.AppendCollectorLifecycle(evidence); err != nil {
		t.Fatalf("exact startup evidence replay failed: %v", err)
	}
	if err = fixture.store.VerifyEngineRecoveryState(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
	); err != nil {
		t.Fatal(err)
	}
	if _, err = fixture.pool.Exec(fixture.ctx, `
UPDATE v1c_engine_startup_evidence
SET reached_healthy=true WHERE account_id=$1`,
		fixture.account.AccountID,
	); err == nil {
		t.Fatal("startup evidence mutation was accepted")
	}
}

func assertV1CEngineObservationFencing(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	err := fixture.store.SetEngineAccountState(
		fixture.ctx, fixture.account.AccountID,
		sandbox.EngineReadyPaused,
		fixture.now.Add(4*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.store.RenewAccountLease(
		fixture.ctx, fixture.account.AccountID,
		"v1c-engine-runtime-worker", fixture.fence,
		fixture.now.Add(5*time.Second), time.Minute,
	); err != nil {
		t.Fatal(err)
	}
	eligibility := exchangecontracts.CollectorHealthSnapshot{
		ObservedAt: fixture.now.Add(6 * time.Second),
		Exchange:   string(fixture.account.Exchange), Instrument: "BTCUSDT",
		BookHealth: "healthy", BookHealthy: true, BookFresh: true,
		BookEligible: true, ClockEligible: true, Eligible: true,
	}
	if err = fixture.store.RecordEngineObservation(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		fixture.account.Exchange, fixture.fence, eligibility,
	); err != nil {
		t.Fatal(err)
	}
	if err = fixture.store.RecordEngineObservation(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		fixture.account.Exchange, fixture.fence+1, eligibility,
	); err == nil {
		t.Fatal("wrong fence wrote an engine observation")
	}
	assertV1CEngineDegradedState(t, fixture)
	if err = fixture.store.RenewAccountLease(
		fixture.ctx, fixture.account.AccountID,
		"wrong-owner", fixture.fence,
		fixture.now.Add(6*time.Second), time.Minute,
	); err == nil {
		t.Fatal("wrong owner renewed the engine lease")
	}
}

func assertV1CEngineDegradedState(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	err := fixture.store.SetEngineAccountState(
		fixture.ctx, fixture.account.AccountID,
		sandbox.EngineDegraded,
		fixture.now.Add(7*time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err = fixture.store.SetEngineAccountState(
		fixture.ctx, "", sandbox.EngineDegraded,
		fixture.now.Add(8*time.Second),
	); err == nil {
		t.Fatal("empty account accepted a degraded transition")
	}
	var accountState sandbox.EngineState
	if err = fixture.pool.QueryRow(fixture.ctx, `
SELECT state FROM v1c_exchange_accounts WHERE id=$1`,
		fixture.account.AccountID,
	).Scan(&accountState); err != nil ||
		accountState != sandbox.EngineDegraded {
		t.Fatalf("degraded account state=%s error=%v", accountState, err)
	}
	if err = fixture.store.SetEngineAccountState(
		fixture.ctx, fixture.account.AccountID,
		sandbox.EngineReadyPaused,
		fixture.now.Add(9*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if err = fixture.pool.QueryRow(fixture.ctx, `
SELECT state FROM v1c_exchange_accounts WHERE id=$1`,
		fixture.account.AccountID,
	).Scan(&accountState); err != nil ||
		accountState != sandbox.EngineReadyPaused {
		t.Fatalf("recovered account state=%s error=%v", accountState, err)
	}
}

func assertV1CEngineCommandFencing(
	t *testing.T,
	fixture v1cEngineRuntimeFixture,
) {
	t.Helper()
	command := sandbox.EngineCommand{
		ID:            "v1c-engine-runtime-query",
		AccountID:     fixture.account.AccountID,
		AccountEpoch:  fixture.created.Epoch,
		Kind:          sandbox.EngineCommandQuery,
		ClientOrderID: "ax-v1c-runtime-query",
		RequestedAt:   fixture.now.Add(7 * time.Second),
	}
	if err := fixture.store.QueueEngineCommand(fixture.ctx, command); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.QueueEngineCommand(fixture.ctx, command); err != nil {
		t.Fatalf("exact command replay failed: %v", err)
	}
	commands, err := fixture.store.ClaimEngineCommands(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		"v1c-engine-runtime-worker", fixture.fence,
		fixture.now.Add(8*time.Second), 1,
	)
	if err != nil || len(commands) != 1 ||
		commands[0].ID != command.ID {
		t.Fatalf("claimed commands=%#v error=%v", commands, err)
	}
	if err = fixture.store.CompleteEngineCommand(
		fixture.ctx, command.ID, fixture.fence+1, true,
		strings.Repeat("c", 64), fixture.now.Add(9*time.Second),
	); err == nil {
		t.Fatal("wrong fence completed an engine command")
	}
	if err = fixture.store.CompleteEngineCommand(
		fixture.ctx, command.ID, fixture.fence, true,
		strings.Repeat("c", 64), fixture.now.Add(9*time.Second),
	); err != nil {
		t.Fatal(err)
	}
}

func assertV1CCanarySessionPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	store, err := NewV1CDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `
SELECT id FROM users WHERE status='active' ORDER BY id LIMIT 1`,
	).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(
		2026, 7, 28, 0, 5, 6, 100_000_000, time.UTC,
	)
	command := sandbox.CanarySessionCommand{
		ID:       "v1c-canary-runtime-session",
		Exchange: sandbox.ExchangeBinance, Instrument: "BTCUSDT",
		ConfigurationID: "v1c-canary-runtime-config",
		StrategySetHash: strings.Repeat("d", 64),
		CreatedBy:       owner, CreatedAt: createdAt,
	}
	session, err := store.CreateCanarySession(ctx, command)
	if err != nil || session.AccountID != "v1c-engine-runtime-account" ||
		session.AccountEpoch != 1 || session.StartupCycle == 0 ||
		session.Revision != 1 {
		t.Fatalf("canary session=%#v error=%v", session, err)
	}
	command.ID = "v1c-canary-runtime-session-duplicate"
	if _, err = store.CreateCanarySession(ctx, command); err == nil {
		t.Fatal("second active canary session reused one account epoch")
	}
	if err = store.StopCanarySession(
		ctx,
		session.ID,
		session.AccountID,
		false,
		createdAt.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
}
