package bybit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	marketrecorder "axiom/internal/recorder"
)

func TestRecorderFailurePreservesBoundedCauseThroughAdapterWrapper(t *testing.T) {
	want := &marketrecorder.Error{Code: "recorder_capacity_exceeded"}
	wrapped := recorderFailure{want}
	var got *marketrecorder.Error
	if !errors.As(wrapped, &got) || got.Code != want.Code || !isRecorderFailure(wrapped) {
		t.Fatalf("wrapped recorder failure=%v detail=%#v", wrapped, got)
	}
}

type deterministicCollectorLifecycle struct {
	now   time.Time
	waits []time.Duration
}

func (lifecycle *deterministicCollectorLifecycle) Now() time.Time {
	return lifecycle.now
}

func (lifecycle *deterministicCollectorLifecycle) Wait(
	ctx context.Context,
	delay time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lifecycle.waits = append(lifecycle.waits, delay)
	lifecycle.now = lifecycle.now.Add(delay)
	return nil
}

func (lifecycle *deterministicCollectorLifecycle) advance(delay time.Duration) {
	lifecycle.now = lifecycle.now.Add(delay)
}

func newLifecycleTestCollector(t *testing.T) (*InstrumentCollector, *deterministicCollectorLifecycle) {
	t.Helper()
	clock := &domain.SystemClock{}
	instrument := approvedInstruments()[0]
	source := &bybitCollectorSource{clock: clock, generations: [][]exchangecontracts.StreamEvent{
		{bybitSnapshotEvent(t, clock, instrument, 10, "100", "101")},
	}}
	config := DefaultCollectorConfig(instrument)
	config.MinimumBackoff, config.MaximumBackoff = time.Second, time.Minute
	collector, err := NewInstrumentCollector(config, source, &bybitCollectorRecorder{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := &deterministicCollectorLifecycle{now: time.Unix(1_700_000_000, 0).UTC()}
	collector.lifecycle = lifecycle
	return collector, lifecycle
}

func TestBybitLifecycleHealthyDisconnectsRestartAtAttemptOne(t *testing.T) {
	collector, lifecycle := newLifecycleTestCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	var attempts []uint64
	calls := 0
	err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		attempts = append(attempts, collector.lifecycleAttempt.Load())
		calls++
		if calls == 3 {
			cancel()
			return generationOutcome{}
		}
		return generationOutcome{reachedHealthy: true, reason: reconnectStream,
			lostHealthAt: lifecycle.Now(), generation: uint64(calls), stage: "stream", cause: "fixture_disconnect"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attempts, []uint64{1, 1, 1}) ||
		!reflect.DeepEqual(lifecycle.waits, []time.Duration{time.Second, time.Second}) {
		t.Fatalf("attempts=%v waits=%v", attempts, lifecycle.waits)
	}
	stats := collector.Stats()
	if stats.Reconnects != 2 || stats.ReconnectReasons.Stream != 2 ||
		stats.FailureCauses["fixture_disconnect"] != 2 {
		t.Fatalf("stats=%#v", stats)
	}
}

func TestBybitLifecycleConsecutivePreHealthFailuresEscalateThenReset(t *testing.T) {
	collector, lifecycle := newLifecycleTestCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	var attempts []uint64
	calls := 0
	err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		attempts = append(attempts, collector.lifecycleAttempt.Load())
		calls++
		switch calls {
		case 1:
			return generationOutcome{reason: reconnectSubscription, stage: "subscription", cause: "fixture_failure"}
		case 2:
			return generationOutcome{reason: reconnectClock, stage: "clock", cause: "fixture_failure"}
		case 3:
			return generationOutcome{reachedHealthy: true, reason: reconnectStream,
				lostHealthAt: lifecycle.Now(), generation: 3, stage: "stream", cause: "fixture_disconnect"}
		default:
			cancel()
			return generationOutcome{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(attempts, []uint64{1, 2, 3, 1}) ||
		!reflect.DeepEqual(lifecycle.waits, []time.Duration{time.Second, 2 * time.Second, time.Second}) {
		t.Fatalf("attempts=%v waits=%v", attempts, lifecycle.waits)
	}
}

func TestBybitLifecycleResyncIncludesFailuresAndBackoff(t *testing.T) {
	collector, lifecycle := newLifecycleTestCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := collector.runLifecycle(ctx, func(_ context.Context, resyncStarted time.Time) generationOutcome {
		calls++
		switch calls {
		case 1:
			return generationOutcome{reachedHealthy: true, reason: reconnectStream,
				lostHealthAt: lifecycle.Now(), generation: 1, stage: "stream", cause: "fixture_disconnect"}
		case 2:
			lifecycle.advance(3 * time.Second)
			return generationOutcome{reason: reconnectSubscription, generation: 2,
				stage: "subscription", cause: "fixture_failure"}
		default:
			collector.recordResynchronization(resyncStarted, 3)
			cancel()
			return generationOutcome{reachedHealthy: true, generation: 3}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	stats := collector.Stats()
	if stats.ResyncSamples != 1 || stats.ResyncMax != 6*time.Second ||
		stats.ResyncP95 != 10*time.Second {
		t.Fatalf("resync stats=%#v waits=%v", stats, lifecycle.waits)
	}
}

func TestBybitLifecycleHighCycleReconnectStress(t *testing.T) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(previousLogger)
	collector, _ := newLifecycleTestCollector(t)
	ctx, cancel := context.WithCancel(context.Background())
	const cycles = 2000
	calls := 0
	err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		calls++
		if calls > cycles {
			cancel()
			return generationOutcome{}
		}
		if collector.lifecycleAttempt.Load() != 1 {
			t.Fatalf("cycle %d attempt=%d", calls, collector.lifecycleAttempt.Load())
		}
		return generationOutcome{reachedHealthy: true, reason: reconnectScheduledRenewal,
			generation: uint64(calls), stage: "scheduled_renewal", cause: "scheduled_renewal"}
	})
	if err != nil || calls != cycles+1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	stats := collector.Stats()
	if stats.Reconnects != cycles || stats.DiagnosticsDropped != 0 ||
		stats.ReconnectReasons.ScheduledRenewal != cycles {
		t.Fatalf("stress stats=%#v", stats)
	}
}

func TestBybitReconnectReasonCountersCoverEveryBoundedReason(t *testing.T) {
	counters := newCollectorCounters()
	reasons := []reconnectReason{
		reconnectSubscription,
		reconnectStream,
		reconnectSnapshot,
		reconnectClock,
		reconnectHeartbeat,
		reconnectStaleBook,
		reconnectQueue,
		reconnectInvalidEvent,
		reconnectSequenceGap,
		reconnectScheduledRenewal,
	}
	for _, reason := range reasons {
		counters.recordReconnectReason(reason)
	}
	want := ReconnectReasonCounts{
		Subscription:     1,
		Stream:           1,
		Snapshot:         1,
		Clock:            1,
		Heartbeat:        1,
		StaleBook:        1,
		Queue:            1,
		InvalidEvent:     1,
		SequenceGap:      1,
		ScheduledRenewal: 1,
	}
	if got := counters.snapshot().ReconnectReasons; got != want {
		t.Fatalf("reasons=%#v want=%#v", got, want)
	}
}

func TestBybitResynchronizationThresholdIsStrictlyAbove15Seconds(t *testing.T) {
	counters := newCollectorCounters()
	counters.resync.record(15 * time.Second)
	counters.resync.record(15*time.Second + time.Nanosecond)
	stats := counters.snapshot()
	if stats.ResyncSamples != 2 || stats.ResyncOver15Seconds != 1 ||
		stats.ResyncP95 != 20*time.Second || stats.ResyncMax != 15*time.Second+time.Nanosecond {
		t.Fatalf("stats=%#v", stats)
	}
}

func TestBybitInitialHealthIsDiagnosedWithoutCreatingResyncSample(t *testing.T) {
	collector, _ := newLifecycleTestCollector(t)
	collector.recordResynchronization(time.Time{}, 7)
	stats := collector.Stats()
	if stats.ResyncSamples != 0 || len(stats.ReconnectDiagnostics) != 1 {
		t.Fatalf("stats=%#v", stats)
	}
	diagnostic := stats.ReconnectDiagnostics[0]
	if diagnostic.Phase != "health_restored" || diagnostic.Stage != "healthy" ||
		diagnostic.Generation != 7 || !diagnostic.ReachedHealthy ||
		diagnostic.Attribution != "recovered" || diagnostic.ResyncElapsed != 0 {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
}
