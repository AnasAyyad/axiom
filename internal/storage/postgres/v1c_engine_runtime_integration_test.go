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
	assertV1CEngineCommandFencing(t, fixture)
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

func assertV1CStrategySessionPersistence(
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
	createdAt := time.Date(2026, 7, 28, 0, 5, 6, 200_000_000, time.UTC)
	command := sandbox.StrategySessionCommand{
		ID: "sandbox-strategy-runtime-session", Strategy: sandbox.StrategyTrend,
		Exchanges: []sandbox.Exchange{sandbox.ExchangeBinance}, Instrument: "BTCUSDT",
		ConfigurationID: "sandbox-strategy-runtime-config",
		StrategySetHash: strings.Repeat("e", 64), CreatedBy: owner, CreatedAt: createdAt,
	}
	session, err := store.CreateStrategySession(ctx, command)
	if err != nil || session.ID != command.ID || session.State != sandbox.StrategySessionPrepared ||
		len(session.Accounts) != 1 || session.Accounts[0].ID != "v1c-engine-runtime-account" ||
		session.Accounts[0].Epoch != 1 || session.Accounts[0].Exchange != sandbox.ExchangeBinance {
		t.Fatalf("strategy session=%#v error=%v", session, err)
	}
	var parentState, childState string
	var parentMembers, childMembers int
	if err = pool.QueryRow(ctx, `
SELECT state FROM v1c_sandbox_sessions WHERE id=$1`, command.ID).Scan(&parentState); err != nil || parentState != "READY_PAUSED" {
		t.Fatalf("strategy parent state=%q error=%v", parentState, err)
	}
	if err = pool.QueryRow(ctx, `
SELECT state FROM sandbox_strategy_sessions WHERE id=$1`, command.ID).Scan(&childState); err != nil || childState != "prepared" {
		t.Fatalf("strategy child state=%q error=%v", childState, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v1c_sandbox_session_accounts WHERE session_id=$1`, command.ID).Scan(&parentMembers); err != nil || parentMembers != 1 {
		t.Fatalf("strategy parent members=%d error=%v", parentMembers, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_strategy_session_accounts WHERE strategy_session_id=$1`, command.ID).Scan(&childMembers); err != nil || childMembers != 1 {
		t.Fatalf("strategy child members=%d error=%v", childMembers, err)
	}
	command.ID = "sandbox-strategy-runtime-session-duplicate"
	if _, err = store.CreateStrategySession(ctx, command); err == nil {
		t.Fatal("second active strategy session reused one account epoch")
	}
	if err = store.StopCanarySession(ctx, session.ID, session.Accounts[0].ID, false, createdAt.Add(50*time.Millisecond)); err != nil {
		t.Fatalf("strategy session stop error=%v", err)
	}
	bybit := V1CEngineAccount{
		AccountID: "v1c-engine-runtime-bybit-account", Exchange: sandbox.ExchangeBybit,
		Environment: sandbox.EnvironmentBybitDemo, AccountIdentityHash: strings.Repeat("f", 64),
		CredentialGeneration: 1, State: sandbox.EngineLocked,
	}
	if _, err = store.EnsureAttestedAccount(ctx, bybit, createdAt.Add(60*time.Millisecond)); err != nil {
		t.Fatalf("bybit account error=%v", err)
	}
	if err = store.RecordValidatedEngineIdentity(ctx, v1cRuntimeIdentity(bybit, createdAt.Add(70*time.Millisecond))); err != nil {
		t.Fatalf("bybit identity error=%v", err)
	}
	bybitFence, leaseErr := store.AcquireAccountLease(ctx, bybit.AccountID, bybit.Environment,
		"v1c-engine-runtime-bybit-worker", createdAt.Add(80*time.Millisecond), time.Minute, sandbox.NoKillPoint{})
	if leaseErr != nil || bybitFence == 0 {
		t.Fatalf("bybit lease=%d error=%v", bybitFence, leaseErr)
	}
	if err = store.SetEngineAccountState(ctx, bybit.AccountID, sandbox.EngineReadyPaused, createdAt.Add(90*time.Millisecond)); err != nil {
		t.Fatalf("bybit ready state error=%v", err)
	}
	binanceHealth := exchangecontracts.CollectorHealthSnapshot{
		ObservedAt: createdAt.Add(100 * time.Millisecond), Exchange: "binance", Instrument: "BTCUSDT",
		BookHealth: "healthy", BookHealthy: true, BookFresh: true, BookEligible: true, ClockEligible: true, Eligible: true,
	}
	bybitHealth := binanceHealth
	bybitHealth.ObservedAt, bybitHealth.Exchange = createdAt.Add(110*time.Millisecond), "bybit"
	if err = store.RecordEngineObservation(ctx, "v1c-engine-runtime-account", 1, sandbox.ExchangeBinance, 1, binanceHealth); err != nil {
		t.Fatalf("binance refresh error=%v", err)
	}
	if err = store.RecordEngineObservation(ctx, bybit.AccountID, 1, sandbox.ExchangeBybit, bybitFence, bybitHealth); err != nil {
		t.Fatalf("bybit observation error=%v", err)
	}
	cross := sandbox.StrategySessionCommand{
		ID: "sandbox-cross-runtime-session", Strategy: sandbox.StrategyCrossExchangeArbitrage,
		Exchanges: []sandbox.Exchange{sandbox.ExchangeBybit, sandbox.ExchangeBinance}, Instrument: "BTCUSDT",
		ConfigurationID: "sandbox-cross-runtime-config",
		StrategySetHash: strings.Repeat("a", 64), CreatedBy: owner, CreatedAt: createdAt.Add(120 * time.Millisecond),
	}
	crossSession, err := store.CreateStrategySession(ctx, cross)
	if err != nil || len(crossSession.Accounts) != 2 ||
		crossSession.Accounts[0].Exchange != sandbox.ExchangeBinance ||
		crossSession.Accounts[1].Exchange != sandbox.ExchangeBybit {
		t.Fatalf("cross strategy session=%#v error=%v", crossSession, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM v1c_sandbox_session_accounts WHERE session_id=$1`, cross.ID).Scan(&parentMembers); err != nil || parentMembers != 2 {
		t.Fatalf("cross parent members=%d error=%v", parentMembers, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_strategy_session_accounts WHERE strategy_session_id=$1`, cross.ID).Scan(&childMembers); err != nil || childMembers != 2 {
		t.Fatalf("cross child members=%d error=%v", childMembers, err)
	}
}
