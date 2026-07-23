package triangular

import (
	"sync"
	"testing"
)

func TestOpportunityLifetimeTracksPeakArrivalAndLatencySurvival(t *testing.T) {
	tracker := NewLifetimeTracker()
	if _, err := tracker.Observe("binance/forward/25", 100, percent("0.002"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Observe("binance/forward/25", 180, percent("0.004"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Observe("binance/forward/25", 240, percent("0"), false); err != nil {
		t.Fatal(err)
	}
	snapshot, err := tracker.RecordArrival("binance/forward/25", 160, percent("0.003"), 50, 80, 100)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.PeakEdge.String() != "0.004" || snapshot.EdgeAtArrival.String() != "0.003" ||
		snapshot.TotalLifetimeNanos != 80 || !snapshot.SurvivedP50 || !snapshot.SurvivedP95 ||
		snapshot.SurvivedP99 {
		t.Fatalf("unexpected lifetime: %#v", snapshot)
	}
}

func TestOpportunityLifetimeIsRaceSafeAndCanonical(t *testing.T) {
	tracker := NewLifetimeTracker()
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			key := "key-" + uintString(uint64(index))
			_, _ = tracker.Observe(key, uint64(index+1), percent("0.002"), true)
		}(index)
	}
	wait.Wait()
	items := tracker.Snapshots()
	if len(items) != 20 {
		t.Fatalf("lost observations: %d", len(items))
	}
	for index := 1; index < len(items); index++ {
		if items[index-1].Key >= items[index].Key {
			t.Fatal("snapshots are not canonical")
		}
	}
}

func TestOpportunityLifetimeWindowIsBoundedAndExistingItemsRemainWritable(t *testing.T) {
	tracker := NewLifetimeTrackerWithLimit(2)
	if _, err := tracker.Observe("one", 1, percent("0.002"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Observe("two", 2, percent("0.002"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.Observe("three", 3, percent("0.002"), true); err == nil {
		t.Fatal("bounded metric window accepted a third identity")
	}
	if _, err := tracker.Observe("one", 4, percent("0.003"), true); err != nil {
		t.Fatalf("bounded window blocked an existing identity: %v", err)
	}
	if len(tracker.Snapshots()) != 2 {
		t.Fatal("metric window exceeded its bound")
	}
}
