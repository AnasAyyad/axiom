package runtimecore

// QueueMetrics are bounded-label counters and gauges for one configured queue.
type QueueMetrics struct {
	Capacity      int
	Depth         int
	OldestAge     LogicalTime
	SaturationAge LogicalTime
	Saturations   uint64
	Coalesced     uint64
	Dropped       uint64
	Resyncs       uint64
}

// Metrics returns aggregates keyed only by the closed partition-kind enum.
func (bus *EventBus) Metrics(now LogicalTime) map[PartitionKind]QueueMetrics {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	result := make(map[PartitionKind]QueueMetrics, 5)
	for _, queue := range bus.partitions {
		metrics := queue.metrics
		if len(queue.events) > 0 && now >= queue.events[0].EnqueuedAt {
			metrics.OldestAge = now - queue.events[0].EnqueuedAt
		}
		if queue.saturatedAt > 0 && now >= queue.saturatedAt {
			metrics.SaturationAge = now - queue.saturatedAt
		}
		aggregate := result[queue.partition.Kind]
		aggregate.Capacity += metrics.Capacity
		aggregate.Depth += metrics.Depth
		if metrics.OldestAge > aggregate.OldestAge {
			aggregate.OldestAge = metrics.OldestAge
		}
		if metrics.SaturationAge > aggregate.SaturationAge {
			aggregate.SaturationAge = metrics.SaturationAge
		}
		aggregate.Saturations += metrics.Saturations
		aggregate.Coalesced += metrics.Coalesced
		aggregate.Dropped += metrics.Dropped
		aggregate.Resyncs += metrics.Resyncs
		result[queue.partition.Kind] = aggregate
	}
	return result
}
