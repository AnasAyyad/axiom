package exchangecontracts

import (
	"testing"
	"time"
)

func TestEstimateClockHealthUsesMidpointAndInclusiveUncertainty(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	health, err := EstimateClockHealth(base, base.Add(200*time.Millisecond), base.Add(150*time.Millisecond),
		time.Second, time.Second+200*time.Millisecond, 100*time.Millisecond)
	if err != nil || !health.Eligible || health.Uncertainty != 100*time.Millisecond ||
		health.Offset != 50*time.Millisecond {
		t.Fatalf("health = %#v, %v", health, err)
	}
	health, err = EstimateClockHealth(base, base.Add(200*time.Millisecond+2*time.Nanosecond),
		base.Add(150*time.Millisecond), time.Second, time.Second+200*time.Millisecond, 100*time.Millisecond)
	if err != nil || health.Eligible || health.Uncertainty != 100*time.Millisecond+2*time.Nanosecond {
		t.Fatalf("excess health = %#v, %v", health, err)
	}
}

func TestEstimateClockHealthRejectsInvalidEvidence(t *testing.T) {
	base := time.Unix(1_700_000_000, 0).UTC()
	if _, err := EstimateClockHealth(base, base, base, time.Second, time.Second-time.Nanosecond,
		100*time.Millisecond); err == nil {
		t.Fatal("monotonic regression accepted")
	}
}

func TestClockEstimatorConservativelyIntersectsCompatibleIntervals(t *testing.T) {
	estimator, err := NewClockEstimator(100 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0).UTC()
	first, err := estimator.Observe(base, base.Add(200*time.Millisecond), base.Add(150*time.Millisecond),
		time.Second, time.Second+200*time.Millisecond)
	if err != nil || first.Uncertainty != 100*time.Millisecond {
		t.Fatalf("first estimate = %#v, %v", first, err)
	}
	second, err := estimator.Observe(base.Add(time.Second), base.Add(time.Second+200*time.Millisecond),
		base.Add(time.Second+50*time.Millisecond), 2*time.Second, 2*time.Second+200*time.Millisecond)
	if err != nil || !second.Eligible || second.Offset != 0 || second.Uncertainty != 50*time.Millisecond {
		t.Fatalf("fused estimate = %#v, %v", second, err)
	}
}

func TestClockEstimatorDoesNotFuseStaleEvidence(t *testing.T) {
	t.Parallel()
	estimator, err := NewClockEstimator(500 * time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	if _, err = estimator.Observe(base, base.Add(200*time.Millisecond), base.Add(110*time.Millisecond),
		0, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	later := base.Add(clockFusionWindow + time.Second)
	second, err := estimator.Observe(later, later.Add(200*time.Millisecond), later.Add(140*time.Millisecond),
		0, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if second.Uncertainty != 100*time.Millisecond || second.Offset != 40*time.Millisecond {
		t.Fatalf("stale evidence was fused: %#v", second)
	}
}
