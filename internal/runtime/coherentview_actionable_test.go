package runtimecore

import (
	"strings"
	"testing"
	"time"
)

func TestCrossExchangeActionableAsOfKeepsStrictB2Unchanged(t *testing.T) {
	views := NewMarketViews()
	keys := coherentKeys(t)
	mustActivateAndPublish(t, views, keys[0], 1, 900_000_000, 1, time.Millisecond, 0)
	mustActivateAndPublish(t, views, keys[1], 1, 950_000_000, 2, time.Millisecond, time.Second)
	trigger := triggerAt(uint64(time.Second), 10)
	if _, err := views.CoherentAsOf(keys, trigger, InitialCoherentMarketDataCoherentPolicy()); err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatalf("strict B2 accepted disjoint intervals: %v", err)
	}
	actionable, err := views.CrossExchangeActionableAsOf(keys, trigger)
	if err != nil || actionable.Policy() != InitialCrossExchangeActionablePolicy() || len(actionable.Members()) != 2 {
		t.Fatalf("actionable view = %#v, %v", actionable, err)
	}
	restored, err := RestoreCoherentView(actionable.Identity(), actionable.Policy(), actionable.Trigger(), actionable.Members())
	if err != nil || restored.Identity() != actionable.Identity() {
		t.Fatalf("actionable restore = %#v, %v", restored, err)
	}
}

func TestCrossExchangeActionableAsOfFailsClosed(t *testing.T) {
	tests := []struct {
		name        string
		receives    []uint64
		uncertainty time.Duration
		mutate      func([]MarketKey)
		want        string
	}{
		{name: "inclusive limits", receives: []uint64{850_000_000, 1_000_000_000}, uncertainty: 100 * time.Millisecond},
		{name: "stale", receives: []uint64{849_999_999, 950_000_000}, uncertainty: time.Millisecond, want: "stale"},
		{name: "uncertainty", receives: []uint64{900_000_000, 950_000_000}, uncertainty: 100*time.Millisecond + time.Nanosecond, want: "uncertainty"},
		{name: "wrong venue", receives: []uint64{900_000_000, 950_000_000}, uncertainty: time.Millisecond,
			mutate: func(keys []MarketKey) { keys[1].Exchange = "kraken" }, want: "membership"},
		{name: "instrument mismatch", receives: []uint64{900_000_000, 950_000_000}, uncertainty: time.Millisecond,
			mutate: func(keys []MarketKey) { keys[1].Instrument = marketKey(t, "ETH", "USDT").Instrument }, want: "membership"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			views := NewMarketViews()
			keys := coherentKeys(t)
			if test.mutate != nil {
				test.mutate(keys)
			}
			for index, key := range keys {
				mustActivateAndPublish(t, views, key, 1, test.receives[index], uint64(index+1),
					test.uncertainty, 0)
			}
			joined, err := views.CrossExchangeActionableAsOf(keys, triggerAt(uint64(time.Second), 10))
			if test.want == "" {
				if err != nil || joined.Identity() == "" {
					t.Fatalf("join = %#v, %v", joined, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v; want %q", err, test.want)
			}
		})
	}
}

func TestStrictCoherentAsOfRejectsActionablePolicySubstitution(t *testing.T) {
	views, keys, trigger := coherentFixture(t, 100*time.Millisecond, 10*time.Millisecond, time.Millisecond)
	if _, err := views.CoherentAsOf(keys, trigger, InitialCrossExchangeActionablePolicy()); err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("actionable policy substituted into strict B2: %v", err)
	}
}
