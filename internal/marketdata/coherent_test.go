package marketdata

import (
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	runtimecore "axiom/internal/runtime"
)

func TestCoherentInputBridgesHealthyBookClockAndRuntimeAuthority(t *testing.T) {
	instrument := testInstrument(t)
	book, err := NewBook("binance", instrument, 10, 20, nil)
	if err != nil || book.BeginGeneration("connection-1", 1) != nil {
		t.Fatal(err)
	}
	if err = book.InstallSnapshot(testSnapshot(t, instrument, 100), testObservation(1, 100, 1, 10)); err != nil {
		t.Fatal(err)
	}
	if err = book.Apply(DepthEvent{Update: testDepth(t, instrument, 101, 101, nil, nil),
		Observation: testObservation(1, 101, 2, 20)}); err != nil {
		t.Fatal(err)
	}
	observedAt := book.View().Observation().ReceivedAt.UTC
	input, err := CoherentInput(book.View(), exchangecontracts.ClockHealth{ObservedAt: observedAt,
		Offset: 2 * time.Millisecond, Uncertainty: 3 * time.Millisecond, Eligible: true}, "collector-1", "test-region")
	if err != nil {
		t.Fatal(err)
	}
	views := runtimecore.NewMarketViews()
	if err = views.ActivateGeneration(input.Key, input.ConnectionGeneration); err != nil {
		t.Fatal(err)
	}
	published, err := views.Publish(input)
	if err != nil || published.Version() != 2 || published.ReceiveMonotonicNanos() != 20 ||
		published.ClockOffset() != 2*time.Millisecond || published.StateHash() == "" {
		t.Fatalf("published = %#v, %v", published, err)
	}

	input, err = CoherentInput(book.View(), exchangecontracts.ClockHealth{}, "collector-1", "test-region")
	if err == nil || input.BookVersion != 0 {
		t.Fatal("ineligible clock evidence accepted")
	}
	input, err = CoherentInput(book.View(), exchangecontracts.ClockHealth{ObservedAt: observedAt.Add(-maximumCoherentClockAge - time.Nanosecond),
		Uncertainty: time.Millisecond, Eligible: true}, "collector-1", "test-region")
	if err == nil || input.BookVersion != 0 {
		t.Fatal("stale clock evidence accepted")
	}
}
