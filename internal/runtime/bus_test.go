package runtimecore

import (
	"testing"

	"axiom/internal/domain"
)

func TestCriticalSaturationRejectsWithoutLossAndLocks(t *testing.T) {
	gate := NewSafetyGate()
	if err := gate.ManualActivate(true); err != nil {
		t.Fatal(err)
	}
	bus, _ := NewEventBus(gate)
	partition := Partition{Kind: PartitionOrder, Key: "virtual-order-a"}
	if err := bus.Register(partition, ClassCritical, 1, false); err != nil {
		t.Fatal(err)
	}
	bus.Seal()
	first := busEvent(t, "event-a", ClassCritical, 1, "")
	if err := bus.Publish(partition, first); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(partition, busEvent(t, "event-b", ClassCritical, 1, "")); err == nil {
		t.Fatal("critical saturation was acknowledged")
	}
	if state, _ := gate.State(); state != StateLocked {
		t.Fatalf("state = %s", state)
	}
	metrics := bus.Metrics(10)[PartitionOrder]
	if metrics.Capacity != 1 || metrics.Depth != 1 || metrics.OldestAge != 9 ||
		metrics.SaturationAge != 9 || metrics.Saturations != 1 || metrics.Dropped != 0 {
		t.Fatalf("saturation metrics = %#v", metrics)
	}
	consumed, ok := bus.Consume(partition)
	if !ok || consumed.ID != first.ID {
		t.Fatal("accepted critical event was lost")
	}
}

func TestQueueMetricAggregationUsesOnlyBoundedPartitionKinds(t *testing.T) {
	gate := NewSafetyGate()
	bus, _ := NewEventBus(gate)
	first := Partition{Kind: PartitionOrder, Key: "order-a"}
	second := Partition{Kind: PartitionOrder, Key: "order-b"}
	_ = bus.Register(first, ClassCritical, 2, false)
	_ = bus.Register(second, ClassCritical, 3, false)
	bus.Seal()
	_ = bus.Publish(first, busEvent(t, "event-a", ClassCritical, 1, ""))
	metrics := bus.Metrics(5)
	if len(metrics) != 1 || metrics[PartitionOrder].Capacity != 5 || metrics[PartitionOrder].Depth != 1 {
		t.Fatalf("bounded aggregate metrics = %#v", metrics)
	}
}

func TestMarketOverflowInvalidatesGenerationAndRequiresResync(t *testing.T) {
	gate := NewSafetyGate()
	_ = gate.ManualActivate(true)
	bus, _ := NewEventBus(gate)
	partition := Partition{Kind: PartitionExchangeInstrument, Key: "binance:BTCUSDT"}
	_ = bus.Register(partition, ClassMarketData, 1, false)
	bus.Seal()
	if err := bus.Publish(partition, busEvent(t, "event-a", ClassMarketData, 1, "")); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(partition, busEvent(t, "event-b", ClassMarketData, 1, "")); err == nil {
		t.Fatal("market overflow accepted")
	}
	if _, ok := bus.Consume(partition); ok {
		t.Fatal("stale market event survived invalidation")
	}
	if err := bus.Publish(partition, busEvent(t, "event-c", ClassMarketData, 1, "")); err == nil {
		t.Fatal("invalid generation accepted")
	}
	if err := bus.Resynchronize(partition, 2); err != nil {
		t.Fatal(err)
	}
	if err := bus.Publish(partition, busEvent(t, "event-d", ClassMarketData, 2, "")); err != nil {
		t.Fatal(err)
	}
	if state, _ := gate.State(); state != StatePaused {
		t.Fatalf("resync auto-activated gate: %s", state)
	}
}

func TestProjectionCoalescesOnlyWithSnapshotRecovery(t *testing.T) {
	gate := NewSafetyGate()
	bus, _ := NewEventBus(gate)
	partition := Partition{Kind: PartitionRiskState, Key: "portfolio-a"}
	if err := bus.Register(partition, ClassProjection, 1, true); err != nil {
		t.Fatal(err)
	}
	bus.Seal()
	_ = bus.Publish(partition, busEvent(t, "event-a", ClassProjection, 1, "risk"))
	_ = bus.Publish(partition, busEvent(t, "event-b", ClassProjection, 1, "risk"))
	event, ok := bus.Consume(partition)
	if !ok || event.ID.Value() != "event-b" {
		t.Fatalf("coalesced event = %#v", event)
	}
	metrics := bus.Metrics(10)[PartitionRiskState]
	if metrics.Coalesced != 1 || len(bus.Metrics(10)) != 1 {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestSafetyGateNeverAutomaticallyActivates(t *testing.T) {
	gate := NewSafetyGate()
	if state, _ := gate.State(); state != StatePaused || gate.Accepting() {
		t.Fatal("gate did not start paused")
	}
	if err := gate.ManualActivate(false); err == nil || gate.Accepting() {
		t.Fatal("unconfirmed activation accepted")
	}
	if err := gate.ManualActivate(true); err != nil || !gate.Accepting() {
		t.Fatal("confirmed activation rejected")
	}
	gate.Lock("fault")
	if err := gate.ManualActivate(true); err == nil {
		t.Fatal("locked gate reactivated")
	}
}

func busEvent(t *testing.T, value string, class EventClass, generation uint64, coalesceKey string) BusEvent {
	t.Helper()
	id, err := domain.NewEventID(value)
	if err != nil {
		t.Fatal(err)
	}
	return BusEvent{ID: id, Class: class, Generation: generation, CoalesceKey: coalesceKey, EnqueuedAt: 1}
}
