package runtimecore

import (
	"strings"
	"testing"
	"time"
)

func TestSameExchangeTriangularAsOfAcceptsSharedClockDisjointIntervals(t *testing.T) {
	views := NewMarketViews()
	keys := sameExchangeTriangularKeys(t)
	receives := []uint64{900_000_000, 950_000_000, 980_000_000}
	utcDeltas := []time.Duration{0, time.Second, -time.Second}
	for index, key := range keys {
		mustActivateAndPublish(t, views, key, 1, receives[index], uint64(index+1),
			time.Millisecond, utcDeltas[index])
	}
	trigger := triggerAt(uint64(time.Second), 10)
	joined, err := views.SameExchangeTriangularAsOf(keys, trigger, 100*time.Millisecond)
	if err != nil || len(joined.Members()) != 3 || joined.Policy().MaximumBookAge != 100*time.Millisecond {
		t.Fatalf("same-exchange join = %#v, %v", joined, err)
	}
	if _, err = RestoreCoherentView(joined.Identity(), joined.Policy(), joined.Trigger(), joined.Members()); err != nil {
		t.Fatalf("same-exchange restore = %v", err)
	}
}

func TestSameExchangeTriangularAsOfFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		receives  []uint64
		uncertain time.Duration
		mutate    func([]MarketKey)
		want      string
	}{
		{name: "inclusive 100ms", receives: []uint64{900_000_000, 950_000_000, 1_000_000_000}, uncertain: 100 * time.Millisecond},
		{name: "stale", receives: []uint64{899_999_999, 950_000_000, 980_000_000}, uncertain: time.Millisecond, want: "stale"},
		{name: "uncertainty", receives: []uint64{950_000_000, 960_000_000, 970_000_000}, uncertain: 100*time.Millisecond + time.Nanosecond, want: "uncertainty"},
		{name: "cross exchange", receives: []uint64{950_000_000, 960_000_000, 970_000_000}, uncertain: time.Millisecond,
			mutate: func(keys []MarketKey) { keys[2].Exchange = "bybit" }, want: "exchange"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			views := NewMarketViews()
			keys := sameExchangeTriangularKeys(t)
			if test.mutate != nil {
				test.mutate(keys)
			}
			for index, key := range keys {
				mustActivateAndPublish(t, views, key, 1, test.receives[index], uint64(index+1),
					test.uncertain, 0)
			}
			joined, err := views.SameExchangeTriangularAsOf(keys, triggerAt(uint64(time.Second), 10),
				100*time.Millisecond)
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

func TestSameExchangeTriangularAsOfRejectsPostTriggerAndInactiveGeneration(t *testing.T) {
	t.Run("post trigger", func(t *testing.T) {
		views := NewMarketViews()
		keys := sameExchangeTriangularKeys(t)
		for index, key := range keys {
			mustActivateAndPublish(t, views, key, 1, 200, uint64(index+1), time.Millisecond, 0)
		}
		if _, err := views.SameExchangeTriangularAsOf(keys, triggerAt(199, 10),
			100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "post_trigger") {
			t.Fatalf("post-trigger views accepted: %v", err)
		}
	})

	t.Run("inactive generation", func(t *testing.T) {
		views := NewMarketViews()
		keys := sameExchangeTriangularKeys(t)
		for index, key := range keys {
			mustActivateAndPublish(t, views, key, 1, uint64(950+index*10), uint64(index+1), time.Millisecond, 0)
		}
		if err := views.ActivateGeneration(keys[1], 2); err != nil {
			t.Fatal(err)
		}
		if _, err := views.SameExchangeTriangularAsOf(keys, triggerAt(1_000, 10),
			100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "generation") {
			t.Fatalf("inactive generation accepted: %v", err)
		}
	})
}

func TestSameExchangeTriangularAsOfRequiresOneSharedClockEstimate(t *testing.T) {
	views := NewMarketViews()
	keys := sameExchangeTriangularKeys(t)
	for index, key := range keys {
		mustActivateAndPublish(t, views, key, 1, uint64(950_000_000+index*10_000_000),
			uint64(index+1), time.Duration(index+1)*time.Millisecond, 0)
	}
	if _, err := views.SameExchangeTriangularAsOf(keys, triggerAt(uint64(time.Second), 10),
		100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatalf("different clock estimates accepted: %v", err)
	}
}

func TestSameExchangeTriangularAsOfRejectsGapAndUnsafePolicy(t *testing.T) {
	views := NewMarketViews()
	keys := sameExchangeTriangularKeys(t)
	for index, key := range keys {
		mustActivateAndPublish(t, views, key, 1, uint64(950_000_000+index*10_000_000),
			uint64(index+1), time.Millisecond, 0)
	}
	if err := views.RecordGap(ViewGap{Key: keys[1], Generation: 1, FirstMonotonicNanos: 960_000_000,
		LastMonotonicNanos: 970_000_000, Reason: "transport_gap"}); err != nil {
		t.Fatal(err)
	}
	if _, err := views.SameExchangeTriangularAsOf(keys, triggerAt(uint64(time.Second), 10),
		100*time.Millisecond); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("gap accepted: %v", err)
	}
	if _, err := views.SameExchangeTriangularAsOf(keys, triggerAt(uint64(time.Second), 10),
		250*time.Millisecond+time.Nanosecond); err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("unsafe policy accepted: %v", err)
	}
}

func sameExchangeTriangularKeys(t *testing.T) []MarketKey {
	t.Helper()
	return []MarketKey{
		marketKey(t, "BTC", "USDT"),
		marketKey(t, "ETH", "BTC"),
		marketKey(t, "ETH", "USDT"),
	}
}
