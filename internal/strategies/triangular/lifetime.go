package triangular

import (
	"sort"
	"sync"

	"axiom/internal/domain"
)

// LifetimeSnapshot is the complete deterministic opportunity survival record.
type LifetimeSnapshot struct {
	Key                 string
	FirstDetectionNanos uint64
	LastProfitableNanos uint64
	PeakEdge            domain.Percent
	EdgeAtArrival       domain.Percent
	TotalLifetimeNanos  uint64
	SurvivedP50         bool
	SurvivedP95         bool
	SurvivedP99         bool
	Revision            uint64
}

// LifetimeTracker serializes exact opportunity observations without using
// wall-clock time or map iteration for output order.
type LifetimeTracker struct {
	mutex sync.Mutex
	items map[string]LifetimeSnapshot
	limit uint32
}

// NewLifetimeTracker constructs an empty deterministic tracker.
func NewLifetimeTracker() *LifetimeTracker {
	return NewLifetimeTrackerWithLimit(DefaultConfiguration().OpportunityMetricWindow)
}

// NewLifetimeTrackerWithLimit constructs a bounded deterministic tracker.
func NewLifetimeTrackerWithLimit(limit uint32) *LifetimeTracker {
	return &LifetimeTracker{items: make(map[string]LifetimeSnapshot), limit: limit}
}

// Observe records one profitable or terminal observation at a monotonic offset.
func (tracker *LifetimeTracker) Observe(
	key string,
	offset uint64,
	edge domain.Percent,
	profitable bool,
) (LifetimeSnapshot, error) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	if key == "" || offset == 0 {
		return LifetimeSnapshot{}, strategyError("lifetime_observation_invalid")
	}
	item, exists := tracker.items[key]
	if !exists {
		if tracker.limit == 0 || uint32(len(tracker.items)) >= tracker.limit {
			return LifetimeSnapshot{}, strategyError("lifetime_window_full")
		}
		if !profitable {
			return LifetimeSnapshot{}, strategyError("lifetime_first_detection_invalid")
		}
		item = LifetimeSnapshot{
			Key: key, FirstDetectionNanos: offset, LastProfitableNanos: offset, PeakEdge: edge, Revision: 1,
		}
	} else {
		if offset < item.LastProfitableNanos || offset < item.FirstDetectionNanos {
			return LifetimeSnapshot{}, strategyError("lifetime_observation_regressed")
		}
		item.Revision++
	}
	if profitable {
		item.LastProfitableNanos = offset
		if edge.Compare(item.PeakEdge) > 0 {
			item.PeakEdge = edge
		}
	}
	item.TotalLifetimeNanos = item.LastProfitableNanos - item.FirstDetectionNanos
	tracker.items[key] = item
	return item, nil
}

// RecordArrival stores the exact edge and latency survival classifications.
func (tracker *LifetimeTracker) RecordArrival(
	key string,
	arrival uint64,
	edge domain.Percent,
	p50, p95, p99 uint64,
) (LifetimeSnapshot, error) {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	item, exists := tracker.items[key]
	if !exists || arrival < item.FirstDetectionNanos || p50 == 0 || p95 < p50 || p99 < p95 {
		return LifetimeSnapshot{}, strategyError("lifetime_arrival_invalid")
	}
	lifetime := item.LastProfitableNanos - item.FirstDetectionNanos
	item.EdgeAtArrival = edge
	item.TotalLifetimeNanos = lifetime
	item.SurvivedP50 = lifetime >= p50
	item.SurvivedP95 = lifetime >= p95
	item.SurvivedP99 = lifetime >= p99
	item.Revision++
	tracker.items[key] = item
	return item, nil
}

// Snapshots returns canonical key-sorted defensive records.
func (tracker *LifetimeTracker) Snapshots() []LifetimeSnapshot {
	tracker.mutex.Lock()
	defer tracker.mutex.Unlock()
	items := make([]LifetimeSnapshot, 0, len(tracker.items))
	for _, item := range tracker.items {
		items = append(items, item)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].Key < items[right].Key })
	return items
}
