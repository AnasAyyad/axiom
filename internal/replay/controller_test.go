package replay

import (
	"context"
	"testing"
	"time"
)

func TestControllerTimingPauseStepSeekAndRestart(t *testing.T) {
	source := &memorySource{events: fixtureEvents()}
	pacer := &recordingPacer{}
	controller, err := NewController(source, pacer, OriginalTiming, 1)
	if err != nil {
		t.Fatal(err)
	}
	if codeOfNext(controller.Next(context.Background())) != "paused" {
		t.Fatal("paused controller delivered an event")
	}
	controller.Step()
	first, ok, err := controller.Next(context.Background())
	if err != nil || !ok || first.Ordinal != 1 || len(pacer.delays) != 1 || pacer.delays[0] != 0 {
		t.Fatalf("first = %#v, %v, delays=%v", first, err, pacer.delays)
	}
	if codeOfNext(controller.Next(context.Background())) != "paused" {
		t.Fatal("step delivered more than one event")
	}
	controller.Resume()
	second, ok, err := controller.Next(context.Background())
	if err != nil || !ok || second.Ordinal != 2 || pacer.delays[1] != 200*time.Nanosecond {
		t.Fatalf("second = %#v, %v, delays=%v", second, err, pacer.delays)
	}
	if err = controller.Restart(2); err != nil {
		t.Fatal(err)
	}
	controller.Step()
	replayed, ok, err := controller.Next(context.Background())
	if err != nil || !ok || replayed.Ordinal != 2 || string(replayed.Canonical) != "two" {
		t.Fatalf("replayed = %#v, %v", replayed, err)
	}
}

func TestControllerAccelerationAndIncidentWindow(t *testing.T) {
	pacer := &recordingPacer{}
	controller, err := NewController(&memorySource{events: fixtureEvents()}, pacer, AcceleratedTiming, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err = controller.SelectWindow(2, 2); err != nil {
		t.Fatal(err)
	}
	controller.Resume()
	event, ok, err := controller.Next(context.Background())
	if err != nil || !ok || event.Ordinal != 2 || pacer.delays[0] != 0 {
		t.Fatalf("window event = %#v, %v, delays=%v", event, err, pacer.delays)
	}
	if codeOfNext(controller.Next(context.Background())) != "window_complete" {
		t.Fatal("event outside incident window was accepted")
	}
}

func TestControllerMaximumModeNeverWaits(t *testing.T) {
	pacer := &recordingPacer{}
	controller, err := NewController(&memorySource{events: fixtureEvents()}, pacer, MaximumTiming, 1)
	if err != nil {
		t.Fatal(err)
	}
	controller.Resume()
	for range fixtureEvents() {
		if _, _, err = controller.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	for _, delay := range pacer.delays {
		if delay != 0 {
			t.Fatalf("maximum replay delay = %s", delay)
		}
	}
}

type memorySource struct {
	events []Event
	index  int
}

func (source *memorySource) Next() (Event, bool, error) {
	if source.index >= len(source.events) {
		return Event{}, false, nil
	}
	event := source.events[source.index]
	source.index++
	return event, true, nil
}

func (source *memorySource) SeekOrdinal(ordinal uint64) error {
	for index, event := range source.events {
		if event.Ordinal == ordinal {
			source.index = index
			return nil
		}
	}
	return replayError("not_found")
}

type recordingPacer struct{ delays []time.Duration }

func (pacer *recordingPacer) Wait(_ context.Context, delay time.Duration) error {
	pacer.delays = append(pacer.delays, delay)
	return nil
}

func fixtureEvents() []Event {
	return []Event{{LogicalTime: 100, Ordinal: 1, Canonical: []byte("one")},
		{LogicalTime: 300, Ordinal: 2, Canonical: []byte("two")},
		{LogicalTime: 600, Ordinal: 3, Canonical: []byte("three")}}
}

func codeOfNext(_ Event, _ bool, err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}
