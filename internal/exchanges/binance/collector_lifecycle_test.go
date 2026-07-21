package binance

import (
	"context"
	"errors"
	"testing"
	"time"

	"axiom/internal/domain"
)

type deterministicCollectorLifecycle struct {
	now   time.Time
	waits []time.Duration
}

func newDeterministicCollectorLifecycle() *deterministicCollectorLifecycle {
	return &deterministicCollectorLifecycle{now: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)}
}

func (clock *deterministicCollectorLifecycle) Now() time.Time { return clock.now }

func (clock *deterministicCollectorLifecycle) Wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	clock.waits = append(clock.waits, delay)
	clock.now = clock.now.Add(delay)
	return nil
}

func (clock *deterministicCollectorLifecycle) Advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}

func lifecycleTestCollector(clock collectorLifecycle, policy ConnectionPolicy) *InstrumentCollector {
	return &InstrumentCollector{config: CollectorConfig{ConnectionPolicy: policy},
		stats: newCollectorStats(), lifecycle: clock}
}

func lifecycleTestPolicy() ConnectionPolicy {
	return ConnectionPolicy{MinimumBackoff: time.Second, MaximumBackoff: time.Minute,
		Renewal: time.Hour, Seed: "lifecycle-test"}
}

func TestLifecycleHealthyDisconnectsAlwaysRestartAtAttemptOne(t *testing.T) {
	clock := newDeterministicCollectorLifecycle()
	policy := lifecycleTestPolicy()
	collector := lifecycleTestCollector(clock, policy)
	ctx, cancel := context.WithCancel(context.Background())
	index := 0
	err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		index++
		switch index {
		case 1:
			return generationOutcome{reachedHealthy: true, reason: reconnectStream}
		case 2:
			return generationOutcome{reachedHealthy: true, reason: reconnectScheduledRenewal}
		default:
			cancel()
			return generationOutcome{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := policy.Backoff(1)
	if len(clock.waits) != 2 || clock.waits[0] != first || clock.waits[1] != first {
		t.Fatalf("healthy reconnect waits = %v, want [%s %s]", clock.waits, first, first)
	}
	stats := collector.Stats()
	if stats.ReconnectReasons.Stream != 1 || stats.ReconnectReasons.ScheduledRenewal != 1 {
		t.Fatalf("healthy reasons = %#v", stats.ReconnectReasons)
	}
}

func TestLifecyclePreHealthFailuresEscalateAndRecoveryResets(t *testing.T) {
	clock := newDeterministicCollectorLifecycle()
	policy := lifecycleTestPolicy()
	collector := lifecycleTestCollector(clock, policy)
	ctx, cancel := context.WithCancel(context.Background())
	outcomes := []generationOutcome{
		{reason: reconnectSubscription},
		{reason: reconnectSnapshot},
		{reachedHealthy: true, reason: reconnectStream},
		{reachedHealthy: true, reason: reconnectClock},
	}
	index := 0
	err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		if index == len(outcomes) {
			cancel()
			return generationOutcome{}
		}
		outcome := outcomes[index]
		index++
		return outcome
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := policy.Backoff(1)
	second, _ := policy.Backoff(2)
	want := []time.Duration{first, second, first, first}
	if len(clock.waits) != len(want) {
		t.Fatalf("wait count = %d, want %d", len(clock.waits), len(want))
	}
	for index := range want {
		if clock.waits[index] != want[index] {
			t.Fatalf("wait %d = %s, want %s; all=%v", index, clock.waits[index], want[index], clock.waits)
		}
	}
	diagnostics := collector.Stats().ReconnectDiagnostics
	if len(diagnostics) != len(outcomes) {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
	wantAttempts := []uint32{1, 2, 1, 1}
	for index, diagnostic := range diagnostics {
		if diagnostic.Attempt != wantAttempts[index] {
			t.Fatalf("diagnostic %d attempt=%d want=%d", index, diagnostic.Attempt, wantAttempts[index])
		}
	}
}

func TestLifecycleResyncIncludesConsecutiveFailuresAndBackoff(t *testing.T) {
	clock := newDeterministicCollectorLifecycle()
	policy := lifecycleTestPolicy()
	collector := lifecycleTestCollector(clock, policy)
	ctx, cancel := context.WithCancel(context.Background())
	index := 0
	err := collector.runLifecycle(ctx, func(_ context.Context, resyncStarted time.Time) generationOutcome {
		index++
		switch index {
		case 1:
			return generationOutcome{reachedHealthy: true, reason: reconnectStream}
		case 2:
			clock.Advance(3 * time.Second)
			return generationOutcome{reason: reconnectSubscription}
		case 3:
			collector.recordResynchronization(resyncStarted, 3)
			return generationOutcome{reachedHealthy: true, reason: reconnectScheduledRenewal}
		default:
			cancel()
			return generationOutcome{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := policy.Backoff(1)
	second, _ := policy.Backoff(2)
	want := first + 3*time.Second + second
	stats := collector.Stats()
	if stats.ResyncSamples != 1 || stats.ResyncMax != want {
		diagnostics := collector.Stats().ReconnectDiagnostics
		for _, diagnostic := range diagnostics {
			if diagnostic.Phase == "health_lost" && diagnostic.ResyncElapsed != 0 {
				t.Fatalf("new health-loss cycle retained old resync elapsed: %#v", diagnostic)
			}
		}
		t.Fatalf("resync = samples %d max %s, want 1 and %s", stats.ResyncSamples, stats.ResyncMax, want)
	}
}

func TestReconnectReasonCountersAreCompleteAndBounded(t *testing.T) {
	clock := newDeterministicCollectorLifecycle()
	policy := ConnectionPolicy{MinimumBackoff: time.Nanosecond, MaximumBackoff: time.Nanosecond,
		Renewal: time.Hour, Seed: "reason-test"}
	collector := lifecycleTestCollector(clock, policy)
	reasons := []reconnectReason{reconnectSubscription, reconnectStream, reconnectSnapshot,
		reconnectSnapshotBridge, reconnectClock, reconnectStaleBook, reconnectQueue, reconnectBuffer,
		reconnectInvalidEvent, reconnectSequenceGap, reconnectScheduledRenewal}
	ctx, cancel := context.WithCancel(context.Background())
	index := 0
	if err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		if index == len(reasons) {
			cancel()
			return generationOutcome{}
		}
		reason := reasons[index]
		index++
		return generationOutcome{reason: reason}
	}); err != nil {
		t.Fatal(err)
	}
	counts := collector.Stats().ReconnectReasons
	if counts.Subscription != 1 || counts.Stream != 1 || counts.Snapshot != 1 ||
		counts.SnapshotBridge != 1 || counts.Clock != 1 || counts.StaleBook != 1 ||
		counts.Queue != 1 || counts.Buffer != 1 || counts.InvalidEvent != 1 ||
		counts.SequenceGap != 1 || counts.ScheduledRenewal != 1 {
		t.Fatalf("reason counts = %#v", counts)
	}
}

func TestResyncHistogramFifteenSecondBoundaryAndExactMaximum(t *testing.T) {
	stats := newCollectorStats()
	stats.resync.record(15 * time.Second)
	stats.resync.record(15*time.Second + time.Nanosecond)
	snapshot := stats.Snapshot()
	if snapshot.ResyncSamples != 2 || snapshot.ResyncOver15Seconds != 1 ||
		snapshot.ResyncP95 != 20*time.Second || snapshot.ResyncMax != 15*time.Second+time.Nanosecond {
		t.Fatalf("boundary snapshot = %#v", snapshot)
	}
}

func TestLifecycleCancellationAndRecorderFailureFailClosed(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		clock := newDeterministicCollectorLifecycle()
		collector := lifecycleTestCollector(clock, lifecycleTestPolicy())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		called := false
		if err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
			called = true
			return generationOutcome{}
		}); err != nil || called {
			t.Fatalf("canceled lifecycle err=%v called=%v", err, called)
		}
	})
	t.Run("recorder", func(t *testing.T) {
		instrument := approvedBTC(t)
		clock := &domain.SystemClock{}
		source := newCollectorSource(t, instrument, clock, 101)
		failure := errors.New("recorder unavailable")
		collector, err := NewInstrumentCollector(testCollectorConfig(instrument), source,
			failingCollectorRecorder{failure: failure}, clock)
		if err != nil {
			t.Fatal(err)
		}
		if err = collector.Run(context.Background()); !errors.Is(err, failure) {
			t.Fatalf("recorder failure = %v", err)
		}
		if collector.Stats().Reconnects != 0 {
			t.Fatal("fatal recorder failure was retried")
		}
	})
}

type failingCollectorRecorder struct{ failure error }

func (recorder failingCollectorRecorder) RecordPublicRaw(context.Context, PublicRawRecord) (StreamRecordToken, error) {
	return StreamRecordToken{}, recorder.failure
}
func (recorder failingCollectorRecorder) RecordPublicCanonical(context.Context, PublicCanonicalRecord) error {
	return recorder.failure
}
func (recorder failingCollectorRecorder) RecordSourceGap(context.Context, SourceGap) error {
	return recorder.failure
}

func TestInstrumentCollectorCountsScheduledRenewal(t *testing.T) {
	instrument := approvedBTC(t)
	clock := &domain.SystemClock{}
	recorder := &collectorRecorder{}
	source := newCollectorSource(t, instrument, clock, 101, 101)
	config := testCollectorConfig(instrument)
	config.ConnectionPolicy.Renewal = 5 * time.Millisecond
	collector, err := NewInstrumentCollector(config, source, recorder, clock)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- collector.Run(ctx) }()
	waitFor(t, func() bool {
		stats := collector.Stats()
		return stats.ReconnectReasons.ScheduledRenewal > 0 && stats.ResyncSamples > 0
	})
	cancel()
	if err = <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleHighCycleReconnectStress(t *testing.T) {
	const cycles = 10_000
	clock := newDeterministicCollectorLifecycle()
	policy := lifecycleTestPolicy()
	collector := lifecycleTestCollector(clock, policy)
	ctx, cancel := context.WithCancel(context.Background())
	count := 0
	if err := collector.runLifecycle(ctx, func(context.Context, time.Time) generationOutcome {
		if count == cycles {
			cancel()
			return generationOutcome{}
		}
		count++
		return generationOutcome{reachedHealthy: true, reason: reconnectStream}
	}); err != nil {
		t.Fatal(err)
	}
	first, _ := policy.Backoff(1)
	if len(clock.waits) != cycles || collector.Stats().Reconnects != cycles {
		t.Fatalf("cycles waits=%d reconnects=%d", len(clock.waits), collector.Stats().Reconnects)
	}
	for index, delay := range clock.waits {
		if delay != first {
			t.Fatalf("cycle %d delay=%s want=%s", index, delay, first)
		}
	}
}

func TestReconnectAttributionUsesBoundedObjectiveEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		diagnostic ReconnectDiagnostic
		want       string
	}{
		{diagnostic: ReconnectDiagnostic{Phase: "health_restored"}, want: "recovered"},
		{diagnostic: ReconnectDiagnostic{Reason: reconnectScheduledRenewal.String()}, want: "scheduled"},
		{diagnostic: ReconnectDiagnostic{Reason: reconnectSequenceGap.String()}, want: "internal"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "rate_limit", HTTPStatus: 429}, want: "upstream"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "rate_limit"}, want: "internal"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "maintenance"}, want: "upstream"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "transient_outage", HTTPStatus: 503}, want: "upstream"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "transient_outage", Cause: "dns_failure"}, want: "network"},
		{diagnostic: ReconnectDiagnostic{FailureKind: "transient_outage", Cause: "transport_failure"},
			want: "external_unclassified"},
		{diagnostic: ReconnectDiagnostic{Cause: "recorder"}, want: "internal"},
		{diagnostic: ReconnectDiagnostic{}, want: "unclassified"},
	}
	for _, test := range tests {
		if got := reconnectAttribution(test.diagnostic); got != test.want {
			t.Fatalf("diagnostic=%#v attribution=%q want=%q", test.diagnostic, got, test.want)
		}
	}
}

func TestReconnectDiagnosticRingIsBoundedAndReportsDroppedEvents(t *testing.T) {
	stats := newCollectorStats()
	for sequence := 0; sequence < maximumReconnectDiagnostics+17; sequence++ {
		stats.recordDiagnostic(ReconnectDiagnostic{Cycle: uint64(sequence)})
	}
	snapshot := stats.Snapshot()
	if len(snapshot.ReconnectDiagnostics) != maximumReconnectDiagnostics || snapshot.DiagnosticsDropped != 17 {
		t.Fatalf("diagnostics=%d dropped=%d", len(snapshot.ReconnectDiagnostics), snapshot.DiagnosticsDropped)
	}
	if snapshot.ReconnectDiagnostics[0].Cycle != 17 ||
		snapshot.ReconnectDiagnostics[len(snapshot.ReconnectDiagnostics)-1].Cycle != maximumReconnectDiagnostics+16 {
		t.Fatalf("diagnostic boundaries=%d..%d", snapshot.ReconnectDiagnostics[0].Cycle,
			snapshot.ReconnectDiagnostics[len(snapshot.ReconnectDiagnostics)-1].Cycle)
	}
}

func TestSuccessfulOperationDiagnosticRetainsPhaseTimingAndClockSnapshot(t *testing.T) {
	clock := newDeterministicCollectorLifecycle()
	collector := lifecycleTestCollector(clock, lifecycleTestPolicy())
	collector.lifecycleCycle.Store(4)
	collector.lifecycleAttempt.Store(2)
	collector.recordOperationDiagnostic("clock", 9, 17*time.Millisecond, -3*time.Millisecond,
		8*time.Millisecond, 12345, 27)
	snapshot := collector.Stats()
	if len(snapshot.ReconnectDiagnostics) != 1 {
		t.Fatalf("diagnostics=%#v", snapshot.ReconnectDiagnostics)
	}
	diagnostic := snapshot.ReconnectDiagnostics[0]
	if diagnostic.Phase != "operation_succeeded" || diagnostic.Stage != "clock" ||
		diagnostic.Attribution != "observed" || diagnostic.Cycle != 4 || diagnostic.Attempt != 2 ||
		diagnostic.Generation != 9 || diagnostic.AttemptDuration != 17*time.Millisecond ||
		diagnostic.ClockOffset != -3*time.Millisecond || diagnostic.ClockUncertainty != 8*time.Millisecond ||
		diagnostic.SnapshotSequence != 12345 || diagnostic.BufferedDepth != 27 {
		t.Fatalf("diagnostic=%#v", diagnostic)
	}
}
