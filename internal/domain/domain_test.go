package domain

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestAggregateIDsAreTypedAndCanonical(t *testing.T) {
	id, err := NewVirtualOrderID("order_0001")
	if err != nil || id.String() != "virtual_order:order_0001" {
		t.Fatalf("id = %q, %v", id.String(), err)
	}
	var parsed VirtualOrderID
	if err := parsed.UnmarshalText([]byte(id.String())); err != nil || parsed != id {
		t.Fatalf("parsed = %#v, %v", parsed, err)
	}
	var wrong DatasetID
	if err := wrong.UnmarshalText([]byte(id.String())); err == nil {
		t.Fatal("wrong aggregate prefix accepted")
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip VirtualOrderID
	if err := json.Unmarshal(encoded, &roundTrip); err != nil || roundTrip != id {
		t.Fatalf("identifier round trip = %#v, %v", roundTrip, err)
	}
}

func TestInitialAssetRegistryAndSpotInstrument(t *testing.T) {
	assets := DefaultAssets()
	if len(assets) != 3 || assets[0].Symbol != "USDT" || assets[1].Symbol != "BTC" || assets[2].Symbol != "ETH" {
		t.Fatalf("unexpected initial assets: %#v", assets)
	}
	for _, asset := range assets {
		if asset.Status != AssetApproved {
			t.Fatalf("initial asset is not approved: %#v", asset)
		}
	}
	instrument, err := NewSpotInstrument("BTC", "USDT")
	if err != nil || instrument.Symbol() != "BTCUSDT" || instrument.Product != ProductSpot {
		t.Fatalf("instrument = %#v, %v", instrument, err)
	}
}

func TestVersionedInstrumentMetadataRequiresUTCAndPositiveFilters(t *testing.T) {
	instrument, _ := NewSpotInstrument("BTC", "USDT")
	minimumNotional, _ := ParseNotional("5")
	metadata := InstrumentMetadata{
		Instrument: instrument, Version: 1,
		EffectiveAt: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC),
		PriceTick:   mustPrice(t, "0.01"), QuantityStep: mustQuantity(t, "0.00001"),
		MinimumQuantity: mustQuantity(t, "0.00001"), MinimumNotional: minimumNotional,
	}
	if err := metadata.Validate(); err != nil {
		t.Fatal(err)
	}
	metadata.EffectiveAt = metadata.EffectiveAt.In(time.FixedZone("UTC-like", 0))
	if err := metadata.Validate(); err == nil {
		t.Fatal("non-canonical UTC location accepted")
	}
}

func TestReplayClockIsDeterministicAndConcurrentSystemClockOrders(t *testing.T) {
	start := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	clock, err := NewReplayClock(start)
	if err != nil {
		t.Fatal(err)
	}
	first, second := clock.Now(), clock.Now()
	if first.UTC != second.UTC || first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("replay times = %#v, %#v", first, second)
	}
	if err := clock.Advance(time.Second); err != nil || clock.Now().UTC != start.Add(time.Second) {
		t.Fatal("replay advance failed")
	}
	system := &SystemClock{}
	sequences := make(chan uint64, 100)
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() { defer group.Done(); sequences <- system.Now().Sequence }()
	}
	group.Wait()
	close(sequences)
	seen := make(map[uint64]struct{})
	for sequence := range sequences {
		seen[sequence] = struct{}{}
	}
	if len(seen) != 100 {
		t.Fatalf("system sequence count = %d", len(seen))
	}
}
