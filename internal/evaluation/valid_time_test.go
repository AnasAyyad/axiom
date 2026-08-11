package evaluation

import (
	"testing"
	"time"
)

func TestValidTimeExcludesUnhealthyAndOverlappingIntervals(t *testing.T) {
	start := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	healthy := ValidTimeObservation{Start: start, End: start.Add(time.Hour), AllFeedsHealthy: true,
		PersistenceHealthy: true, ClockSafe: true, NoQueueDrops: true, EvidenceRecorded: true}
	total, end, ok := AccumulateValidTime(0, time.Time{}, healthy)
	if !ok || total != time.Hour || end != healthy.End {
		t.Fatalf("total=%s end=%s ok=%t", total, end, ok)
	}
	unhealthy := healthy
	unhealthy.Start, unhealthy.End, unhealthy.NoQueueDrops = end, end.Add(time.Hour), false
	if next, _, accepted := AccumulateValidTime(total, end, unhealthy); accepted || next != total {
		t.Fatalf("unhealthy interval counted: %s", next)
	}
	overlap := healthy
	overlap.Start, overlap.End = start.Add(30*time.Minute), start.Add(90*time.Minute)
	if _, _, accepted := AccumulateValidTime(total, end, overlap); accepted {
		t.Fatal("overlapping interval accepted")
	}
}
