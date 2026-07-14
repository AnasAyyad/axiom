package runtimecore

import "sync"

// LeaseMetricState is a bounded lease status label.
type LeaseMetricState string

// Bounded lease metric states.
const (
	LeaseMetricAbsent LeaseMetricState = "absent"
	LeaseMetricHeld   LeaseMetricState = "held"
	LeaseMetricLost   LeaseMetricState = "lost"
)

// RuntimeMetricSnapshot contains integer diagnostics outside decision authority.
type RuntimeMetricSnapshot struct {
	SchedulingLag LogicalTime
	LeaseState    LeaseMetricState
	ShutdownNanos uint64
}

// RuntimeMetrics records bounded-label scheduler, lease, and shutdown diagnostics.
type RuntimeMetrics struct {
	mutex    sync.RWMutex
	snapshot RuntimeMetricSnapshot
}

// NewRuntimeMetrics constructs safe absent/zero diagnostics.
func NewRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{snapshot: RuntimeMetricSnapshot{LeaseState: LeaseMetricAbsent}}
}

// RecordSchedulingLag updates an integer logical-time gauge.
func (metrics *RuntimeMetrics) RecordSchedulingLag(lag LogicalTime) {
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()
	metrics.snapshot.SchedulingLag = lag
}

// RecordLeaseState accepts only the closed label enum.
func (metrics *RuntimeMetrics) RecordLeaseState(state LeaseMetricState) error {
	if state != LeaseMetricAbsent && state != LeaseMetricHeld && state != LeaseMetricLost {
		return runtimeError("metric_label_rejected", "lease")
	}
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()
	metrics.snapshot.LeaseState = state
	return nil
}

// RecordShutdown records measured non-negative shutdown time.
func (metrics *RuntimeMetrics) RecordShutdown(nanoseconds uint64) {
	metrics.mutex.Lock()
	defer metrics.mutex.Unlock()
	metrics.snapshot.ShutdownNanos = nanoseconds
}

// Snapshot returns a consistent diagnostics value.
func (metrics *RuntimeMetrics) Snapshot() RuntimeMetricSnapshot {
	metrics.mutex.RLock()
	defer metrics.mutex.RUnlock()
	return metrics.snapshot
}
