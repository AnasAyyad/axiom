package marketdata

import (
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func TestProviderReturnsImmutableCompletedCandles(t *testing.T) {
	instrument := testInstrument(t)
	store, err := NewCandleStore("binance", instrument, "4h", 2)
	if err != nil {
		t.Fatal(err)
	}
	provider := NewProvider()
	if err = provider.RegisterCandles(store); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		open := time.Unix(1_700_000_000+int64(index*14_400), 0).UTC()
		candle := testCandle(t, instrument, open, testHash(string(rune('a'+index))))
		if err = store.Add(candle, testObservation(1, uint64(index+1), uint64(index+1), uint64(index+1))); err != nil {
			t.Fatal(err)
		}
	}
	view, err := provider.CompletedCandles("binance", instrument, "4h")
	if err != nil || view.Version() != 3 || len(view.Candles()) != 2 {
		t.Fatalf("candle view = %#v, %v", view.record, err)
	}
	copy := view.Candles()
	copy[0].RawPayloadHash = "changed"
	if providerView, _ := provider.CompletedCandles("binance", instrument, "4h"); providerView.Candles()[0].RawPayloadHash == "changed" {
		t.Fatal("caller mutated candle store")
	}
	if err = store.Add(view.Candles()[1], testObservation(1, 9, 9, 9)); err != nil {
		t.Fatalf("identical duplicate rejected: %v", err)
	}
}

func TestCandleStoreRejectsOpenAndConflictingCandles(t *testing.T) {
	instrument := testInstrument(t)
	store, _ := NewCandleStore("binance", instrument, "4h", 2)
	open := time.Unix(1_700_000_000, 0).UTC()
	candle := testCandle(t, instrument, open, testHash("a"))
	candle.Closed = false
	if err := store.Add(candle, testObservation(1, 1, 1, 1)); errorCode(err) != "candle_rejected" {
		t.Fatalf("open candle error = %v", err)
	}
	candle.Closed = true
	if err := store.Add(candle, testObservation(1, 1, 1, 1)); err != nil {
		t.Fatal(err)
	}
	candle.RawPayloadHash = testHash("b")
	if err := store.Add(candle, testObservation(1, 2, 2, 2)); errorCode(err) != "candle_conflict" {
		t.Fatalf("conflict error = %v", err)
	}
}

func testCandle(t *testing.T, instrument domain.Instrument, open time.Time, hash string) exchangecontracts.Candle {
	t.Helper()
	price, _ := domain.ParsePrice("100")
	quantity, _ := domain.ParseQuantity("10")
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "4h", OpenTime: open,
		CloseTime: open.Add(4 * time.Hour), Open: price, High: price, Low: price, Close: price,
		Volume: quantity, Closed: true, ReceivedAt: testEventTime(1), RawPayloadHash: hash}
}
