package runtimecore

import (
	"context"
	"runtime"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestPipelinePreservesExactOrderAndCausation(t *testing.T) {
	var stages []Stage
	handlers := make([]StageHandler, int(StageAPIStream))
	for index := range handlers {
		handlers[index] = func(_ context.Context, causation Causation) (Causation, error) {
			stages = append(stages, causation.Stage)
			return causation, nil
		}
	}
	pipeline, err := NewPipeline(handlers)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := domain.NewEventID("event-a")
	causation := Causation{
		EventID: id, ConfigurationHash: PayloadDigest([]byte("configuration")),
		ViewVectorHash: PayloadDigest([]byte("views")),
	}
	result, err := pipeline.Process(context.Background(), causation)
	if err != nil || result.Stage != StageAPIStream || len(stages) != int(StageAPIStream) {
		t.Fatalf("pipeline result = %#v, stages=%v, %v", result, stages, err)
	}
	for index, stage := range stages {
		if stage != Stage(index+1) {
			t.Fatalf("stage order = %v", stages)
		}
	}
}

func TestPipelineRejectsCausationMutation(t *testing.T) {
	handlers := make([]StageHandler, int(StageAPIStream))
	for index := range handlers {
		handlers[index] = func(_ context.Context, causation Causation) (Causation, error) { return causation, nil }
	}
	handlers[3] = func(_ context.Context, causation Causation) (Causation, error) {
		causation.ConfigurationHash = PayloadDigest([]byte("changed"))
		return causation, nil
	}
	pipeline, _ := NewPipeline(handlers)
	id, _ := domain.NewEventID("event-a")
	_, err := pipeline.Process(context.Background(), Causation{
		EventID: id, ConfigurationHash: PayloadDigest([]byte("configuration")), ViewVectorHash: PayloadDigest([]byte("views")),
	})
	if err == nil {
		t.Fatal("causation mutation accepted")
	}
}

func TestPersistenceHandoffDoesNotAcknowledgeSaturation(t *testing.T) {
	gate := NewSafetyGate()
	bus, _ := NewEventBus(gate)
	partition := Partition{Kind: PartitionReservation, Key: "reservation-a"}
	_ = bus.Register(partition, ClassCritical, 1, false)
	bus.Seal()
	handoff, _ := NewPersistenceHandoff(bus, partition)
	acknowledged := 0
	if err := handoff.Admit(busEvent(t, "event-a", ClassCritical, 1, ""), func() { acknowledged++ }); err != nil {
		t.Fatal(err)
	}
	if err := handoff.Admit(busEvent(t, "event-b", ClassCritical, 1, ""), func() { acknowledged++ }); err == nil {
		t.Fatal("saturated handoff succeeded")
	}
	if acknowledged != 1 {
		t.Fatalf("acknowledgements = %d", acknowledged)
	}
}

func TestSequenceTrackerInvalidatesWithoutReordering(t *testing.T) {
	tracker, _ := NewSequenceTracker(2)
	results := []SequenceResult{
		tracker.Observe(2, 10), tracker.Observe(2, 10), tracker.Observe(2, 11), tracker.Observe(2, 13), tracker.Observe(2, 12),
	}
	want := []SequenceResult{SequenceAccepted, SequenceDuplicate, SequenceAccepted, SequenceGap, SequenceInvalid}
	for index := range want {
		if results[index] != want[index] {
			t.Fatalf("sequence results = %v", results)
		}
	}
}

func TestLifecycleShutdownIsBoundedAndLeaksNoOwnedWorkers(t *testing.T) {
	before := runtime.NumGoroutine()
	gate := NewSafetyGate()
	metrics := NewRuntimeMetrics()
	lifecycle, _ := NewLifecycle(gate, metrics, time.Second, 4)
	for range 4 {
		if err := lifecycle.Go(func(ctx context.Context) { <-ctx.Done() }); err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.Go(func(context.Context) {}); err == nil {
		t.Fatal("worker bound exceeded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := lifecycle.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if gate.Accepting() || metrics.Snapshot().ShutdownNanos == 0 {
		t.Fatal("shutdown did not pause and record duration")
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before+1 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if runtime.NumGoroutine() > before+1 {
		t.Fatalf("owned workers leaked: before=%d after=%d", before, runtime.NumGoroutine())
	}
}

func TestRuntimeMetricLabelsAreClosed(t *testing.T) {
	metrics := NewRuntimeMetrics()
	if err := metrics.RecordLeaseState(LeaseMetricState("owner-123")); err == nil {
		t.Fatal("unbounded lease label accepted")
	}
	if err := metrics.RecordLeaseState(LeaseMetricHeld); err != nil {
		t.Fatal(err)
	}
	metrics.RecordSchedulingLag(5)
	if snapshot := metrics.Snapshot(); snapshot.LeaseState != LeaseMetricHeld || snapshot.SchedulingLag != 5 {
		t.Fatalf("metrics = %#v", snapshot)
	}
}

func TestRecoveryReadinessIsOrderedMeasuredAndNeverActivates(t *testing.T) {
	const fiveMinutes = LogicalTime(5 * time.Minute)
	clock, _ := NewDeterministicClock(1)
	gate := NewSafetyGate()
	recovery, err := NewRecoveryGate(clock, gate)
	if err != nil {
		t.Fatal(err)
	}
	stages := RecoverySequence()
	if err := recovery.Complete(stages[1]); err == nil {
		t.Fatal("out-of-order recovery stage accepted")
	}
	for index, stage := range stages {
		at := 1 + LogicalTime(index+1)*(fiveMinutes-2)/LogicalTime(len(stages))
		if err := clock.Advance(at); err != nil {
			t.Fatal(err)
		}
		if err := recovery.Complete(stage); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := recovery.Snapshot()
	if !snapshot.Ready || snapshot.Elapsed >= fiveMinutes || snapshot.Completed != snapshot.Total {
		t.Fatalf("recovery snapshot = %#v", snapshot)
	}
	if state, _ := gate.State(); state != StateLocked || gate.Accepting() {
		t.Fatalf("recovery auto-activated execution: state=%s", state)
	}
}

func TestRecoveryQualificationProfileWithinFiveMinutes(t *testing.T) {
	clock := NewRealClock()
	gate := NewSafetyGate()
	recovery, err := NewRecoveryGate(clock, gate)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range RecoverySequence() {
		if err := recovery.Complete(stage); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := recovery.Snapshot()
	if !snapshot.Ready || snapshot.Elapsed >= LogicalTime(5*time.Minute) {
		t.Fatalf("recovery qualification snapshot = %#v", snapshot)
	}
	if gate.Accepting() {
		t.Fatal("qualification profile enabled strategy execution")
	}
	t.Logf("A3 recovery conformance profile ready in %d ns", snapshot.Elapsed)
}
