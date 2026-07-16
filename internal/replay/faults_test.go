package replay

import (
	"testing"
	"time"
)

func TestFaultSourceInjectsEveryFaultDeterministically(t *testing.T) {
	faults := []Fault{
		{Kind: FaultSequenceGap, Ordinal: 1},
		{Kind: FaultLatency, Ordinal: 2, Delay: 7 * time.Nanosecond},
		{Kind: FaultPartialFill, Ordinal: 3},
		{Kind: FaultCancelFillRace, Ordinal: 4},
		{Kind: FaultUnknownState, Ordinal: 5},
		{Kind: FaultRejection, Ordinal: 6},
	}
	observed := make([]FaultEvent, 0, len(faults))
	source, err := NewFaultSource(&memorySource{events: faultEvents(6)}, faults, func(event FaultEvent) error {
		observed = append(observed, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 2 || event.LogicalTime != 27 {
		t.Fatalf("unexpected first event: %#v %v %v", event, ok, err)
	}
	for expected := uint64(3); expected <= 6; expected++ {
		event, ok, err = source.Next()
		if err != nil || !ok || event.Ordinal != expected {
			t.Fatalf("ordinal %d not delivered", expected)
		}
	}
	if len(observed) != len(faults) {
		t.Fatalf("fault observations = %d", len(observed))
	}
}

func TestFaultSourceRetriesDurableBoundaryEvent(t *testing.T) {
	kinds := []FaultKind{FaultDisconnect, FaultStorageFailure, FaultRestart}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			source, err := NewFaultSource(&memorySource{events: faultEvents(1)},
				[]Fault{{Kind: kind, Ordinal: 1}}, func(FaultEvent) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = source.Next(); err == nil {
				t.Fatal("durable-boundary fault did not interrupt")
			}
			event, ok, err := source.Next()
			if err != nil || !ok || event.Ordinal != 1 {
				t.Fatal("selected event was lost across retry")
			}
		})
	}
}

func faultEvents(count uint64) []Event {
	events := make([]Event, count)
	for index := uint64(0); index < count; index++ {
		events[index] = Event{LogicalTime: (index + 1) * 10, Ordinal: index + 1, Canonical: []byte{byte(index + 1)}}
	}
	return events
}
