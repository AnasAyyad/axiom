package runtimecore

// PersistenceHandoff admits critical facts only through a bounded queue.
type PersistenceHandoff struct {
	bus       *EventBus
	partition Partition
}

// NewPersistenceHandoff binds a critical configured ownership partition.
func NewPersistenceHandoff(bus *EventBus, partition Partition) (*PersistenceHandoff, error) {
	if bus == nil {
		return nil, runtimeError("invalid_persistence_handoff", "bus")
	}
	return &PersistenceHandoff{bus: bus, partition: partition}, nil
}

// Admit acknowledges only after the critical fact is accepted by bounded capacity.
func (handoff *PersistenceHandoff) Admit(event BusEvent, acknowledge func()) error {
	if event.Class != ClassCritical || acknowledge == nil {
		return runtimeError("persistence_handoff_rejected", "event")
	}
	if err := handoff.bus.Publish(handoff.partition, event); err != nil {
		return err
	}
	acknowledge()
	return nil
}
