package replay

import (
	"fmt"
	"time"
)

// FaultKind identifies one deterministic A8 replay failure scenario.
type FaultKind string

// Supported replay and virtual execution faults.
const (
	FaultDisconnect     FaultKind = "disconnect"
	FaultSequenceGap    FaultKind = "sequence_gap"
	FaultLatency        FaultKind = "latency"
	FaultRejection      FaultKind = "rejection"
	FaultPartialFill    FaultKind = "partial_fill"
	FaultCancelFillRace FaultKind = "cancel_fill_race"
	FaultUnknownState   FaultKind = "unknown_state"
	FaultStorageFailure FaultKind = "storage_failure"
	FaultRestart        FaultKind = "restart_at_event"
)

// Fault is one immutable ordinal-keyed injection.
type Fault struct {
	Kind       FaultKind
	Ordinal    uint64
	Delay      time.Duration
	Repeatable bool
}

// FaultEvent is emitted to the engine without mutating canonical input bytes.
type FaultEvent struct {
	Kind    FaultKind
	Ordinal uint64
}

// FaultSource wraps a replay source with sorted deterministic injections.
type FaultSource struct {
	source   Source
	faults   map[uint64]Fault
	emitted  map[uint64]bool
	pending  *Event
	observer func(FaultEvent) error
}

// NewFaultSource validates a fault schedule and constructs a source wrapper.
func NewFaultSource(source Source, faults []Fault, observer func(FaultEvent) error) (*FaultSource, error) {
	if source == nil || observer == nil {
		return nil, replayError("fault_configuration_invalid")
	}
	indexed := make(map[uint64]Fault, len(faults))
	for _, fault := range faults {
		if fault.Ordinal == 0 || !validFault(fault) {
			return nil, replayError("fault_configuration_invalid")
		}
		if _, exists := indexed[fault.Ordinal]; exists {
			return nil, replayError("fault_ordinal_duplicate")
		}
		indexed[fault.Ordinal] = fault
	}
	return &FaultSource{source: source, faults: indexed, emitted: make(map[uint64]bool), observer: observer}, nil
}

// Next injects the configured fault before delivering its selected event.
func (source *FaultSource) Next() (Event, bool, error) {
	if source.pending != nil {
		event := *source.pending
		source.pending = nil
		return cloneEvent(event), true, nil
	}
	event, ok, err := source.source.Next()
	if err != nil || !ok {
		return Event{}, ok, err
	}
	fault, exists := source.faults[event.Ordinal]
	if !exists || source.emitted[event.Ordinal] {
		return cloneEvent(event), true, nil
	}
	if !fault.Repeatable {
		source.emitted[event.Ordinal] = true
	}
	if err = source.observer(FaultEvent{Kind: fault.Kind, Ordinal: fault.Ordinal}); err != nil {
		return Event{}, false, err
	}
	if fault.Kind == FaultSequenceGap {
		return source.Next()
	}
	if fault.Kind == FaultLatency {
		if fault.Delay < 0 || uint64(fault.Delay) > ^uint64(0)-event.LogicalTime {
			return Event{}, false, replayError("fault_latency_invalid")
		}
		event.LogicalTime += uint64(fault.Delay)
	}
	if fault.Kind == FaultDisconnect || fault.Kind == FaultStorageFailure || fault.Kind == FaultRestart {
		source.pending = &event
		return Event{}, false, replayError(fmt.Sprintf("fault_%s", fault.Kind))
	}
	return cloneEvent(event), true, nil
}

// SeekOrdinal resets pending delivery and delegates indexed seeking.
func (source *FaultSource) SeekOrdinal(ordinal uint64) error {
	source.pending = nil
	return source.source.SeekOrdinal(ordinal)
}

func validFault(fault Fault) bool {
	switch fault.Kind {
	case FaultDisconnect, FaultSequenceGap, FaultRejection, FaultPartialFill,
		FaultCancelFillRace, FaultUnknownState, FaultStorageFailure, FaultRestart:
		return fault.Delay == 0
	case FaultLatency:
		return fault.Delay > 0
	default:
		return false
	}
}

func cloneEvent(event Event) Event {
	event.Canonical = append([]byte(nil), event.Canonical...)
	return event
}
