package bootstrap

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/trend"
)

func TestBuildTrendInputRequiresFreshBoundFactsAndAValidBook(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 3, time.UTC)
	configuration, err := trend.NewConfiguration(config.DefaultConfiguration().Trend)
	if err != nil {
		t.Fatal(err)
	}
	work, market, facts := validSandboxTrendInputBuilderFacts(t, now)
	input, err := BuildTrendInput(work, configuration, market, trend.PositionState{}, facts, now)
	if err != nil || input.Instrument != market.Instrument || input.Sizing.NotionalLimits[0].String() != "10" ||
		input.Evidence.ConfigurationHash != configuration.Hash || input.Evidence.CausationID == "" {
		t.Fatalf("input=%#v error=%v", input, err)
	}
	stale := facts
	stale.AccountSnapshot.ObservedAt = now.Add(-251 * time.Millisecond)
	if _, err = BuildTrendInput(work, configuration, market, trend.PositionState{}, stale, now); err == nil {
		t.Fatal("stale account facts accepted")
	}
	mismatched := facts
	mismatched.ConfigurationHash = strings.Repeat("e", 64)
	if _, err = BuildTrendInput(work, configuration, market, trend.PositionState{}, mismatched, now); err == nil {
		t.Fatal("facts from another configuration accepted")
	}
	crossed := market
	crossed.Book.Asks = append([]exchangecontracts.PriceLevel(nil), market.Book.Asks...)
	crossed.Book.Asks[0].Price = market.Book.Bids[0].Price
	belowBid, parseErr := domain.ParsePrice("298")
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	crossed.Book.Asks[0].Price = belowBid
	if _, err = BuildTrendInput(work, configuration, crossed, trend.PositionState{}, facts, now); err == nil {
		t.Fatal("crossed book accepted")
	}
}

func validSandboxTrendInputBuilderFacts(t *testing.T, now time.Time) (
	sandbox.StrategySessionWork,
	sandbox.StrategyMarketInput,
	SandboxStrategySizingFacts,
) {
	t.Helper()
	values := sandboxTrendInputFixtureValues(t)
	metadata := domain.InstrumentMetadata{Instrument: values.instrument, Version: 1,
		EffectiveAt: now.Add(-800 * time.Hour),
		PriceTick:   ownerConsoleE2EPrice(t, "0.01"), QuantityStep: ownerConsoleE2EQuantity(t, "0.0001"),
		MinimumQuantity: ownerConsoleE2EQuantity(t, "0.0001"), MinimumNotional: values.minimum}
	candles := ownerConsoleE2ETrendInput(t, config.DefaultConfiguration(), now).Candles
	for index := range candles {
		candles[index].RawPayloadHash = strings.Repeat("c", 64)
	}
	work := sandbox.StrategySessionWork{SessionID: "input-builder-session", Strategy: sandbox.StrategyTrend,
		Instrument: "BTCUSDT", Account: sandbox.StrategySessionAccount{ID: "account", Epoch: 1, Exchange: sandbox.ExchangeBinance},
		ConfigurationID: "configuration", ConfigurationHash: strings.Repeat("d", 64), StrategySetHash: strings.Repeat("e", 64),
		SessionRevision: 1, StrategyRevision: 1, ArmID: "arm", ArmRevision: 1,
		StartedAt: now.Add(-time.Minute), ArmExpiresAt: now.Add(time.Minute)}
	market := sandbox.StrategyMarketInput{Instrument: values.instrument, ObservedAt: domain.EventTime{UTC: now, Sequence: 1},
		Metadata: exchangecontracts.InstrumentRecord{Metadata: metadata, RawPayloadHash: strings.Repeat("a", 64)},
		Book: exchangecontracts.BookSnapshot{Instrument: values.instrument, LastSequence: 1,
			ReceivedAt:     domain.EventTime{UTC: now, Sequence: 1},
			Bids:           []exchangecontracts.PriceLevel{{Price: values.bid, Quantity: values.quantity}},
			Asks:           []exchangecontracts.PriceLevel{{Price: values.ask, Quantity: values.quantity}},
			RawPayloadHash: strings.Repeat("b", 64)},
		Candles: map[string][]exchangecontracts.Candle{"4h": candles}}
	facts := SandboxStrategySizingFacts{AccountSnapshot: sandbox.AccountSnapshot{AccountID: work.Account.ID, Epoch: work.Account.Epoch,
		Balances: []sandbox.Balance{{Asset: "USDT", Available: values.available}}, OrdersHash: strings.Repeat("1", 64),
		FillsHash: strings.Repeat("2", 64), SnapshotHash: strings.Repeat("3", 64), ObservedAt: now},
		CentralRiskFacts: sandbox.StrategyRiskFacts{AccountID: work.Account.ID, AccountEpoch: work.Account.Epoch,
			SnapshotHash: strings.Repeat("3", 64), PolicyID: "risk-policy", PolicyVersion: 1,
			PolicyHash: strings.Repeat("4", 64), Policy: sizingFactsRiskPolicy("risk-policy", 1),
			MinimumReserve: values.money, MaximumReserved: values.money, ObservedAt: now},
		PortfolioRevision: 1, PositionRevision: 1, AssetEligibility: 1, RiskPolicyID: "risk-policy", RiskPolicyVersion: 1,
		RiskPolicyHash: strings.Repeat("4", 64), MinimumReserve: values.money,
		MaximumOrderNotional: values.maximum, EntryFeeRate: values.rate, ExitFeeRate: values.rate,
		GapAllowance: values.gap, LatencyDeterioration: values.latency,
		SlippageAllowance: values.gap, LiquidityDomain: "binance-btc-usdt", FencingToken: 1, EvaluationOrdinal: 1,
		EvaluationLogicalTime: 1, ConfigurationHash: work.ConfigurationHash, ConfigurationVersion: config.SchemaVersion,
		FeeModelID: "fee-v1", LatencyModelID: "latency-v1", FillModelID: "fill-v1", SlippageModelID: "slippage-v1",
		GapModelID: "gap-v1", CorrelationModelID: "correlation-v1"}
	return work, market, facts
}

type sandboxTrendInputFixture struct {
	instrument     domain.Instrument
	ask, bid       domain.Price
	quantity       domain.Quantity
	minimum        domain.Notional
	money, maximum domain.Money
	available      domain.Balance
	rate           domain.Rate
	gap, latency   domain.Price
}

func sandboxTrendInputFixtureValues(t *testing.T) sandboxTrendInputFixture {
	t.Helper()
	instrument, instrumentErr := domain.NewSpotInstrument("BTC", "USDT")
	ask, askErr := domain.ParsePrice("300")
	bid, bidErr := domain.ParsePrice("299")
	quantity, quantityErr := domain.ParseQuantity("1")
	minimum, minimumErr := domain.ParseNotional("10")
	money, moneyErr := domain.ParseMoney("75")
	maximum, maximumErr := domain.ParseMoney("10")
	available, availableErr := domain.ParseBalance("500")
	rate, rateErr := domain.ParseRate("0.001")
	gap, gapErr := domain.ParsePrice("0.5")
	latency, latencyErr := domain.ParsePrice("0.1")
	if instrumentErr != nil || askErr != nil || bidErr != nil || quantityErr != nil ||
		minimumErr != nil || moneyErr != nil || maximumErr != nil || availableErr != nil ||
		rateErr != nil || gapErr != nil || latencyErr != nil {
		t.Fatal("sandbox trend fixture decimal invalid")
	}
	return sandboxTrendInputFixture{instrument: instrument, ask: ask, bid: bid, quantity: quantity,
		minimum: minimum, money: money, maximum: maximum, available: available,
		rate: rate, gap: gap, latency: latency}
}
