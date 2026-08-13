package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestStrategySessionRequiresEveryArmedAccountAndRejectsAdvisoryStrategy(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	arm := Arm{ID: "arm", SessionID: "session", AccountIDs: []AccountID{"binance"}, AuthorizationHash: strings.Repeat("a", 64), ActorUserID: "owner", ActorSessionID: "browser", ReasonHash: strings.Repeat("b", 64), CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(14 * time.Minute), Revision: 1}
	trend := StrategySession{ID: "session", Strategy: StrategyTrend, Accounts: []StrategySessionAccount{{ID: "binance", Epoch: 1, Exchange: ExchangeBinance}}, State: StrategySessionPrepared, CreatedAt: now}
	if started, err := trend.Start(arm, now); err != nil || started.State != StrategySessionRunning {
		t.Fatalf("trend start=%+v err=%v", started, err)
	}
	advisory := trend
	advisory.Strategy = "inventory-rebalancing"
	if _, err := advisory.Start(arm, now); err == nil {
		t.Fatal("advisory rebalancing strategy was accepted")
	}
	cross := StrategySession{ID: "session", Strategy: StrategyCrossExchangeArbitrage, Accounts: []StrategySessionAccount{{ID: "binance", Epoch: 1, Exchange: ExchangeBinance}, {ID: "bybit", Epoch: 1, Exchange: ExchangeBybit}}, State: StrategySessionPrepared, CreatedAt: now}
	if _, err := cross.Start(arm, now); err == nil {
		t.Fatal("cross-exchange session started without every account armed")
	}
}

func TestStrategySessionStopsAndCannotStartAfterArmExpiry(t *testing.T) {
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	arm := Arm{ID: "arm", SessionID: "session", AccountIDs: []AccountID{"binance"}, AuthorizationHash: strings.Repeat("a", 64), ActorUserID: "owner", ActorSessionID: "browser", ReasonHash: strings.Repeat("b", 64), CreatedAt: now.Add(-16 * time.Minute), ExpiresAt: now.Add(-time.Minute), Revision: 1}
	session := StrategySession{ID: "session", Strategy: StrategyTrend, Accounts: []StrategySessionAccount{{ID: "binance", Epoch: 1, Exchange: ExchangeBinance}}, State: StrategySessionPrepared, CreatedAt: now}
	if _, err := session.Start(arm, now); err == nil {
		t.Fatal("expired arm started a strategy session")
	}
	session.State = StrategySessionRunning
	stopped, err := session.Stop()
	if err != nil || stopped.State != StrategySessionStopped {
		t.Fatalf("stop=%+v err=%v", stopped, err)
	}
}

func TestStrategySessionCommandRejectsAdvisoryAndInvalidVenueTopology(t *testing.T) {
	command := StrategySessionCommand{ID: "session", Strategy: StrategyCrossExchangeArbitrage,
		Exchanges: []Exchange{ExchangeBinance, ExchangeBybit}, Instrument: "BTCUSDT",
		ConfigurationID: "configuration", StrategySetHash: strings.Repeat("c", 64),
		CreatedBy: "owner", CreatedAt: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	command.Strategy, command.Exchanges = "inventory-rebalancing", []Exchange{ExchangeBinance}
	if err := command.Validate(); err == nil {
		t.Fatal("advisory strategy command was accepted")
	}
	command.Strategy, command.Exchanges = StrategyCrossExchangeArbitrage, []Exchange{ExchangeBinance}
	if err := command.Validate(); err == nil {
		t.Fatal("cross-exchange command accepted a single venue")
	}
	command.Exchanges, command.StrategySetHash = []Exchange{ExchangeBinance, ExchangeBybit}, strings.Repeat("z", 64)
	if err := command.Validate(); err == nil {
		t.Fatal("non-hex strategy hash was accepted")
	}
}
