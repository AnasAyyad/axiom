package runtimecore

import (
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestKeyedRandomnessIsReproducibleSeparatedAndOrderIndependent(t *testing.T) {
	seed := make([]byte, 32)
	for index := range seed {
		seed[index] = byte(index + 1)
	}
	first, _ := NewRandomness(seed)
	second, _ := NewRandomness(seed)
	key := RandomKey{RunID: "run-a", ComponentID: "strategy-a", DecisionID: "decision-a", OrderLegID: "leg-a", EventID: "event-a"}
	want, err := first.Uint64(key, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := second.Uint64(key, 7); got != want {
		t.Fatal("same root/key/index produced a different draw")
	}
	unrelated := key
	unrelated.EventID = "event-unrelated"
	if got, _ := first.Uint64(unrelated, 7); got == want {
		t.Fatal("key separation failed")
	}
	for index := uint64(0); index < 100; index++ {
		_, _ = first.Uint64(unrelated, index)
	}
	if got, _ := first.Uint64(key, 7); got != want {
		t.Fatal("unrelated work changed a keyed draw")
	}
}

func TestKeyedRandomnessConcurrentCallsAreStable(t *testing.T) {
	randomness, _ := NewRandomness(make([]byte, 32))
	key := RandomKey{RunID: "r", ComponentID: "c", DecisionID: "d", OrderLegID: "o", EventID: "e"}
	want, _ := randomness.Uint64(key, 42)
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := randomness.Uint64(key, 42)
			if err != nil || got != want {
				t.Errorf("draw = %d, %v", got, err)
			}
		}()
	}
	group.Wait()
}

func TestMarketViewVectorIsCompleteImmutableAndCanonical(t *testing.T) {
	views := NewMarketViews()
	btc := marketKey(t, "BTC", "USDT")
	eth := marketKey(t, "ETH", "USDT")
	for _, key := range []MarketKey{eth, btc} {
		if err := views.ActivateGeneration(key, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := views.Publish(testMarketViewInput(key, 1, 1, 1)); err != nil {
			t.Fatal(err)
		}
	}
	vector, err := views.Capture([]MarketKey{eth, btc})
	if err != nil {
		t.Fatal(err)
	}
	references := vector.References()
	if len(references) != 2 || references[0].Key.Instrument.Symbol() != "BTCUSDT" || vector.Hash() == "" {
		t.Fatalf("vector = %#v", references)
	}
	references[0].BookVersion = 99
	if vector.References()[0].BookVersion != 1 {
		t.Fatal("vector mutated through returned slice")
	}
	if _, err := views.Capture([]MarketKey{btc, marketKey(t, "BTC", "ETH")}); err == nil {
		t.Fatal("incomplete vector accepted")
	}
	invalid := testMarketViewInput(btc, 3, 3, 3)
	if _, err := views.Publish(invalid); err == nil {
		t.Fatal("non-monotonic view version accepted")
	}
}

func TestMarketViewIdentityCannotCollideOnConcatenatedSymbols(t *testing.T) {
	views := NewMarketViews()
	left := marketKey(t, "AB", "CDE")
	right := marketKey(t, "ABC", "DE")
	if left.Instrument.Symbol() != right.Instrument.Symbol() {
		t.Fatal("adversarial fixture does not share a display symbol")
	}
	for index, key := range []MarketKey{left, right} {
		if err := views.ActivateGeneration(key, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := views.Publish(testMarketViewInput(key, 1, uint64(index+1), uint64(index+1))); err != nil {
			t.Fatal(err)
		}
	}
	vector, err := views.Capture([]MarketKey{left, right})
	if err != nil || len(vector.References()) != 2 {
		t.Fatalf("distinct instrument identities collapsed: %#v, %v", vector.References(), err)
	}
}

func testMarketViewInput(key MarketKey, version, monotonic, ordinal uint64) MarketViewInput {
	return MarketViewInput{Key: key, BookVersion: version, ConnectionGeneration: 1,
		ReceiveMonotonicNanos: monotonic, ReceiveUTC: time.Date(2026, 7, 14, 8, 0, 0, int(monotonic), time.UTC),
		IngestOrdinal: ordinal, ClockOffset: 0, ClockUncertainty: time.Nanosecond,
		StateHash:         PayloadDigest([]byte(key.Exchange + key.Instrument.Symbol())),
		CollectorInstance: "collector-1", CollectorRegion: "test-region"}
}

func marketKey(t *testing.T, base, quote domain.AssetSymbol) MarketKey {
	t.Helper()
	instrument, err := domain.NewSpotInstrument(base, quote)
	if err != nil {
		t.Fatal(err)
	}
	return MarketKey{Exchange: "binance", Instrument: instrument}
}
