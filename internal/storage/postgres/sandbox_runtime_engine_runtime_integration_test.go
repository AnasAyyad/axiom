package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"axiom/internal/api/generated"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"

	"github.com/jackc/pgx/v5/pgxpool"
)

func assertSandboxRuntimeEngineRuntimePersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newSandboxRuntimeEngineRuntimeFixture(t, ctx, pool)
	assertSandboxRuntimeEngineStartupEvidence(t, fixture)
	assertSandboxRuntimeEngineObservationFencing(t, fixture)
	assertSandboxRuntimeEngineCommandFencing(t, fixture)
}

type sandboxRuntimeEngineRuntimeFixture struct {
	ctx     context.Context
	pool    *pgxpool.Pool
	store   *SandboxRuntimeDispatcherStore
	now     time.Time
	account SandboxRuntimeEngineAccount
	created SandboxRuntimeEngineAccount
	fence   uint64
}

func newSandboxRuntimeEngineRuntimeFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) sandboxRuntimeEngineRuntimeFixture {
	t.Helper()
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 28, 0, 5, 0, 0, time.UTC)
	account := sandboxRuntimeRuntimeAccount()
	created, err := store.EnsureAttestedAccount(ctx, account, now)
	if err != nil || created.Epoch != 1 ||
		created.State != sandbox.EngineLocked {
		t.Fatalf("engine account=%#v error=%v", created, err)
	}
	identity := sandboxRuntimeRuntimeIdentity(account, now)
	if err = store.RecordValidatedEngineIdentity(ctx, identity); err != nil {
		t.Fatal(err)
	}
	fence, err := store.AcquireAccountLease(
		ctx, account.AccountID, account.Environment,
		"sandbox_runtime-engine-runtime-worker", now.Add(2*time.Second),
		time.Minute, sandbox.NoKillPoint{},
	)
	if err != nil || fence == 0 {
		t.Fatalf("engine lease=%d error=%v", fence, err)
	}
	return sandboxRuntimeEngineRuntimeFixture{
		ctx: ctx, pool: pool, store: store, now: now,
		account: account, created: created, fence: fence,
	}
}

func sandboxRuntimeRuntimeAccount() SandboxRuntimeEngineAccount {
	return SandboxRuntimeEngineAccount{
		AccountID:            "sandbox_runtime-engine-runtime-account",
		Exchange:             sandbox.ExchangeBinance,
		Environment:          sandbox.EnvironmentBinanceSpotTestnet,
		AccountIdentityHash:  strings.Repeat("a", 64),
		CredentialGeneration: 1,
		State:                sandbox.EngineLocked,
	}
}

func sandboxRuntimeRuntimeIdentity(
	account SandboxRuntimeEngineAccount,
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

func assertSandboxRuntimeEngineStartupEvidence(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
) {
	t.Helper()
	sink, err := NewSandboxRuntimeEngineStartupEvidenceSink(
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
UPDATE sandbox_runtime_engine_startup_evidence
SET reached_healthy=true WHERE account_id=$1`,
		fixture.account.AccountID,
	); err == nil {
		t.Fatal("startup evidence mutation was accepted")
	}
}

func assertSandboxRuntimeEngineObservationFencing(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
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
		"sandbox_runtime-engine-runtime-worker", fixture.fence,
		fixture.now.Add(5*time.Second), time.Minute,
	); err != nil {
		t.Fatal(err)
	}
	eligibilities := sandboxRuntimeEngineObservationFixtures(fixture)
	if err = fixture.store.RecordEngineObservations(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		fixture.account.Exchange, fixture.fence,
		eligibilities,
	); err != nil {
		t.Fatal(err)
	}
	if err = fixture.store.RecordEngineObservations(
		fixture.ctx, fixture.account.AccountID, fixture.created.Epoch,
		fixture.account.Exchange, fixture.fence+1,
		eligibilities[:1],
	); err == nil {
		t.Fatal("wrong fence wrote an engine observation")
	}
	assertSandboxRuntimeEngineDegradedState(t, fixture)
	if err = fixture.store.RenewAccountLease(
		fixture.ctx, fixture.account.AccountID,
		"wrong-owner", fixture.fence,
		fixture.now.Add(6*time.Second), time.Minute,
	); err == nil {
		t.Fatal("wrong owner renewed the engine lease")
	}
}

func sandboxRuntimeEngineObservationFixtures(fixture sandboxRuntimeEngineRuntimeFixture) []exchangecontracts.CollectorHealthSnapshot {
	result := make([]exchangecontracts.CollectorHealthSnapshot, 0, 3)
	for _, instrument := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		result = append(result, exchangecontracts.CollectorHealthSnapshot{
			ObservedAt: fixture.now.Add(6 * time.Second), Exchange: string(fixture.account.Exchange),
			Instrument: instrument, BookHealth: "healthy", BookHealthy: true, BookFresh: true,
			BookEligible: true, ClockEligible: true, Eligible: true})
	}
	return result
}

func assertSandboxRuntimeEngineDegradedState(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
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
SELECT state FROM sandbox_runtime_exchange_accounts WHERE id=$1`,
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
SELECT state FROM sandbox_runtime_exchange_accounts WHERE id=$1`,
		fixture.account.AccountID,
	).Scan(&accountState); err != nil ||
		accountState != sandbox.EngineReadyPaused {
		t.Fatalf("recovered account state=%s error=%v", accountState, err)
	}
}

func assertSandboxRuntimeEngineCommandFencing(
	t *testing.T,
	fixture sandboxRuntimeEngineRuntimeFixture,
) {
	t.Helper()
	command := sandbox.EngineCommand{
		ID:            "sandbox_runtime-engine-runtime-query",
		AccountID:     fixture.account.AccountID,
		AccountEpoch:  fixture.created.Epoch,
		Kind:          sandbox.EngineCommandQuery,
		ClientOrderID: "ax-sandbox-runtime-runtime-query",
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
		"sandbox_runtime-engine-runtime-worker", fixture.fence,
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

func assertSandboxRuntimeCanarySessionPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	store, err := NewSandboxRuntimeDispatcherStore(pool)
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
		ID:       "sandbox_runtime-canary-runtime-session",
		Exchange: sandbox.ExchangeBinance, Instrument: "BTCUSDT",
		ConfigurationID: "sandbox_runtime-canary-runtime-config",
		StrategySetHash: strings.Repeat("d", 64),
		CreatedBy:       owner, CreatedAt: createdAt,
	}
	session, err := store.CreateCanarySession(ctx, command)
	if err != nil || session.AccountID != "sandbox_runtime-engine-runtime-account" ||
		session.AccountEpoch != 1 || session.StartupCycle == 0 ||
		session.Revision != 1 {
		t.Fatalf("canary session=%#v error=%v", session, err)
	}
	command.ID = "sandbox_runtime-canary-runtime-session-duplicate"
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

func assertSandboxRuntimeStrategySessionPersistence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	fixture := newSandboxRuntimeStrategyPersistenceFixture(t, ctx, pool)
	store, command, session := fixture.store, fixture.command, fixture.session
	createdAt := command.CreatedAt
	assertSandboxRuntimeStrategyPersistenceRows(t, ctx, pool, command)
	assertSandboxRuntimeActiveStrategySessionWork(t, ctx, pool, store, command, session.Accounts[0], createdAt)
	var err error
	command.ID = "sandbox-strategy-runtime-session-duplicate"
	if _, err = store.CreateStrategySession(ctx, command); err == nil {
		t.Fatal("second active strategy session reused one account epoch")
	}
	postWorkAt := createdAt.Add(2 * time.Second)
	if err = store.StopCanarySession(ctx, session.ID, session.Accounts[0].ID, false, postWorkAt); err != nil {
		t.Fatalf("strategy session stop error=%v", err)
	}
	if work, workErr := store.ActiveStrategySessionWork(ctx, session.Accounts[0].ID, session.Accounts[0].Epoch,
		"sandbox_runtime-engine-runtime-worker", 1, postWorkAt.Add(time.Second), 1); workErr != nil || len(work) != 0 {
		t.Fatalf("revoked strategy work=%#v error=%v", work, workErr)
	}
	if err = store.SetEngineAccountState(ctx, session.Accounts[0].ID, sandbox.EngineReadyPaused, postWorkAt.Add(10*time.Millisecond)); err != nil {
		t.Fatalf("strategy account ready state error=%v", err)
	}
	assertSandboxRuntimeCrossStrategyPersistence(t, ctx, pool, fixture.owner, postWorkAt)
}

type sandboxRuntimeStrategyPersistenceFixture struct {
	store   *SandboxRuntimeDispatcherStore
	owner   string
	command sandbox.StrategySessionCommand
	session sandbox.StrategySession
}

func newSandboxRuntimeStrategyPersistenceFixture(t *testing.T, ctx context.Context,
	pool *pgxpool.Pool,
) sandboxRuntimeStrategyPersistenceFixture {
	t.Helper()
	store, err := NewSandboxRuntimeDispatcherStore(pool)
	if err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = pool.QueryRow(ctx, `SELECT id FROM users WHERE status='active' ORDER BY id LIMIT 1`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 7, 28, 0, 5, 6, 200_000_000, time.UTC)
	if _, err = pool.Exec(ctx, `INSERT INTO configuration_versions(
 id,version,configuration_hash,canonical_payload,actor,recorded_at)
 VALUES ('sandbox-strategy-runtime-config',1,$1,'{}','sandbox_runtime-runtime-test',$2)
 ON CONFLICT (id) DO NOTHING`, strings.Repeat("e", 64), createdAt); err != nil {
		t.Fatalf("strategy configuration error=%v", err)
	}
	command := sandbox.StrategySessionCommand{ID: "sandbox-strategy-runtime-session",
		Strategy: sandbox.StrategyTrend, Exchanges: []sandbox.Exchange{sandbox.ExchangeBinance},
		Instrument: "BTCUSDT", ConfigurationID: "sandbox-strategy-runtime-config",
		StrategySetHash: strings.Repeat("e", 64), CreatedBy: owner, CreatedAt: createdAt}
	session, err := store.CreateStrategySession(ctx, command)
	if err != nil || session.ID != command.ID || session.State != sandbox.StrategySessionPrepared ||
		len(session.Accounts) != 1 || session.Accounts[0].ID != "sandbox_runtime-engine-runtime-account" ||
		session.Accounts[0].Epoch != 1 || session.Accounts[0].Exchange != sandbox.ExchangeBinance {
		t.Fatalf("strategy session=%#v error=%v", session, err)
	}
	return sandboxRuntimeStrategyPersistenceFixture{store: store, owner: owner, command: command, session: session}
}

func assertSandboxRuntimeStrategyPersistenceRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	command sandbox.StrategySessionCommand,
) {
	t.Helper()
	var parentState, childState, instrument string
	var parentMembers, childMembers int
	if err := pool.QueryRow(ctx, `SELECT state FROM sandbox_runtime_sandbox_sessions WHERE id=$1`, command.ID).Scan(&parentState); err != nil || parentState != "READY_PAUSED" {
		t.Fatalf("strategy parent state=%q error=%v", parentState, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state,instrument FROM sandbox_strategy_sessions WHERE id=$1`, command.ID).Scan(&childState, &instrument); err != nil || childState != "prepared" || instrument != "BTCUSDT" {
		t.Fatalf("strategy child state=%q instrument=%q error=%v", childState, instrument, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_runtime_sandbox_session_accounts WHERE session_id=$1`, command.ID).Scan(&parentMembers); err != nil || parentMembers != 1 {
		t.Fatalf("strategy parent members=%d error=%v", parentMembers, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_strategy_session_accounts WHERE strategy_session_id=$1`, command.ID).Scan(&childMembers); err != nil || childMembers != 1 {
		t.Fatalf("strategy child members=%d error=%v", childMembers, err)
	}
}

func assertSandboxRuntimeCrossStrategyPersistence(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	owner string, postWorkAt time.Time,
) {
	t.Helper()
	store, _ := NewSandboxRuntimeDispatcherStore(pool)
	prepareSandboxRuntimeCrossStrategyAccount(t, ctx, store, postWorkAt)
	cross := sandbox.StrategySessionCommand{
		ID: "sandbox-cross-runtime-session", Strategy: sandbox.StrategyCrossExchangeArbitrage,
		Exchanges: []sandbox.Exchange{sandbox.ExchangeBybit, sandbox.ExchangeBinance}, Instrument: "BTCUSDT",
		ConfigurationID: "sandbox-cross-runtime-config",
		StrategySetHash: strings.Repeat("a", 64), CreatedBy: owner, CreatedAt: postWorkAt.Add(1160 * time.Millisecond),
	}
	crossSession, err := store.CreateStrategySession(ctx, cross)
	if err != nil || len(crossSession.Accounts) != 2 ||
		crossSession.Accounts[0].Exchange != sandbox.ExchangeBinance ||
		crossSession.Accounts[1].Exchange != sandbox.ExchangeBybit {
		t.Fatalf("cross strategy session=%#v error=%v", crossSession, err)
	}
	var parentMembers, childMembers int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_runtime_sandbox_session_accounts WHERE session_id=$1`, cross.ID).Scan(&parentMembers); err != nil || parentMembers != 2 {
		t.Fatalf("cross parent members=%d error=%v", parentMembers, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_strategy_session_accounts WHERE strategy_session_id=$1`, cross.ID).Scan(&childMembers); err != nil || childMembers != 2 {
		t.Fatalf("cross child members=%d error=%v", childMembers, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE sandbox_strategy_sessions
 SET state='running',started_at=$2,revision=revision+1 WHERE id=$1`, cross.ID, cross.CreatedAt); err != nil {
		t.Fatalf("cross session test start error=%v", err)
	}
	blocked, err := store.BlockExpiredStrategySessions(ctx, "sandbox_runtime-engine-runtime-account", 1, cross.CreatedAt.Add(time.Second))
	if err != nil || blocked != 2 {
		t.Fatalf("expired strategy sessions=%d error=%v", blocked, err)
	}
	var childState, blockingReason string
	if err = pool.QueryRow(ctx, `SELECT state,blocking_reason FROM sandbox_strategy_sessions WHERE id=$1`, cross.ID).Scan(&childState, &blockingReason); err != nil || childState != "blocked" || blockingReason != "arm_expired_or_revoked" {
		t.Fatalf("expired strategy state=%q reason=%q error=%v", childState, blockingReason, err)
	}
	if err = pool.QueryRow(ctx, `SELECT state,blocking_reason FROM sandbox_strategy_sessions WHERE id=$1`,
		"sandbox-strategy-runtime-session").Scan(&childState, &blockingReason); err != nil || childState != "blocked" || blockingReason != "arm_expired_or_revoked" {
		t.Fatalf("expired prior strategy state=%q reason=%q error=%v", childState, blockingReason, err)
	}
}

func prepareSandboxRuntimeCrossStrategyAccount(t *testing.T, ctx context.Context, store *SandboxRuntimeDispatcherStore,
	postWorkAt time.Time,
) {
	t.Helper()
	var err error
	bybit := SandboxRuntimeEngineAccount{
		AccountID: "sandbox_runtime-engine-runtime-bybit-account", Exchange: sandbox.ExchangeBybit,
		Environment: sandbox.EnvironmentBybitDemo, AccountIdentityHash: strings.Repeat("f", 64),
		CredentialGeneration: 1, State: sandbox.EngineLocked,
	}
	if _, err = store.EnsureAttestedAccount(ctx, bybit, postWorkAt.Add(20*time.Millisecond)); err != nil {
		t.Fatalf("bybit account error=%v", err)
	}
	if err = store.RecordValidatedEngineIdentity(ctx, sandboxRuntimeRuntimeIdentity(bybit, postWorkAt.Add(30*time.Millisecond))); err != nil {
		t.Fatalf("bybit identity error=%v", err)
	}
	bybitFence, leaseErr := store.AcquireAccountLease(ctx, bybit.AccountID, bybit.Environment,
		"sandbox_runtime-engine-runtime-bybit-worker", postWorkAt.Add(1100*time.Millisecond), time.Minute, sandbox.NoKillPoint{})
	if leaseErr != nil || bybitFence == 0 {
		t.Fatalf("bybit lease=%d error=%v", bybitFence, leaseErr)
	}
	if err = store.SetEngineAccountState(ctx, bybit.AccountID, sandbox.EngineReadyPaused, postWorkAt.Add(1120*time.Millisecond)); err != nil {
		t.Fatalf("bybit ready state error=%v", err)
	}
	binanceHealth := exchangecontracts.CollectorHealthSnapshot{
		ObservedAt: postWorkAt.Add(1140 * time.Millisecond), Exchange: "binance", Instrument: "BTCUSDT",
		BookHealth: "healthy", BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true,
	}
	bybitHealth := binanceHealth
	bybitHealth.ObservedAt, bybitHealth.Exchange = postWorkAt.Add(1140*time.Millisecond), "bybit"
	if err = store.RecordEngineObservations(ctx, "sandbox_runtime-engine-runtime-account", 1, sandbox.ExchangeBinance, 1, []exchangecontracts.CollectorHealthSnapshot{binanceHealth}); err != nil {
		t.Fatalf("binance refresh error=%v", err)
	}
	if err = store.RecordEngineObservations(ctx, bybit.AccountID, 1, sandbox.ExchangeBybit, bybitFence, []exchangecontracts.CollectorHealthSnapshot{bybitHealth}); err != nil {
		t.Fatalf("bybit observation error=%v", err)
	}
}

func assertSandboxRuntimeActiveStrategySessionWork(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore,
	command sandbox.StrategySessionCommand,
	account sandbox.StrategySessionAccount,
	createdAt time.Time,
) {
	t.Helper()
	arm, now := createSandboxRuntimeStrategyWorkArm(t, ctx, pool, store, command, account, createdAt)
	work, now := assertSandboxRuntimeStrategyWorkAdmission(t, ctx, pool, store, command, account, arm, now)
	if _, err := store.ActiveStrategySessionWork(ctx, account.ID, account.Epoch,
		"sandbox_runtime-engine-runtime-worker", 2, now, 1); err == nil {
		t.Fatal("strategy work was readable with the wrong fence")
	}
	assertSandboxRuntimeStrategyEvaluationRevocation(t, ctx, pool, store, command, account, arm, work, now)
}

func createSandboxRuntimeStrategyWorkArm(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore, command sandbox.StrategySessionCommand,
	account sandbox.StrategySessionAccount, createdAt time.Time,
) (sandbox.Arm, time.Time) {
	t.Helper()
	var actorSession string
	if err := pool.QueryRow(ctx, `
SELECT id FROM sessions
WHERE user_id=$1 AND revoked_at IS NULL AND expires_at>$2
ORDER BY id LIMIT 1`, command.CreatedBy, createdAt).Scan(&actorSession); err != nil {
		t.Fatalf("strategy work actor session error=%v", err)
	}
	armAt := createdAt.Add(10 * time.Millisecond).Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `
INSERT INTO sandbox_runtime_sandbox_authorizations(
 id,token_hash,user_id,session_id,purpose,totp_counter,session_revision,
 source_hash,reason_hash,created_at,expires_at,consumed_at
) VALUES (
 'sandbox-strategy-work-auth',$1,$2,$3,'sandbox_arm',77,
 (SELECT revision FROM sessions WHERE id=$3),$4,$5,$6,$7,$6
)`, strings.Repeat("1", 64), command.CreatedBy, actorSession,
		strings.Repeat("2", 64), strings.Repeat("3", 64), armAt,
		armAt.Add(2*time.Minute)); err != nil {
		t.Fatalf("strategy work authorization error=%v", err)
	}
	arm := sandbox.Arm{
		ID: "sandbox-strategy-work-arm", SessionID: command.ID,
		AccountIDs: []sandbox.AccountID{account.ID}, AuthorizationHash: strings.Repeat("4", 64),
		ActorUserID: command.CreatedBy, ActorSessionID: actorSession,
		ReasonHash: strings.Repeat("3", 64), CreatedAt: armAt,
		ExpiresAt: armAt.Add(sandbox.ArmLifetime), Revision: 1,
	}
	if _, err := store.CreateSandboxArm(ctx, sandbox.ArmCommand{
		Arm: arm, AuthorizationID: "sandbox-strategy-work-auth",
		SourceHash: strings.Repeat("2", 64), ExpectedSessionRevision: 1,
	}); err != nil {
		t.Fatalf("strategy work arm error=%v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_strategy_sessions
SET state='running',started_at=$2,revision=revision+1
WHERE id=$1`, command.ID, armAt); err != nil {
		t.Fatalf("strategy work start error=%v", err)
	}
	now := armAt.Add(time.Second)
	return arm, now
}

func assertSandboxRuntimeStrategyWorkAdmission(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore, command sandbox.StrategySessionCommand,
	account sandbox.StrategySessionAccount, arm sandbox.Arm, now time.Time,
) (sandbox.StrategySessionWork, time.Time) {
	t.Helper()
	if err := store.RecordEngineObservations(ctx, account.ID, account.Epoch,
		account.Exchange, 1, []exchangecontracts.CollectorHealthSnapshot{{
			ObservedAt: now, Exchange: string(account.Exchange), Instrument: "BTCUSDT",
			BookHealth: "healthy", BookHealthy: true, BookFresh: true,
			BookEligible: true, ClockEligible: true, Eligible: true,
		}}); err != nil {
		t.Fatalf("strategy work observation error=%v", err)
	}
	work, err := store.ActiveStrategySessionWork(ctx, account.ID, account.Epoch,
		"sandbox_runtime-engine-runtime-worker", 1, now, 1)
	if err != nil || len(work) != 1 || work[0].SessionID != sandbox.SessionID(command.ID) ||
		work[0].Strategy != sandbox.StrategyTrend || work[0].Instrument != "BTCUSDT" ||
		work[0].ConfigurationHash != strings.Repeat("e", 64) ||
		work[0].Account != account || work[0].ArmID != arm.ID ||
		work[0].ArmRevision != arm.Revision || work[0].ArmExpiresAt != arm.ExpiresAt {
		t.Fatalf("strategy work=%#v error=%v", work, err)
	}
	configuration, err := store.StrategySessionConfiguration(ctx, work[0], now)
	if err != nil || configuration.ID != command.ConfigurationID ||
		configuration.Hash != strings.Repeat("e", 64) || string(configuration.Payload) != "{}" {
		t.Fatalf("strategy configuration=%#v error=%v", configuration, err)
	}
	admission, err := store.StrategySessionAdmission(ctx, work[0], now,
		[4]bool{true, true, true, true})
	if err != nil || admission.Valid() != nil ||
		admission.Work.ArmID != arm.ID || admission.StartupCycle == 0 {
		t.Fatalf("strategy admission=%#v error=%v", admission, err)
	}
	now = assertSandboxStrategyFillAccounting(t, ctx, pool, store, admission)
	assertOwnerSandboxRunProjection(t, ctx, pool, string(command.ID))
	return work[0], now
}

func assertOwnerSandboxRunProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	id string,
) {
	t.Helper()
	consoleStore, err := NewOwnerConsoleStore(pool, []byte(strings.Repeat("r", 32)), &domain.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	run, err := consoleStore.Run(ctx, id)
	if err != nil || run.Mode != generated.RunResourceModeSandbox || run.Environment != generated.BinanceSpotTestnet {
		t.Fatalf("owner sandbox run=%#v error=%v", run, err)
	}
	for _, view := range []string{"event", "decision", "order", "projection"} {
		page, outputErr := consoleStore.RunOutputs(ctx, id, view)
		if outputErr != nil || len(page.Items) == 0 {
			t.Fatalf("owner sandbox %s outputs=%d error=%v", view, len(page.Items), outputErr)
		}
		for _, item := range page.Items {
			if !json.Valid([]byte(item.CanonicalPayload)) || len(item.ContentHash) != 64 {
				t.Fatalf("owner sandbox %s output invalid: %#v", view, item)
			}
		}
	}
	portfolio, err := consoleStore.RunPortfolio(ctx, id)
	if err != nil || portfolio.State != generated.RunPortfolioProjectionStateRecorded ||
		portfolio.TotalPnl == nil || portfolio.Positions == nil || len(*portfolio.Positions) == 0 ||
		portfolio.CanonicalPayload == nil || portfolio.ContentHash == nil {
		t.Fatalf("owner sandbox portfolio=%#v error=%v", portfolio, err)
	}
	riskProjection, err := consoleStore.RunRisk(ctx, id)
	if err != nil || riskProjection.State != generated.RunRiskProjectionStateRecorded ||
		riskProjection.Observations == nil || len(*riskProjection.Observations) == 0 {
		t.Fatalf("owner sandbox risk=%#v error=%v", riskProjection, err)
	}
}

func assertSandboxRuntimeStrategyEvaluationRevocation(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	store *SandboxRuntimeDispatcherStore, command sandbox.StrategySessionCommand,
	account sandbox.StrategySessionAccount, arm sandbox.Arm, work sandbox.StrategySessionWork, now time.Time,
) {
	t.Helper()
	evaluation := sandbox.StrategySessionEvaluation{Work: work,
		State:  sandbox.StrategySessionEvaluationWaiting,
		Reason: "waiting_for_finalized_candle", EvidenceHash: strings.Repeat("6", 64),
		OccurredAt: now}
	if err := store.RecordStrategySessionEvaluation(ctx, "sandbox_runtime-engine-runtime-worker", 1, evaluation); err != nil {
		t.Fatalf("strategy evaluation record error=%v", err)
	}
	var evaluationCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM sandbox_strategy_session_evaluations
WHERE strategy_session_id=$1 AND account_id=$2 AND evidence_hash=$3`,
		command.ID, account.ID, evaluation.EvidenceHash).Scan(&evaluationCount); err != nil || evaluationCount != 1 {
		t.Fatalf("strategy evaluation count=%d error=%v", evaluationCount, err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE sandbox_runtime_sandbox_arms SET revoked_at=$2,revision=revision+1 WHERE id=$1`, arm.ID, now.Add(time.Millisecond)); err != nil {
		t.Fatalf("strategy arm revocation error=%v", err)
	}
	evaluation.OccurredAt = now.Add(2 * time.Millisecond)
	evaluation.EvidenceHash = strings.Repeat("7", 64)
	if err := store.RecordStrategySessionEvaluation(ctx, "sandbox_runtime-engine-runtime-worker", 1, evaluation); err == nil {
		t.Fatal("strategy evaluation persisted after arm revocation")
	}
}
