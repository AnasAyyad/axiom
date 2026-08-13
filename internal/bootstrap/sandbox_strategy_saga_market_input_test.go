package bootstrap

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/arbitrage"
)

func TestSandboxSagaMarketInputReaderBuildsSynchronizedTriangularAndCrossInputs(t *testing.T) {
	now := time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)
	triangularSource := sagaMarketSource(t, now, []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHBTC")},
		{Exchange: "binance", Instrument: sagaInstrument(t, "ETHUSDT")},
	})
	reader, err := NewSandboxSagaMarketInputReader(triangularSource)
	if err != nil {
		t.Fatal(err)
	}
	triWork := sagaPlanFacts(t, sandbox.StrategyTriangular, now).Coordinator.Work
	tri, err := reader.ReadTriangular(context.Background(), triWork, now)
	if err != nil || len(tri.Markets) != 3 || len(tri.CoherentViewID) != 64 ||
		len(tri.InstrumentMetadataID) != 64 || tri.Trigger.MonotonicNanos != 1_000_000 {
		t.Fatalf("triangular input=%#v error=%v", tri, err)
	}
	if riskMarket, riskErr := tri.RiskMarket(triWork); riskErr != nil ||
		len(riskMarket.Candles) != 0 || sandbox.StrategyMarketEvidenceHash(riskMarket) == "" {
		t.Fatalf("triangular risk market=%#v error=%v", riskMarket, riskErr)
	}
	for _, market := range tri.Markets {
		if len(market.Snapshot.RawPayloadHash) != 64 || market.Observation.Validate() != nil {
			t.Fatalf("triangular market=%#v", market)
		}
	}

	crossKeys := []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "bybit", Instrument: sagaInstrument(t, "BTCUSDT")},
	}
	crossSource := sagaMarketSource(t, now, crossKeys)
	reader, _ = NewSandboxSagaMarketInputReader(crossSource)
	crossWork := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now).Coordinator.Work
	cross, err := reader.ReadCrossExchange(context.Background(), crossWork, now)
	if err != nil || len(cross.Markets) != 2 || len(cross.Coherent.Identity) != 64 ||
		len(cross.Coherent.Members) != 2 || len(cross.InstrumentMetadataSetHash) != 64 {
		t.Fatalf("cross input=%#v error=%v", cross, err)
	}
	for _, admission := range sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now).Admissions {
		if riskMarket, riskErr := cross.RiskMarket(admission.Work); riskErr != nil ||
			len(riskMarket.Candles) != 0 || sandbox.StrategyMarketEvidenceHash(riskMarket) == "" {
			t.Fatalf("cross risk market=%#v error=%v", riskMarket, riskErr)
		}
	}
}

func TestSandboxSagaMarketInputReaderRejectsStaleSkewedAndCredentialBoundarySubstitution(t *testing.T) {
	now := time.Date(2026, 8, 9, 14, 5, 0, 0, time.UTC)
	keys := []runtimecore.MarketKey{
		{Exchange: "binance", Instrument: sagaInstrument(t, "BTCUSDT")},
		{Exchange: "bybit", Instrument: sagaInstrument(t, "BTCUSDT")},
	}
	work := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now).Coordinator.Work
	for name, mutate := range map[string]func(*sagaMarketViewSource){
		"stale": func(source *sagaMarketViewSource) {
			source.set.Trigger.UTC = now.Add(-time.Second)
		},
		"skewed": func(source *sagaMarketViewSource) {
			member := source.set.Members[1]
			member.View = sagaBookView(t, member.View.Exchange(), member.View.Instrument(), now,
				member.View.Observation().ReceivedOffsetNanos+300_000_000, 20)
			source.set.Members[1] = member
		},
		"wrong_exchange": func(source *sagaMarketViewSource) {
			source.set.Members[1] = source.set.Members[0]
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := sagaMarketSource(t, now, keys)
			mutate(source)
			reader, _ := NewSandboxSagaMarketInputReader(source)
			if _, err := reader.ReadCrossExchange(context.Background(), work, now); err == nil {
				t.Fatal("unsafe multi-market capture accepted")
			}
		})
	}
	bybitWork := sagaPlanFacts(t, sandbox.StrategyCrossExchangeArbitrage, now).
		Admissions[sandbox.ExchangeBybit].Work
	reader, _ := NewSandboxSagaMarketInputReader(sagaMarketSource(t, now, keys))
	if _, err := reader.ReadCrossExchange(context.Background(), bybitWork, now); err == nil {
		t.Fatal("credential-owning Bybit engine became the cross coordinator")
	}
}

type sagaMarketViewSource struct{ set SandboxSagaMarketViewSet }

func (source *sagaMarketViewSource) CaptureSandboxSagaMarketViews(
	context.Context,
	[]runtimecore.MarketKey,
	time.Time,
) (SandboxSagaMarketViewSet, error) {
	return source.set, nil
}

func sagaMarketSource(
	t *testing.T,
	now time.Time,
	keys []runtimecore.MarketKey,
) *sagaMarketViewSource {
	t.Helper()
	members := make([]SandboxSagaMarketMember, 0, len(keys))
	for index, key := range keys {
		offset := uint64(900_000 + index*10_000)
		view := sagaBookView(t, key.Exchange, key.Instrument, now, offset, uint64(index+1))
		members = append(members, SandboxSagaMarketMember{View: view,
			Clock: exchangecontracts.ClockHealth{ObservedAt: now.Add(-time.Second),
				Uncertainty: time.Millisecond, Eligible: true},
			Rules:             sagaInstrumentRules(t, key.Exchange, key.Instrument, now),
			CollectorInstance: "sandbox-collector-" + key.Exchange, CollectorRegion: "local-region"})
	}
	return &sagaMarketViewSource{set: SandboxSagaMarketViewSet{
		Trigger: runtimecore.AsOfTrigger{MonotonicNanos: 1_000_000,
			IngestOrdinal: 100, UTC: now},
		FirstDetectedOffset: 990_000, Members: members}}
}

func sagaBookView(
	t *testing.T,
	exchange string,
	instrument domain.Instrument,
	now time.Time,
	offset, ordinal uint64,
) marketdata.BookView {
	return sagaBookViewPrices(t, exchange, instrument, now, offset, ordinal, "99", "100")
}

func sagaBookViewPrices(
	t *testing.T,
	exchange string,
	instrument domain.Instrument,
	now time.Time,
	offset, ordinal uint64,
	bidText, askText string,
) marketdata.BookView {
	t.Helper()
	book, err := marketdata.NewBook(exchange, instrument, 50, 50, nil)
	if err != nil || book.BeginGeneration("sandbox-"+exchange+"-"+instrument.Symbol(), 1) != nil {
		t.Fatal("book setup failed")
	}
	observation := marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: now.Add(-20 * time.Millisecond), Sequence: ordinal*3 - 2},
		ProcessedAt:  domain.EventTime{UTC: now.Add(-10 * time.Millisecond), Sequence: ordinal*3 - 1},
		PublishedAt:  domain.EventTime{UTC: now, Sequence: ordinal * 3},
		ConnectionID: "sandbox-" + exchange + "-" + instrument.Symbol(), ConnectionGeneration: 1,
		SourceSequence: ordinal, IngestOrdinal: ordinal,
		ReceivedOffsetNanos: offset, ProcessedOffsetNanos: offset + 1,
		PublishedOffsetNanos: offset + 2,
	}
	bid, _ := domain.ParsePrice(bidText)
	ask, _ := domain.ParsePrice(askText)
	quantity, _ := domain.ParseQuantity("10")
	snapshot := exchangecontracts.BookSnapshot{Exchange: exchangecontracts.ExchangeID(exchange),
		Instrument: instrument, LastSequence: ordinal, ReceivedAt: observation.ReceivedAt,
		Bids:           []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks:           []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}},
		RawPayloadHash: strings.Repeat("a", 64)}
	if err = book.ReplaceSnapshot(snapshot, observation); err != nil {
		t.Fatal(err)
	}
	return book.View()
}

func sagaInstrumentRules(
	t *testing.T,
	exchange string,
	instrument domain.Instrument,
	now time.Time,
) arbitrage.InstrumentRules {
	t.Helper()
	tick, _ := domain.ParsePrice("1")
	step, _ := domain.ParseQuantity("0.000001")
	minimumNotional, _ := domain.ParseNotional("0.01")
	maximum, _ := domain.ParseQuantity("1000")
	fee, _ := domain.ParseRate("0.001")
	return arbitrage.InstrumentRules{Exchange: exchange,
		Metadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1,
			EffectiveAt: now.Add(-time.Minute), PriceTick: tick, QuantityStep: step,
			MinimumQuantity: step, MinimumNotional: minimumNotional},
		MaximumQuantity: maximum, Fee: arbitrage.FeeSchedule{Version: "sandbox-fee-v1",
			Rate: fee, Asset: instrument.Quote}, Active: true, ObservedAt: now.Add(-time.Minute)}
}

func sagaInstrument(t *testing.T, symbol string) domain.Instrument {
	t.Helper()
	instrument, err := sandboxSagaInstrument(symbol)
	if err != nil {
		t.Fatal(err)
	}
	return instrument
}

var _ SandboxSagaMarketViewSource = (*sagaMarketViewSource)(nil)
