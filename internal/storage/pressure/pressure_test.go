package pressure

import (
	"math"
	"testing"
	"time"
)

func TestPolicyClassifiesExactWatermarks(t *testing.T) {
	policy := Policy{HighFreeBytes: 10 << 30, CriticalFreeBytes: 5 << 30,
		SampleInterval: 15 * time.Second}
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	checks := []struct {
		available uint64
		want      Level
	}{
		{available: 11 << 30, want: LevelNormal},
		{available: 10 << 30, want: LevelHigh},
		{available: 5 << 30, want: LevelCritical},
	}
	for _, check := range checks {
		observation, err := policy.Classify(check.available, 100<<30, now)
		if err != nil || observation.Level != check.want {
			t.Fatalf("available=%d level=%s want=%s error=%v", check.available,
				observation.Level, check.want, err)
		}
	}
}

func TestPolicyRejectsWeakenedReserveAndInvalidSample(t *testing.T) {
	if err := (Policy{HighFreeBytes: 9 << 30, CriticalFreeBytes: 5 << 30,
		SampleInterval: 15 * time.Second}).Validate(); err == nil {
		t.Fatal("high free-byte reserve below ten GiB accepted")
	}
	policy := Policy{HighFreeBytes: 10 << 30, CriticalFreeBytes: 5 << 30,
		SampleInterval: 15 * time.Second}
	if _, err := policy.Classify(101, 100, time.Now().UTC()); err == nil {
		t.Fatal("available bytes above total accepted")
	}
	if _, err := policy.Classify(100, math.MaxUint64, time.Now().UTC()); err == nil {
		t.Fatal("filesystem capacity above PostgreSQL bigint accepted")
	}
}

func TestFilesystemProbeIsBounded(t *testing.T) {
	policy := Policy{HighFreeBytes: 10 << 30, CriticalFreeBytes: 5 << 30,
		SampleInterval: 15 * time.Second}
	observation, err := policy.Probe(t.TempDir(), time.Now().UTC())
	if err != nil || observation.TotalBytes == 0 || observation.AvailableBytes > observation.TotalBytes {
		t.Fatalf("filesystem observation=%+v error=%v", observation, err)
	}
}
