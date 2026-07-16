package trend

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestEMASimpleMeanSeedAndHalfEvenProgression(t *testing.T) {
	values := []domain.Price{price(t, "1"), price(t, "2"), price(t, "3"), price(t, "4"), price(t, "5")}
	result, err := EMA(values, 3)
	if err != nil || result.String() != "4" {
		t.Fatalf("EMA = %s, %v", result.String(), err)
	}
}

func TestATRExactTrueRangeAndWilderSeed(t *testing.T) {
	candles := []exchangecontracts.Candle{
		indicatorCandle(t, 0, "8", "10", "9"),
		indicatorCandle(t, 1, "9", "12", "11"),
		indicatorCandle(t, 2, "10", "13", "12"),
		indicatorCandle(t, 3, "11", "14", "13"),
	}
	result, err := ATR(candles, 3)
	if err != nil || result.String() != "2.777777777777777778" {
		t.Fatalf("ATR = %s, %v", result.String(), err)
	}
}

func indicatorCandle(t *testing.T, index int, low, high, close string) exchangecontracts.Candle {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	open := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * 4 * time.Hour)
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "4h",
		OpenTime: open, CloseTime: open.Add(4 * time.Hour), Open: price(t, close), High: price(t, high),
		Low: price(t, low), Close: price(t, close), Volume: quantity(t, "1"), Closed: true,
		ReceivedAt: domain.EventTime{UTC: open.Add(4 * time.Hour), Sequence: uint64(index + 1)}, RawPayloadHash: "indicator"}
}

func price(t *testing.T, value string) domain.Price {
	t.Helper()
	result, err := domain.ParsePrice(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func quantity(t *testing.T, value string) domain.Quantity {
	t.Helper()
	result, err := domain.ParseQuantity(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
