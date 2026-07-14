package binance

import (
	"testing"
	"time"
)

func TestConnectionPolicyIsDeterministicBoundedAndRenewable(t *testing.T) {
	policy := ConnectionPolicy{MinimumBackoff: time.Second, MaximumBackoff: 30 * time.Second,
		Renewal: 23 * time.Hour, Seed: "a7-reconnect"}
	prior := time.Duration(0)
	for attempt := uint32(1); attempt <= 20; attempt++ {
		left, err := policy.Backoff(attempt)
		right, secondErr := policy.Backoff(attempt)
		if err != nil || secondErr != nil || left != right || left < policy.MinimumBackoff || left > policy.MaximumBackoff {
			t.Fatalf("attempt %d delay = %s/%s, %v/%v", attempt, left, right, err, secondErr)
		}
		if attempt > 1 && left < prior && prior != policy.MaximumBackoff {
			t.Fatalf("backoff regressed: %s -> %s", prior, left)
		}
		prior = left
	}
	if policy.RenewalDue(22*time.Hour) || !policy.RenewalDue(23*time.Hour) {
		t.Fatal("scheduled renewal boundary is incorrect")
	}
	invalid := policy
	invalid.Renewal = 25 * time.Hour
	if _, err := invalid.Backoff(1); err == nil {
		t.Fatal("unsafe connection policy accepted")
	}
}
