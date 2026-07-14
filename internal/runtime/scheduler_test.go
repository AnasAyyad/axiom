package runtimecore

import (
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestSchedulerUsesEveryTupleFieldDeterministically(t *testing.T) {
	clock, _ := NewDeterministicClock(1)
	scheduler, _ := NewDeterministicScheduler(clock, 10)
	scheduler.Resume()
	baseTime := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	keys := []SchedulerKey{
		schedulerKey(t, "event-e", 2, baseTime, 1, 1),
		schedulerKey(t, "event-d", 1, baseTime.Add(time.Nanosecond), 1, 1),
		schedulerKey(t, "event-c", 1, baseTime, 2, 1),
		schedulerKey(t, "event-b", 1, baseTime, 1, 2),
		schedulerKey(t, "event-a", 1, baseTime, 1, 1),
	}
	var mutex sync.Mutex
	var order []string
	for _, key := range keys {
		key := key
		if err := scheduler.Schedule(ScheduledWork{Key: key, Action: func() ([]ScheduledWork, error) {
			mutex.Lock()
			defer mutex.Unlock()
			order = append(order, key.StableID.Value())
			return nil, nil
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if executed, err := scheduler.Advance(2); err != nil || executed != 5 {
		t.Fatalf("advance = %d, %v", executed, err)
	}
	want := []string{"event-a", "event-b", "event-c", "event-d", "event-e"}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestSchedulerMissingSentinelsCausalWorkPauseAndCancel(t *testing.T) {
	clock, _ := NewDeterministicClock(1)
	scheduler, _ := NewDeterministicScheduler(clock, 4)
	missingID, _ := domain.NewEventID("missing")
	presentID, _ := domain.NewEventID("present")
	cancelID, _ := domain.NewEventID("cancel")
	var order []string
	missing := ScheduledWork{Key: SchedulerKey{ScheduledTime: 5, StableID: missingID}, Action: appendAction(&order, "missing")}
	present := ScheduledWork{Key: SchedulerKey{
		ScheduledTime: 5, ExchangeTime: OptionalTime{Present: true, Value: time.Unix(0, 1).UTC()}, StableID: presentID,
	}, Action: func() ([]ScheduledWork, error) {
		order = append(order, "present")
		derivedID, _ := domain.NewEventID("derived")
		return []ScheduledWork{{Key: SchedulerKey{ScheduledTime: 5, StableID: derivedID}, Action: appendAction(&order, "derived")}}, nil
	}}
	cancelled := ScheduledWork{Key: SchedulerKey{ScheduledTime: 6, StableID: cancelID}, Action: appendAction(&order, "cancel")}
	for _, work := range []ScheduledWork{present, cancelled, missing} {
		if err := scheduler.Schedule(work); err != nil {
			t.Fatal(err)
		}
	}
	if !scheduler.Cancel(cancelID) {
		t.Fatal("cancel failed")
	}
	if _, err := scheduler.Advance(6); err == nil {
		t.Fatal("paused scheduler advanced")
	}
	if stepped, err := scheduler.Step(); err != nil || !stepped || order[0] != "missing" {
		t.Fatalf("step = %t, %v, %v", stepped, err, order)
	}
	scheduler.Resume()
	if _, err := scheduler.Advance(6); err != nil {
		t.Fatal(err)
	}
	want := []string{"missing", "present", "derived"}
	for index := range want {
		if order[index] != want[index] {
			t.Fatalf("order = %v", order)
		}
	}
}

func TestSchedulerCapacityAndClockRegression(t *testing.T) {
	clock, _ := NewDeterministicClock(10)
	scheduler, _ := NewDeterministicScheduler(clock, 1)
	id, _ := domain.NewEventID("only")
	work := ScheduledWork{Key: SchedulerKey{ScheduledTime: 10, StableID: id}, Action: appendAction(new([]string), "only")}
	if err := scheduler.Schedule(work); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Schedule(work); err == nil {
		t.Fatal("scheduler capacity was exceeded")
	}
	scheduler.Resume()
	if _, err := scheduler.Advance(9); err == nil {
		t.Fatal("logical time regression accepted")
	}
}

func TestSchedulerRejectsDuplicateIdentityAndNonCanonicalSentinels(t *testing.T) {
	clock, _ := NewDeterministicClock(1)
	scheduler, _ := NewDeterministicScheduler(clock, 3)
	id, _ := domain.NewEventID("stable-a")
	valid := ScheduledWork{Key: SchedulerKey{ScheduledTime: 2, StableID: id}, Action: appendAction(new([]string), "valid")}
	if err := scheduler.Schedule(valid); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Schedule(valid); err == nil {
		t.Fatal("duplicate stable identity accepted")
	}
	otherID, _ := domain.NewEventID("stable-b")
	invalid := ScheduledWork{Key: SchedulerKey{
		ScheduledTime: 3, ExchangeTime: OptionalTime{Value: time.Unix(0, 1).UTC()}, StableID: otherID,
	}, Action: appendAction(new([]string), "invalid")}
	if err := scheduler.Schedule(invalid); err == nil {
		t.Fatal("non-canonical absent sentinel accepted")
	}
}

func schedulerKey(t *testing.T, idValue string, scheduled LogicalTime, exchange time.Time, sequence, ordinal uint64) SchedulerKey {
	t.Helper()
	id, err := domain.NewEventID(idValue)
	if err != nil {
		t.Fatal(err)
	}
	return SchedulerKey{
		ScheduledTime: scheduled, ExchangeTime: OptionalTime{Present: true, Value: exchange},
		SourceSequence: OptionalUint64{Present: true, Value: sequence},
		IngestOrdinal:  OptionalUint64{Present: true, Value: ordinal}, StableID: id,
	}
}

func appendAction(order *[]string, value string) WorkAction {
	return func() ([]ScheduledWork, error) {
		*order = append(*order, value)
		return nil, nil
	}
}
