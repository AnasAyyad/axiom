package sandbox

import (
	"strings"
	"testing"
	"time"
)

func TestStrategySessionWorkAcceptsOnlyLiveExactAccountSnapshots(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := StrategySessionWork{
		SessionID: "session", Strategy: StrategyTrend, Instrument: "BTCUSDT",
		Account:         StrategySessionAccount{ID: "binance-account", Epoch: 7, Exchange: ExchangeBinance},
		ConfigurationID: "configuration", StrategySetHash: strings.Repeat("a", 64),
		SessionRevision: 3, StrategyRevision: 4,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute),
	}
	if err := work.ValidAt(now); err != nil {
		t.Fatal(err)
	}
	work.ArmExpiresAt = now
	if err := work.ValidAt(now); err == nil {
		t.Fatal("expired arm work snapshot accepted")
	}
	work.ArmExpiresAt = now.Add(time.Minute)
	work.Account.Epoch = 0
	if err := work.ValidAt(now); err == nil {
		t.Fatal("epoch-less work snapshot accepted")
	}
}

func TestStrategySessionWorkRejectsAdvisoryAndOrderShapedValues(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := StrategySessionWork{
		SessionID: "session", Strategy: "inventory-rebalancing", Instrument: "ETHUSDT",
		Account:         StrategySessionAccount{ID: "bybit-account", Epoch: 2, Exchange: ExchangeBybit},
		ConfigurationID: "configuration", StrategySetHash: strings.Repeat("b", 64),
		SessionRevision: 1, StrategyRevision: 1,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute),
	}
	if err := work.ValidAt(now); err == nil {
		t.Fatal("advisory strategy work snapshot accepted")
	}
}
