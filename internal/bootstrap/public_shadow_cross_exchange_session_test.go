package bootstrap

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/replay"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/crossarb"
)

func TestOwnerConsoleCrossExchangeShadowBuildsCoherentOwnedRecordedInput(t *testing.T) {
	now := time.Date(2026, 8, 9, 22, 30, 0, 0, time.UTC)
	claim, session, instrument := newOwnerConsoleCrossExchangeTestSession(t, now)
	market, err := session.captureMarket(context.Background(), now)
	if err != nil || market.Coherent.Identity == "" || len(market.Markets) != 2 {
		t.Fatalf("market=%#v error=%v", market, err)
	}
	initializeOwnerConsoleCrossExchangeTestBalances(t, session, market)
	input, err := session.buildInput(market)
	if err != nil || len(input.Inventory) != 2 || input.Simulation == nil || input.CentralRisk == nil ||
		input.Inventory[0].Owner != "cross-exchange:"+claim.ConfigurationHash {
		t.Fatalf("input=%#v error=%v", input, err)
	}
	assertOwnerConsoleCrossExchangeRecordedInput(t, claim, session, input)
	bybit := runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument}
	session.collectors[bybit].(*publicShadowSagaCollector).health.Eligible = false
	if _, err = session.captureMarket(context.Background(), now); !errorsIsCrossInputUnavailable(err) {
		t.Fatalf("ineligible pair error=%v", err)
	}
}

func newOwnerConsoleCrossExchangeTestSession(t *testing.T, now time.Time) (
	postgresstore.PublicShadowClaim, *ownerConsoleCrossExchangeShadowSession, domain.Instrument,
) {
	t.Helper()
	instrument := sagaInstrument(t, "BTCUSDT")
	claim := postgresstore.PublicShadowClaim{ID: "cross-shadow-test", StrategyID: "cross-exchange-arbitrage-1-0-0",
		StrategyVersion: "cross-exchange-arbitrage@1.0.0", ExchangeID: "binance", InstrumentID: "BTCUSDT",
		ConfigurationHash: "cross-shadow-configuration", Configuration: config.DefaultMultiStrategyConfiguration(),
		VenueAccountIDs: map[string]string{"binance": "cross-binance", "bybit": "cross-bybit"},
		MarketScopes: []postgresstore.PublicShadowMarketScope{
			{Ordinal: 1, ExchangeID: "binance", InstrumentID: "BTCUSDT", Purpose: "paired_market"},
			{Ordinal: 2, ExchangeID: "bybit", InstrumentID: "BTCUSDT", Purpose: "paired_market"},
		}}
	configuration, err := crossarb.ConfigurationFromReviewed(claim.Configuration.CrossExchange)
	if err != nil {
		t.Fatal(err)
	}
	session := &ownerConsoleCrossExchangeShadowSession{claim: claim, configuration: configuration,
		clients: map[string]shadowPublicClient{"binance": ownerConsoleTriangularPublicClient{offset: 1_000_000},
			"bybit": ownerConsoleTriangularPublicClient{offset: 1_000_000}},
		collectors: make(map[runtimecore.MarketKey]shadowPublicCollector, 2),
		metadata:   make(map[runtimecore.MarketKey]domain.InstrumentMetadata, 2),
		maximum:    make(map[runtimecore.MarketKey]domain.Quantity, 2)}
	populateOwnerConsoleCrossExchangeTestMarkets(t, session, instrument, now)
	return claim, session, instrument
}

func populateOwnerConsoleCrossExchangeTestMarkets(t *testing.T, session *ownerConsoleCrossExchangeShadowSession,
	instrument domain.Instrument, now time.Time,
) {
	t.Helper()
	values := []struct{ exchange, bid, ask string }{
		{"binance", "99.5", "100"}, {"bybit", "110", "110.5"},
	}
	for index, value := range values {
		key := runtimecore.MarketKey{Exchange: value.exchange, Instrument: instrument}
		view := sagaBookViewPrices(t, value.exchange, instrument, now,
			uint64(900_000+index*10_000), uint64(index+1), value.bid, value.ask)
		session.collectors[key] = &publicShadowSagaCollector{provider: publicShadowSagaViews{view: view},
			health: exchangecontracts.CollectorHealthSnapshot{Exchange: value.exchange,
				Instrument: instrument.Symbol(), BookHealth: "HEALTHY", BookHealthy: true,
				BookFresh: true, BookEligible: true, ClockEligible: true,
				ClockObservedAt: now.Add(-time.Second), ClockUncertainty: time.Millisecond, Eligible: true}}
		tick, _ := domain.ParsePrice("0.01")
		step, _ := domain.ParseQuantity("0.000001")
		minimum, _ := domain.ParseNotional("0.01")
		session.metadata[key] = domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: now.Add(-time.Minute), PriceTick: tick, QuantityStep: step,
			MinimumQuantity: step, MinimumNotional: minimum}
		session.maximum[key], _ = domain.ParseQuantity("1000")
	}
}

func initializeOwnerConsoleCrossExchangeTestBalances(t *testing.T, session *ownerConsoleCrossExchangeShadowSession,
	market SandboxCrossExchangeMarketInput,
) {
	t.Helper()
	capital, _ := domain.ParseBalance("250")
	initialized, _, err := crossarb.InitializeSingleInstrumentInventory(market.Markets, capital)
	if err != nil {
		t.Fatal(err)
	}
	session.balances = make(map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, 2)
	for exchange, balances := range initialized {
		session.balances[exchange] = make(map[domain.AssetSymbol]accounting.BalanceSnapshot, 3)
		for asset, value := range balances {
			session.balances[exchange][asset] = accounting.BalanceSnapshot{Available: value, Revision: 2}
		}
	}
}

func assertOwnerConsoleCrossExchangeRecordedInput(t *testing.T, claim postgresstore.PublicShadowClaim,
	session *ownerConsoleCrossExchangeShadowSession, input crossarb.Input,
) {
	t.Helper()
	prepared, projection, err := crossarb.AttachCleanRecordedReduction(input,
		"shadow/cross-exchange/"+claim.ID, ownerConsoleCrossExchangeAvailable(session.balances))
	if err != nil || projection == nil || prepared.Reduction == nil ||
		projection.Simulation.Outcome != crossarb.OutcomeBothFilled {
		t.Fatalf("projection=%#v error=%v", projection, err)
	}
	runID, _ := domain.NewRunID("cross-shadow-test-run")
	portfolioID, _ := domain.NewPortfolioID("cross-shadow-test-portfolio")
	processor, err := newOwnerConsoleCrossExchangeOperationalProcessorWithOwnership(backtest.JobClaim{
		ID: claim.ID, Configuration: claim.Configuration,
		Manifest: backtest.RunManifest{RunID: runID, Mode: "shadow",
			ConfigurationHash: claim.ConfigurationHash, StrategyVersion: claim.StrategyVersion},
	}, portfolioID, "cross_exchange")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), replay.Event{Ordinal: prepared.Ordinal,
		LogicalTime: prepared.LogicalTime, Canonical: payload})
	if err != nil || result.Ordinal != prepared.Ordinal || !json.Valid(result.Decision) ||
		!json.Valid(result.Orders) || !json.Valid(result.ExecutionEvents) || !json.Valid(result.Balances) {
		t.Fatalf("operational result=%#v error=%v", result, err)
	}
}

func errorsIsCrossInputUnavailable(err error) bool {
	return err == errPublicShadowCrossExchangeMarketInputUnavailable
}
