package runtimecore

import (
	"sort"
	"sync"

	"axiom/internal/domain"
)

// SchedulerKey is the exact five-part deterministic derived-work tuple.
type SchedulerKey struct {
	ScheduledTime  LogicalTime
	ExchangeTime   OptionalTime
	SourceSequence OptionalUint64
	IngestOrdinal  OptionalUint64
	StableID       domain.EventID
}

// WorkAction is one deterministic reduction that may return causal follow-up work.
type WorkAction func() ([]ScheduledWork, error)

// ScheduledWork is one bounded scheduler item.
type ScheduledWork struct {
	Key    SchedulerKey
	Action WorkAction
}

// Scheduler supplies deterministic timers and delayed work.
type Scheduler interface {
	Schedule(ScheduledWork) error
	Cancel(domain.EventID) bool
	Pause()
	Resume()
	Step() (bool, error)
	Advance(LogicalTime) (int, error)
}

// DeterministicScheduler executes a bounded total order independent of goroutines.
type DeterministicScheduler struct {
	mutex    sync.Mutex
	clock    *DeterministicClock
	capacity int
	paused   bool
	work     []ScheduledWork
	seen     map[string]struct{}
}

// NewDeterministicScheduler creates an initially paused bounded scheduler.
func NewDeterministicScheduler(clock *DeterministicClock, capacity int) (*DeterministicScheduler, error) {
	if clock == nil || capacity <= 0 {
		return nil, runtimeError("invalid_scheduler", "configuration")
	}
	return &DeterministicScheduler{clock: clock, capacity: capacity, paused: true, seen: make(map[string]struct{})}, nil
}

// Schedule inserts work according to the complete five-part tuple.
func (scheduler *DeterministicScheduler) Schedule(work ScheduledWork) error {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	return scheduler.scheduleLocked(work)
}

// Cancel removes pending work by stable identity.
func (scheduler *DeterministicScheduler) Cancel(id domain.EventID) bool {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	for index := range scheduler.work {
		if scheduler.work[index].Key.StableID == id {
			scheduler.work = append(scheduler.work[:index], scheduler.work[index+1:]...)
			return true
		}
	}
	return false
}

// Pause prevents Advance from executing work.
func (scheduler *DeterministicScheduler) Pause() {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	scheduler.paused = true
}

// Resume permits time advancement and eligible work execution.
func (scheduler *DeterministicScheduler) Resume() {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	scheduler.paused = false
}

// Step executes exactly one next item while remaining paused.
func (scheduler *DeterministicScheduler) Step() (bool, error) {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	if len(scheduler.work) == 0 {
		return false, nil
	}
	return true, scheduler.executeFirst()
}

// Advance executes all eligible work through a non-regressing logical time.
func (scheduler *DeterministicScheduler) Advance(to LogicalTime) (int, error) {
	scheduler.mutex.Lock()
	defer scheduler.mutex.Unlock()
	if scheduler.paused {
		return 0, runtimeError("scheduler_paused", "advance")
	}
	if to < scheduler.clock.Now() {
		return 0, runtimeError("time_regression", "scheduler")
	}
	executed := 0
	for len(scheduler.work) > 0 && scheduler.work[0].Key.ScheduledTime <= to {
		if err := scheduler.executeFirst(); err != nil {
			return executed, err
		}
		executed++
	}
	return executed, scheduler.clock.Advance(to)
}

func (scheduler *DeterministicScheduler) executeFirst() error {
	work := scheduler.work[0]
	if err := scheduler.clock.Advance(work.Key.ScheduledTime); err != nil {
		return err
	}
	scheduler.work = scheduler.work[1:]
	derived, err := work.Action()
	if err != nil {
		return err
	}
	if len(scheduler.work)+len(derived) > scheduler.capacity {
		return runtimeError("scheduler_saturated", "derived_work")
	}
	derivedIDs := make(map[string]struct{}, len(derived))
	for _, item := range derived {
		if err := scheduler.validateWorkLocked(item); err != nil {
			return err
		}
		identity := item.Key.StableID.String()
		if _, duplicate := derivedIDs[identity]; duplicate {
			return runtimeError("invalid_scheduled_work", "duplicate_id")
		}
		derivedIDs[identity] = struct{}{}
	}
	for identity := range derivedIDs {
		scheduler.seen[identity] = struct{}{}
	}
	scheduler.work = append(scheduler.work, derived...)
	scheduler.sortLocked()
	return nil
}

func (scheduler *DeterministicScheduler) scheduleLocked(work ScheduledWork) error {
	if len(scheduler.work) >= scheduler.capacity {
		return runtimeError("scheduler_saturated", "work")
	}
	if err := scheduler.validateWorkLocked(work); err != nil {
		return err
	}
	scheduler.seen[work.Key.StableID.String()] = struct{}{}
	scheduler.work = append(scheduler.work, work)
	scheduler.sortLocked()
	return nil
}

func (scheduler *DeterministicScheduler) validateWorkLocked(work ScheduledWork) error {
	if work.Key.ScheduledTime == 0 || work.Key.StableID.Value() == "" || work.Action == nil {
		return runtimeError("invalid_scheduled_work", "fields")
	}
	if !validOptionalTime(work.Key.ExchangeTime) || !validOptionalUint64(work.Key.SourceSequence) ||
		!validOptionalUint64(work.Key.IngestOrdinal) {
		return runtimeError("invalid_scheduled_work", "sentinel")
	}
	if _, duplicate := scheduler.seen[work.Key.StableID.String()]; duplicate {
		return runtimeError("invalid_scheduled_work", "duplicate_id")
	}
	return nil
}

func (scheduler *DeterministicScheduler) sortLocked() {
	sort.SliceStable(scheduler.work, func(left, right int) bool {
		return schedulerKeyLess(scheduler.work[left].Key, scheduler.work[right].Key)
	})
}

func schedulerKeyLess(left, right SchedulerKey) bool {
	if left.ScheduledTime != right.ScheduledTime {
		return left.ScheduledTime < right.ScheduledTime
	}
	if comparison := compareOptionalTime(left.ExchangeTime, right.ExchangeTime); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareOptionalUint64(left.SourceSequence, right.SourceSequence); comparison != 0 {
		return comparison < 0
	}
	if comparison := compareOptionalUint64(left.IngestOrdinal, right.IngestOrdinal); comparison != 0 {
		return comparison < 0
	}
	return left.StableID.String() < right.StableID.String()
}

func compareOptionalTime(left, right OptionalTime) int {
	if left.Present != right.Present {
		if !left.Present {
			return -1
		}
		return 1
	}
	if !left.Present || left.Value.Equal(right.Value) {
		return 0
	}
	if left.Value.Before(right.Value) {
		return -1
	}
	return 1
}

func compareOptionalUint64(left, right OptionalUint64) int {
	if left.Present != right.Present {
		if !left.Present {
			return -1
		}
		return 1
	}
	if !left.Present || left.Value == right.Value {
		return 0
	}
	if left.Value < right.Value {
		return -1
	}
	return 1
}

var _ Clock = (*RealClock)(nil)
var _ Clock = (*DeterministicClock)(nil)
var _ Scheduler = (*DeterministicScheduler)(nil)
