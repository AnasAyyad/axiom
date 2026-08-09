package sandbox

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestStrategyRiskObservationBindsEveryCentralRiskInput(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	market := StrategyMarketInput{Instrument: instrument,
		Metadata:   exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat("6", 64)},
		Book:       exchangecontracts.BookSnapshot{RawPayloadHash: strings.Repeat("7", 64)},
		Candles:    map[string][]exchangecontracts.Candle{"4h": {{RawPayloadHash: strings.Repeat("8", 64)}}},
		ObservedAt: domain.EventTime{UTC: now, Sequence: 1}}
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	observation := validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	if err := observation.ValidFor(work, snapshot, market, facts, now); err != nil {
		t.Fatalf("valid observation rejected: %v", err)
	}
	inputs := observation.RiskObservations()
	if inputs.AccountDrawdown == nil || inputs.UTCDayLoss == nil || inputs.Rolling24HourLoss == nil ||
		inputs.StrategyLoss == nil || inputs.AssetExposure == nil || inputs.CombinedExposure == nil ||
		inputs.ExchangeExposure == nil || inputs.Reserve == nil || inputs.ReservedCapital == nil ||
		inputs.Spread == nil || inputs.Slippage == nil || inputs.OpenOrders == nil || inputs.BookAge == nil ||
		inputs.QueueLag == nil || inputs.ClockDrift == nil || inputs.QualityScore == nil ||
		inputs.Health.Gap == nil || inputs.Health.LeaseLost == nil {
		t.Fatalf("incomplete central-risk inputs: %#v", inputs)
	}
	observation.SnapshotHash = strings.Repeat("9", 64)
	if err := observation.ValidFor(work, snapshot, market, facts, now); err == nil {
		t.Fatal("observation bound to another account snapshot accepted")
	}
	observation = validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	otherSession := observation
	otherSession.StrategySessionID = "another-session"
	if otherSession.EvidenceHash() == observation.EvidenceHash() ||
		otherSession.ValidFor(work, snapshot, market, facts, now) == nil {
		t.Fatal("risk evidence identity does not bind the exact strategy session")
	}
	staleMarket := market
	staleMarket.ObservedAt.UTC = now.Add(-time.Second)
	staleObservation := validStrategyRiskObservation(t, work, snapshot, staleMarket, facts, now)
	if staleObservation.ValidFor(work, snapshot, staleMarket, facts, now) == nil {
		t.Fatal("stale public market evidence accepted")
	}
}

func TestStrategyRiskObservationAllowsBookOnlyArbitrageButNotCandleStrategies(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 5, 0, 0, time.UTC)
	work := validStrategyRiskWork(t, now)
	snapshot := validStrategyRiskSnapshot(t, work, now)
	facts := validStrategyRiskFacts(t, work, snapshot, now)
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	market := StrategyMarketInput{Instrument: instrument,
		Metadata:   exchangecontracts.InstrumentRecord{RawPayloadHash: strings.Repeat("6", 64)},
		Book:       exchangecontracts.BookSnapshot{RawPayloadHash: strings.Repeat("7", 64)},
		ObservedAt: domain.EventTime{UTC: now, Sequence: 1}}
	observation := validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	if observation.ValidFor(work, snapshot, market, facts, now) == nil {
		t.Fatal("Trend risk observation accepted missing finalized candles")
	}
	work.Strategy = StrategyTriangular
	observation = validStrategyRiskObservation(t, work, snapshot, market, facts, now)
	if err := observation.ValidFor(work, snapshot, market, facts, now); err != nil {
		t.Fatalf("order-book-only triangular risk observation rejected: %v", err)
	}
	if StrategyMarketEvidenceHash(market) == "" {
		t.Fatal("order-book-only evidence hash was not produced")
	}
}

func validStrategyRiskObservation(
	t *testing.T,
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	now time.Time,
) StrategyRiskObservation {
	t.Helper()
	zero, err := domain.ParsePercent("0")
	if err != nil {
		t.Fatal(err)
	}
	one, err := domain.ParsePercent("1")
	if err != nil {
		t.Fatal(err)
	}
	return StrategyRiskObservation{StrategySessionID: work.SessionID, StrategyRevision: work.StrategyRevision,
		AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
		SnapshotHash: snapshot.SnapshotHash, MarketHash: StrategyMarketEvidenceHash(market), Instrument: work.Instrument,
		PolicyID: facts.PolicyID, PolicyVersion: facts.PolicyVersion, PolicyHash: facts.PolicyHash, Reserve: one,
		AccountDrawdown: zero, UTCDayLoss: zero, Rolling24HourLoss: zero, StrategyLoss: zero,
		AssetExposure: zero, CombinedExposure: zero, ExchangeExposure: zero, ReservedCapital: zero,
		Spread: zero, Slippage: zero, QualityScore: 100, ObservedAt: now}
}
