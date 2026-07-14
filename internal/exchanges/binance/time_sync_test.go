package binance

import (
	"testing"
	"time"
)

func TestTimeSynchronizerUsesMonotonicRoundTripAndFailsClosed(t *testing.T) {
	synchronizer, err := NewTimeSynchronizer(50 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0).UTC()
	if err = synchronizer.Observe(base, base.Add(20*time.Millisecond), base.Add(15*time.Millisecond),
		10*time.Second, 10*time.Second+20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	health := synchronizer.Health()
	if !health.Eligible || health.Uncertainty != 10*time.Millisecond || health.Offset != 5*time.Millisecond {
		t.Fatalf("unexpected time health: %#v", health)
	}
	if err = synchronizer.Observe(base, base.Add(200*time.Millisecond), base.Add(100*time.Millisecond),
		20*time.Second, 20*time.Second+200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if synchronizer.Health().Eligible {
		t.Fatal("excessive uncertainty remained eligible")
	}
	if err = synchronizer.Observe(base, base, base, time.Second, time.Second-time.Nanosecond); err == nil {
		t.Fatal("monotonic regression accepted")
	}
}
