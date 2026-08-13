package sandbox

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestStrategyMarketInputReaderAcceptsOnlyFreshFinalizedCompletePublicInputs(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	data := validStrategyMarketData(t, instrument, now)
	reader, err := NewStrategyMarketInputReader(data, clock)
	if err != nil {
		t.Fatal(err)
	}
	input, err := reader.Read(context.Background(), instrument, StrategyMarketRequirements{
		CandleIntervals: []string{"1h", "4h"}, CandleLimit: 2, BookDepth: 50,
		MaximumBookAge: 250 * time.Millisecond,
	})
	if err != nil || input.Instrument != instrument || len(input.Candles["1h"]) != 2 ||
		len(input.Candles["4h"]) != 2 || input.ObservedAt.UTC != now {
		t.Fatalf("input=%#v error=%v", input, err)
	}
	bound := domain.EventTime{UTC: now, Sequence: 99}
	input, err = reader.ReadAt(context.Background(), instrument, StrategyMarketRequirements{
		CandleIntervals: []string{"1h", "4h"}, CandleLimit: 2, BookDepth: 50,
		MaximumBookAge: 250 * time.Millisecond,
	}, bound)
	if err != nil || input.ObservedAt != bound {
		t.Fatalf("bound input=%#v error=%v", input, err)
	}
}

func TestStrategyMarketInputReaderFailsClosedForStalePartialAndMismatchedFacts(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	clock, err := domain.NewReplayClock(now)
	if err != nil {
		t.Fatal(err)
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	requirements := StrategyMarketRequirements{CandleIntervals: []string{"4h"}, CandleLimit: 2,
		BookDepth: 50, MaximumBookAge: 250 * time.Millisecond}
	for _, test := range []struct {
		name   string
		mutate func(*strategyMarketData)
	}{
		{name: "insufficient warmup", mutate: func(data *strategyMarketData) { data.candles["4h"] = data.candles["4h"][:1] }},
		{name: "stale book", mutate: func(data *strategyMarketData) { data.book.ReceivedAt.UTC = now.Add(-251 * time.Millisecond) }},
		{name: "partial candle", mutate: func(data *strategyMarketData) { data.candles["4h"][1].Closed = false }},
		{name: "mismatched metadata", mutate: func(data *strategyMarketData) {
			data.metadata.Metadata.Instrument, _ = domain.NewSpotInstrument("ETH", "USDT")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := validStrategyMarketData(t, instrument, now)
			test.mutate(data)
			reader, newErr := NewStrategyMarketInputReader(data, clock)
			if newErr != nil {
				t.Fatal(newErr)
			}
			if _, readErr := reader.Read(context.Background(), instrument, requirements); readErr == nil {
				t.Fatal("unsafe market input accepted")
			}
		})
	}
}

type strategyMarketData struct {
	metadata exchangecontracts.InstrumentRecord
	book     exchangecontracts.BookSnapshot
	candles  map[string][]exchangecontracts.Candle
}

func (data *strategyMarketData) Snapshot(context.Context, exchangecontracts.SnapshotRequest) (exchangecontracts.BookSnapshot, error) {
	return data.book, nil
}
func (data *strategyMarketData) Subscribe(context.Context, exchangecontracts.StreamRequest) (exchangecontracts.Stream, error) {
	return nil, fmt.Errorf("not used")
}
func (data *strategyMarketData) Instruments(context.Context, []domain.Instrument) ([]exchangecontracts.InstrumentRecord, error) {
	return []exchangecontracts.InstrumentRecord{data.metadata}, nil
}
func (data *strategyMarketData) Trades(context.Context, exchangecontracts.HistoryRequest) ([]exchangecontracts.Trade, error) {
	return nil, fmt.Errorf("not used")
}
func (data *strategyMarketData) Candles(_ context.Context, request exchangecontracts.CandleRequest) ([]exchangecontracts.Candle, error) {
	return append([]exchangecontracts.Candle(nil), data.candles[request.Interval]...), nil
}

func validStrategyMarketData(t *testing.T, instrument domain.Instrument, now time.Time) *strategyMarketData {
	t.Helper()
	price, err := domain.ParsePrice("100")
	if err != nil {
		t.Fatal(err)
	}
	quantity, err := domain.ParseQuantity("1")
	if err != nil {
		t.Fatal(err)
	}
	minimum, err := domain.ParseNotional("10")
	if err != nil {
		t.Fatal(err)
	}
	return &strategyMarketData{metadata: exchangecontracts.InstrumentRecord{Metadata: domain.InstrumentMetadata{
		Instrument: instrument, Version: 1, EffectiveAt: now.Add(-time.Hour), PriceTick: price,
		QuantityStep: quantity, MinimumQuantity: quantity, MinimumNotional: minimum,
	}, RawPayloadHash: strings.Repeat("a", 64)}, book: exchangecontracts.BookSnapshot{
		Instrument: instrument, LastSequence: 1, ReceivedAt: domain.EventTime{UTC: now, Sequence: 1},
		Bids: []exchangecontracts.PriceLevel{{Price: price, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: price, Quantity: quantity}}, RawPayloadHash: strings.Repeat("b", 64),
	}, candles: map[string][]exchangecontracts.Candle{
		"1h": strategyMarketCandles(instrument, "1h", now, price),
		"4h": strategyMarketCandles(instrument, "4h", now, price),
	}}
}

func strategyMarketCandles(instrument domain.Instrument, interval string, now time.Time, price domain.Price) []exchangecontracts.Candle {
	width := time.Hour
	if interval == "4h" {
		width = 4 * time.Hour
	}
	return []exchangecontracts.Candle{
		{Instrument: instrument, Interval: interval, OpenTime: now.Add(-2 * width), CloseTime: now.Add(-width),
			Open: price, High: price, Low: price, Close: price, Closed: true, RawPayloadHash: strings.Repeat("c", 64)},
		{Instrument: instrument, Interval: interval, OpenTime: now.Add(-width), CloseTime: now.Add(-time.Nanosecond),
			Open: price, High: price, Low: price, Close: price, Closed: true, RawPayloadHash: strings.Repeat("d", 64)},
	}
}
