package runtimecore

import (
	"strings"
	"testing"
	"time"
)

func TestCoherentViewInitialPolicyExactBoundaries(t *testing.T) {
	policy := InitialB2CoherentPolicy()
	if policy.MaximumBookAge != 250*time.Millisecond ||
		policy.MaximumInterBookSkew != 250*time.Millisecond ||
		policy.MaximumClockUncertainty != 100*time.Millisecond {
		t.Fatalf("initial policy = %#v", policy)
	}

	tests := []struct {
		name       string
		age        time.Duration
		skew       time.Duration
		uncertain  time.Duration
		maximumAge time.Duration
		wantCode   string
	}{
		{name: "inclusive limits", age: 250 * time.Millisecond, skew: 250 * time.Millisecond, uncertain: 100 * time.Millisecond},
		{name: "age exceeds by one nanosecond", age: 250*time.Millisecond + time.Nanosecond, uncertain: time.Millisecond, wantCode: "stale"},
		{name: "skew exceeds by one nanosecond", age: 250*time.Millisecond + time.Nanosecond, skew: 250*time.Millisecond + time.Nanosecond, uncertain: 100 * time.Millisecond, maximumAge: 500 * time.Millisecond, wantCode: "skew"},
		{name: "uncertainty exceeds by one nanosecond", age: 250 * time.Millisecond, uncertain: 100*time.Millisecond + time.Nanosecond, wantCode: "uncertainty"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			views, keys, trigger := coherentFixture(t, test.age, test.skew, test.uncertain)
			candidatePolicy := policy
			if test.maximumAge > 0 {
				candidatePolicy.MaximumBookAge = test.maximumAge
			}
			joined, err := views.CoherentAsOf(keys, trigger, candidatePolicy)
			if test.wantCode == "" {
				if err != nil || joined.Identity() == "" || len(joined.Members()) != 2 {
					t.Fatalf("join = %#v, %v", joined, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantCode) {
				t.Fatalf("error = %v; want %q", err, test.wantCode)
			}
		})
	}
}

func TestCoherentViewRejectsFutureGapInactiveGenerationAndDisjointIntervals(t *testing.T) {
	policy := InitialB2CoherentPolicy()

	t.Run("post trigger only", func(t *testing.T) {
		views := NewMarketViews()
		keys := coherentKeys(t)
		for index, key := range keys {
			mustActivateAndPublish(t, views, key, 1, uint64(200+index), uint64(index+1), time.Millisecond, 0)
		}
		_, err := views.CoherentAsOf(keys, triggerAt(199, 10), policy)
		if err == nil || !strings.Contains(err.Error(), "post_trigger") {
			t.Fatalf("post-trigger error = %v", err)
		}
	})

	t.Run("unresolved gap", func(t *testing.T) {
		views, keys, trigger := coherentFixture(t, 100*time.Nanosecond, 0, time.Millisecond)
		if err := views.RecordGap(ViewGap{Key: keys[0], Generation: 1, FirstMonotonicNanos: 50,
			LastMonotonicNanos: 60, Reason: "transport_gap"}); err != nil {
			t.Fatal(err)
		}
		_, err := views.CoherentAsOf(keys, trigger, policy)
		if err == nil || !strings.Contains(err.Error(), "gap") {
			t.Fatalf("gap error = %v", err)
		}
	})

	t.Run("inactive generation", func(t *testing.T) {
		views, keys, trigger := coherentFixture(t, 100*time.Nanosecond, 0, time.Millisecond)
		if err := views.ActivateGeneration(keys[0], 2); err != nil {
			t.Fatal(err)
		}
		_, err := views.CoherentAsOf(keys, trigger, policy)
		if err == nil || !strings.Contains(err.Error(), "generation") {
			t.Fatalf("generation error = %v", err)
		}
	})

	t.Run("uncertainty intervals do not overlap", func(t *testing.T) {
		views := NewMarketViews()
		keys := coherentKeys(t)
		mustActivateAndPublish(t, views, keys[0], 1, 100, 1, time.Nanosecond, 0)
		mustActivateAndPublish(t, views, keys[1], 1, 100, 2, time.Nanosecond, time.Second)
		_, err := views.CoherentAsOf(keys, triggerAt(110, 3), policy)
		if err == nil || !strings.Contains(err.Error(), "interval") {
			t.Fatalf("interval error = %v", err)
		}
	})
}

func TestCoherentViewAsOfUsesLatestCommittedEligibleVersion(t *testing.T) {
	views := NewMarketViews()
	keys := coherentKeys(t)
	for _, key := range keys {
		if err := views.ActivateGeneration(key, 1); err != nil {
			t.Fatal(err)
		}
	}
	for index, key := range keys {
		mustPublishCoherent(t, views, key, 1, 100, uint64(index+1), time.Millisecond, 0)
		mustPublishCoherent(t, views, key, 2, 300, uint64(index+3), time.Millisecond, 0)
	}
	joined, err := views.CoherentAsOf(keys, triggerAt(200, 10), InitialB2CoherentPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range joined.Members() {
		if member.BookVersion != 1 || member.ReceiveMonotonicNanos != 100 {
			t.Fatalf("member = %#v", member)
		}
	}
}

func TestCoherentViewAsOfRejectsPostTriggerOrdinalEvenWithEarlierReceiveTime(t *testing.T) {
	views := NewMarketViews()
	keys := coherentKeys(t)
	for _, key := range keys {
		if err := views.ActivateGeneration(key, 1); err != nil {
			t.Fatal(err)
		}
	}
	mustPublishCoherent(t, views, keys[0], 1, 100, 11, time.Millisecond, 0)
	mustPublishCoherent(t, views, keys[1], 1, 100, 12, time.Millisecond, 0)
	if _, err := views.CoherentAsOf(keys, triggerAt(200, 10), InitialB2CoherentPolicy()); err == nil || !strings.Contains(err.Error(), "post_trigger") {
		t.Fatalf("post-trigger ordinals accepted: %v", err)
	}
}

func TestCoherentViewHashIsPermutationRestartAndRunStable(t *testing.T) {
	keys := coherentKeys(t)
	var want string
	for run := range 10 {
		views := NewMarketViews()
		order := keys
		if run%2 == 1 {
			order = []MarketKey{keys[1], keys[0]}
		}
		for _, key := range order {
			mustActivateAndPublish(t, views, key, 1, 100, uint64(key.Instrument.Base[0]), time.Millisecond, 0)
		}
		joined, err := views.CoherentAsOf(order, triggerAt(200, 999), InitialB2CoherentPolicy())
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 {
			want = joined.VersionVectorHash()
		} else if joined.VersionVectorHash() != want {
			t.Fatalf("run %d hash = %s; want %s", run, joined.VersionVectorHash(), want)
		}

		restored, err := RestoreMarketViews(views.State())
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := restored.CoherentAsOf(keys, triggerAt(200, 999), InitialB2CoherentPolicy())
		if err != nil || replayed.VersionVectorHash() != want {
			t.Fatalf("restart hash = %s, %v; want %s", replayed.VersionVectorHash(), err, want)
		}
	}
}

func coherentFixture(t *testing.T, age, skew, uncertainty time.Duration) (*MarketViews, []MarketKey, AsOfTrigger) {
	t.Helper()
	views := NewMarketViews()
	keys := coherentKeys(t)
	triggerNanos := uint64(time.Second)
	firstReceive := triggerNanos - uint64(age.Nanoseconds())
	secondReceive := firstReceive + uint64(skew.Nanoseconds())
	mustActivateAndPublish(t, views, keys[0], 1, firstReceive, 1, uncertainty, 0)
	// Keep corrected UTC intervals overlapping so the skew boundary is tested independently.
	mustActivateAndPublish(t, views, keys[1], 1, secondReceive, 2, uncertainty, -skew)
	return views, keys, triggerAt(triggerNanos, 100)
}

func coherentKeys(t *testing.T) []MarketKey {
	t.Helper()
	binance := marketKey(t, "BTC", "USDT")
	bybit := marketKey(t, "BTC", "USDT")
	bybit.Exchange = "bybit"
	return []MarketKey{binance, bybit}
}

func mustActivateAndPublish(t *testing.T, views *MarketViews, key MarketKey, version, monotonic, ordinal uint64,
	uncertainty, utcDelta time.Duration,
) {
	t.Helper()
	if err := views.ActivateGeneration(key, 1); err != nil {
		t.Fatal(err)
	}
	mustPublishCoherent(t, views, key, version, monotonic, ordinal, uncertainty, utcDelta)
}

func mustPublishCoherent(t *testing.T, views *MarketViews, key MarketKey, version, monotonic, ordinal uint64,
	uncertainty, utcDelta time.Duration,
) {
	t.Helper()
	input := testMarketViewInput(key, version, monotonic, ordinal)
	input.ReceiveUTC = time.Unix(0, int64(monotonic)).Add(utcDelta).UTC()
	input.ClockUncertainty = uncertainty
	input.StateHash = PayloadDigest([]byte(key.Exchange + key.Instrument.Symbol() + time.Duration(monotonic).String()))
	if _, err := views.Publish(input); err != nil {
		t.Fatal(err)
	}
}

func triggerAt(monotonic, ordinal uint64) AsOfTrigger {
	return AsOfTrigger{MonotonicNanos: monotonic, IngestOrdinal: ordinal,
		UTC: time.Unix(0, int64(monotonic)).UTC()}
}
