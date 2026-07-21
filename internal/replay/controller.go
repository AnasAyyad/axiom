package replay

import (
	"context"
	"sync"
	"time"
)

// TimingMode controls wall pacing without changing logical event order.
type TimingMode string

// Supported replay timing modes.
const (
	OriginalTiming    TimingMode = "original"
	AcceleratedTiming TimingMode = "accelerated"
	MaximumTiming     TimingMode = "maximum"
)

// Event is the minimal immutable canonical replay input.
type Event struct {
	LogicalTime uint64
	Ordinal     uint64
	Canonical   []byte
}

// Source supplies verified canonical events and manifest-indexed seeking.
type Source interface {
	Next() (Event, bool, error)
	SeekOrdinal(uint64) error
}

// Pacer waits in wall time while remaining cancelable.
type Pacer interface {
	Wait(context.Context, time.Duration) error
}

// RealPacer is the production wall-time replay pacer.
type RealPacer struct{}

// Wait sleeps for a bounded duration or returns cancellation.
func (RealPacer) Wait(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Controller serializes replay commands and one-event delivery.
type Controller struct {
	mutex        sync.Mutex
	source       Source
	pacer        Pacer
	mode         TimingMode
	acceleration uint64
	paused       bool
	step         bool
	windowStart  uint64
	windowEnd    uint64
	priorTime    uint64
	priorOrdinal uint64
}

// NewController creates an initially paused replay controller.
func NewController(source Source, pacer Pacer, mode TimingMode, acceleration uint64) (*Controller, error) {
	if source == nil || pacer == nil || !validTiming(mode, acceleration) {
		return nil, replayError("configuration_invalid")
	}
	return &Controller{source: source, pacer: pacer, mode: mode,
		acceleration: acceleration, paused: true}, nil
}

// Pause stops ordinary event delivery without changing the cursor.
func (controller *Controller) Pause() {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.paused = true
}

// Resume permits paced event delivery.
func (controller *Controller) Resume() {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.paused, controller.step = false, false
}

// Step permits exactly one event while the controller remains paused.
func (controller *Controller) Step() {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	controller.paused, controller.step = true, true
}

// Seek positions the next delivery at an indexed ingest ordinal.
func (controller *Controller) Seek(ordinal uint64) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	return controller.seekLocked(ordinal)
}

// SelectWindow constrains delivery to one inclusive incident ordinal window.
func (controller *Controller) SelectWindow(first, last uint64) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if first == 0 || last < first {
		return replayError("window_invalid")
	}
	if err := controller.seekLocked(first); err != nil {
		return err
	}
	controller.windowStart, controller.windowEnd = first, last
	return nil
}

// Restart deterministically resets state at one selected event.
func (controller *Controller) Restart(ordinal uint64) error {
	controller.mutex.Lock()
	defer controller.mutex.Unlock()
	if err := controller.seekLocked(ordinal); err != nil {
		return err
	}
	controller.paused, controller.step = true, false
	return nil
}

// Next returns one event after configured pacing.
func (controller *Controller) Next(ctx context.Context) (Event, bool, error) {
	controller.mutex.Lock()
	if controller.paused && !controller.step {
		controller.mutex.Unlock()
		return Event{}, false, replayError("paused")
	}
	event, ok, err := controller.source.Next()
	if err != nil || !ok {
		controller.mutex.Unlock()
		return Event{}, ok, err
	}
	if err = controller.validateEventLocked(event); err != nil {
		controller.mutex.Unlock()
		return Event{}, false, err
	}
	delay := controller.delayLocked(event.LogicalTime)
	controller.priorTime, controller.priorOrdinal = event.LogicalTime, event.Ordinal
	if controller.step {
		controller.step = false
	}
	controller.mutex.Unlock()
	if err = controller.pacer.Wait(ctx, delay); err != nil {
		return Event{}, false, err
	}
	event.Canonical = append([]byte(nil), event.Canonical...)
	return event, true, nil
}

func (controller *Controller) seekLocked(ordinal uint64) error {
	if ordinal == 0 || controller.source.SeekOrdinal(ordinal) != nil {
		return replayError("seek_invalid")
	}
	controller.priorTime, controller.priorOrdinal = 0, ordinal-1
	return nil
}

func (controller *Controller) validateEventLocked(event Event) error {
	if event.LogicalTime == 0 || event.Ordinal == 0 || len(event.Canonical) == 0 ||
		(controller.priorOrdinal > 0 && event.Ordinal <= controller.priorOrdinal) ||
		(controller.priorTime > 0 && event.LogicalTime < controller.priorTime) {
		return replayError("event_invalid")
	}
	if controller.windowEnd > 0 && (event.Ordinal < controller.windowStart || event.Ordinal > controller.windowEnd) {
		return replayError("window_complete")
	}
	return nil
}

func (controller *Controller) delayLocked(logicalTime uint64) time.Duration {
	if controller.mode == MaximumTiming || controller.priorTime == 0 {
		return 0
	}
	delta := logicalTime - controller.priorTime
	if controller.mode == AcceleratedTiming {
		delta /= controller.acceleration
	}
	if delta > uint64(time.Duration(1<<63-1)) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(delta)
}

func validTiming(mode TimingMode, acceleration uint64) bool {
	if mode == OriginalTiming || mode == MaximumTiming {
		return acceleration == 1
	}
	return mode == AcceleratedTiming && acceleration > 1
}
