package sandbox

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
)

func TestStrategyRiskFactsRequireExactFreshAccountSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	if err := facts.ValidFor(work, snapshot, now); err != nil {
		t.Fatalf("valid facts rejected: %v", err)
	}
	wrongSnapshot := snapshot
	wrongSnapshot.SnapshotHash = strings.Repeat("8", 64)
	if err := facts.ValidFor(work, wrongSnapshot, now); err == nil {
		t.Fatal("risk facts bound to another snapshot accepted")
	}
	facts.ObservedAt = now.Add(-251 * time.Millisecond)
	if err := facts.ValidFor(work, snapshot, now); err == nil {
		t.Fatal("stale risk facts accepted")
	}
	facts = validStrategyRiskFacts(t, work, snapshot, now)
	zero, err := domain.ParseMoney("0")
	if err != nil {
		t.Fatal(err)
	}
	facts.MaximumReserved = zero
	if err := facts.ValidFor(work, snapshot, now); err != nil {
		t.Fatalf("zero maximum reservation is a valid fail-closed policy result: %v", err)
	}
}

func validStrategyRiskWork(t *testing.T, now time.Time) StrategySessionWork {
	t.Helper()
	work := StrategySessionWork{SessionID: "session-risk-facts", Account: StrategySessionAccount{
		ID: "account-risk-facts", Epoch: 1, Exchange: ExchangeBinance},
		Strategy: StrategyTrend, Instrument: "BTCUSDT", SessionRevision: 1,
		StrategyRevision: 1, ConfigurationID: "configuration-risk-facts",
		ConfigurationHash: strings.Repeat("1", 64), StrategySetHash: strings.Repeat("2", 64),
		ArmID: "arm-risk-facts", ArmRevision: 1, StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)}
	if err := work.ValidAt(now); err != nil {
		t.Fatal(err)
	}
	return work
}

func validStrategyRiskSnapshot(t *testing.T, work StrategySessionWork, now time.Time) AccountSnapshot {
	t.Helper()
	available, err := domain.ParseBalance("100")
	if err != nil {
		t.Fatal(err)
	}
	return AccountSnapshot{AccountID: work.Account.ID, Epoch: work.Account.Epoch,
		Balances: []Balance{{Asset: "USDT", Available: available}}, OrdersHash: strings.Repeat("3", 64),
		FillsHash: strings.Repeat("4", 64), SnapshotHash: strings.Repeat("5", 64), ObservedAt: now}
}

func validStrategyRiskFacts(
	t *testing.T,
	work StrategySessionWork,
	snapshot AccountSnapshot,
	now time.Time,
) StrategyRiskFacts {
	t.Helper()
	reserve, err := domain.ParseMoney("15")
	if err != nil {
		t.Fatal(err)
	}
	return StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: snapshot.SnapshotHash, PolicyID: "global-risk-policy", PolicyVersion: 1,
		PolicyHash: strings.Repeat("6", 64), Policy: validStrategyRiskPolicy("global-risk-policy", 1),
		MinimumReserve: reserve, MaximumReserved: reserve, ObservedAt: now}
}

func validStrategyRiskPolicy(id string, version uint64) risk.Policy {
	policy := risk.DefaultGlobalPolicy()
	policy.ID, policy.Version, policy.State = id, version, risk.StateNormal
	return policy
}
