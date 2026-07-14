package runtimecore

import (
	"sync"

	"axiom/internal/domain"
)

// EventClass selects one explicit bounded overload policy.
type EventClass string

// Supported event classes and their fixed loss policies.
const (
	ClassCritical   EventClass = "critical"
	ClassMarketData EventClass = "market_data"
	ClassProjection EventClass = "projection"
)

// PartitionKind declares single-writer ownership scope.
type PartitionKind string

// Supported ownership partition kinds.
const (
	PartitionExchangeInstrument PartitionKind = "exchange_instrument"
	PartitionAccountInstrument  PartitionKind = "account_instrument"
	PartitionOrder              PartitionKind = "order"
	PartitionReservation        PartitionKind = "reservation"
	PartitionRiskState          PartitionKind = "risk_state"
)

// Partition identifies one configured bounded ownership queue.
type Partition struct {
	Kind PartitionKind
	Key  string
}

// BusEvent is immutable queue metadata; authoritative payload remains in its owner.
type BusEvent struct {
	ID          domain.EventID
	Class       EventClass
	Generation  uint64
	CoalesceKey string
	EnqueuedAt  LogicalTime
}

type partitionQueue struct {
	partition        Partition
	class            EventClass
	capacity         int
	snapshotRecovery bool
	generation       uint64
	valid            bool
	events           []BusEvent
	metrics          QueueMetrics
	saturatedAt      LogicalTime
}

// EventBus is a sealed collection of bounded in-process ownership queues.
type EventBus struct {
	mutex      sync.Mutex
	gate       *SafetyGate
	partitions map[string]*partitionQueue
	sealed     bool
}

// NewEventBus constructs an unsealed fail-closed bus.
func NewEventBus(gate *SafetyGate) (*EventBus, error) {
	if gate == nil {
		return nil, runtimeError("invalid_bus", "gate")
	}
	return &EventBus{gate: gate, partitions: make(map[string]*partitionQueue)}, nil
}

// Register declares one bounded partition before the bus is sealed.
func (bus *EventBus) Register(partition Partition, class EventClass, capacity int, snapshotRecovery bool) error {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	if bus.sealed || !validPartition(partition) || !validEventClass(class) || capacity <= 0 {
		return runtimeError("partition_rejected", "registration")
	}
	if class == ClassProjection && !snapshotRecovery {
		return runtimeError("partition_rejected", "projection_recovery")
	}
	identity := partitionIdentity(partition)
	if _, exists := bus.partitions[identity]; exists {
		return runtimeError("partition_rejected", "duplicate")
	}
	bus.partitions[identity] = &partitionQueue{
		partition: partition, class: class, capacity: capacity,
		snapshotRecovery: snapshotRecovery, generation: 1, valid: true,
		metrics: QueueMetrics{Capacity: capacity},
	}
	return nil
}

// Seal prevents runtime label/partition growth.
func (bus *EventBus) Seal() {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	bus.sealed = true
}

// Publish applies the partition's fixed overload policy without blocking.
func (bus *EventBus) Publish(partition Partition, event BusEvent) error {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	queue, err := bus.queue(partition, event)
	if err != nil {
		return err
	}
	if queue.class == ClassMarketData && (!queue.valid || event.Generation != queue.generation) {
		return runtimeError("market_generation_rejected", partitionIdentity(partition))
	}
	if queue.class == ClassProjection {
		return bus.publishProjection(queue, event)
	}
	if len(queue.events) >= queue.capacity {
		return bus.handleSaturation(queue, event.EnqueuedAt)
	}
	queue.events = append(queue.events, event)
	queue.metrics.Depth = len(queue.events)
	return nil
}

// Consume removes the next event in partition order.
func (bus *EventBus) Consume(partition Partition) (BusEvent, bool) {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	queue, exists := bus.partitions[partitionIdentity(partition)]
	if !exists || len(queue.events) == 0 {
		return BusEvent{}, false
	}
	event := queue.events[0]
	queue.events = queue.events[1:]
	queue.metrics.Depth = len(queue.events)
	if len(queue.events) < queue.capacity {
		queue.saturatedAt = 0
	}
	return event, true
}

// Resynchronize installs a strictly newer empty market-data generation.
func (bus *EventBus) Resynchronize(partition Partition, generation uint64) error {
	bus.mutex.Lock()
	defer bus.mutex.Unlock()
	queue, exists := bus.partitions[partitionIdentity(partition)]
	if !exists || queue.class != ClassMarketData || generation <= queue.generation {
		return runtimeError("resync_rejected", partitionIdentity(partition))
	}
	queue.events = nil
	queue.generation, queue.valid = generation, true
	queue.metrics.Depth = 0
	queue.metrics.Resyncs++
	queue.saturatedAt = 0
	return nil
}

func (bus *EventBus) queue(partition Partition, event BusEvent) (*partitionQueue, error) {
	if !bus.sealed {
		return nil, runtimeError("bus_unsealed", "publish")
	}
	queue, exists := bus.partitions[partitionIdentity(partition)]
	if !exists || event.ID.Value() == "" || event.Class != queue.class || event.EnqueuedAt == 0 {
		return nil, runtimeError("event_rejected", partitionIdentity(partition))
	}
	return queue, nil
}

func (bus *EventBus) publishProjection(queue *partitionQueue, event BusEvent) error {
	if event.CoalesceKey == "" || !queue.snapshotRecovery {
		return runtimeError("event_rejected", "projection")
	}
	for index := range queue.events {
		if queue.events[index].CoalesceKey == event.CoalesceKey {
			queue.events[index] = event
			queue.metrics.Coalesced++
			return nil
		}
	}
	if len(queue.events) >= queue.capacity {
		queue.metrics.Dropped++
		queue.metrics.Saturations++
		if queue.saturatedAt == 0 {
			queue.saturatedAt = event.EnqueuedAt
		}
		return runtimeError("projection_coalesced_to_snapshot", partitionIdentity(queue.partition))
	}
	queue.events = append(queue.events, event)
	queue.metrics.Depth = len(queue.events)
	return nil
}

func (bus *EventBus) handleSaturation(queue *partitionQueue, now LogicalTime) error {
	queue.metrics.Saturations++
	if queue.saturatedAt == 0 {
		queue.saturatedAt = now
	}
	if queue.class == ClassCritical {
		bus.gate.Lock("critical_queue_saturated")
		return runtimeError("critical_queue_saturated", partitionIdentity(queue.partition))
	}
	queue.metrics.Dropped += uint64(len(queue.events) + 1)
	queue.events, queue.valid = nil, false
	queue.metrics.Depth = 0
	bus.gate.Pause("market_generation_invalid")
	return runtimeError("market_queue_invalidated", partitionIdentity(queue.partition))
}

func validPartition(partition Partition) bool {
	if partition.Key == "" || len(partition.Key) > 128 {
		return false
	}
	switch partition.Kind {
	case PartitionExchangeInstrument, PartitionAccountInstrument, PartitionOrder,
		PartitionReservation, PartitionRiskState:
		return true
	default:
		return false
	}
}

func validEventClass(class EventClass) bool {
	return class == ClassCritical || class == ClassMarketData || class == ClassProjection
}

func partitionIdentity(partition Partition) string {
	return string(partition.Kind) + ":" + partition.Key
}
