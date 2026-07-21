package execution

import (
	"strings"
	"testing"

	"axiom/internal/domain"
)

func TestSagaPersistsRecoveryAndCheckpointDefensively(t *testing.T) {
	planID, _ := domain.NewExecutionPlanID("plan-one")
	firstID, _ := domain.NewVirtualOrderID("order-one")
	secondID, _ := domain.NewVirtualOrderID("order-two")
	reservationID, _ := domain.NewReservationID("reservation-one")
	depends := uint32(0)
	reducer, err := NewSaga(planID, DispatchSequential, []SagaLeg{
		{Index: 0, OrderID: firstID, State: OrderCreated},
		{Index: 1, OrderID: secondID, DependsOn: &depends, State: OrderCreated},
	}, []domain.ReservationID{reservationID})
	if err != nil || reducer.Activate() != nil {
		t.Fatal(err)
	}
	asset, _ := domain.ParseAssetSymbol("BTC")
	exposure, _ := domain.ParseBalance("0.5")
	order := sagaOrder(t, planID, firstID, OrderFilled)
	if err = reducer.ApplyOrder(order, []Exposure{{Asset: asset, Quantity: exposure}}); err != nil {
		t.Fatal(err)
	}
	order = sagaOrder(t, planID, secondID, OrderRejected)
	if err = reducer.ApplyOrder(order, []Exposure{{Asset: asset, Quantity: exposure}}); err != nil {
		t.Fatal(err)
	}
	if reducer.Snapshot().State != PlanRecoveryRequired {
		t.Fatalf("state = %s", reducer.Snapshot().State)
	}
	if err = reducer.AddRecovery(RecoveryAttempt{Attempt: 1, Action: "risk_reduce",
		Disposition: "quarantine", LossAsset: asset, Loss: exposure}); err != nil {
		t.Fatal(err)
	}
	if err = reducer.ResolveRecovery("manual_review", true); err != nil {
		t.Fatal(err)
	}
	checkpoint := Checkpoint{RunManifestHash: strings.Repeat("a", 64), CursorOrdinal: 2,
		CursorLogicalTime: 20, Orders: []Order{order}, Plans: []Saga{reducer.Snapshot()}, LiquidityHash: "liquidity",
		JournalHash: "journal", ProjectionHash: "projection", ModelNamespace: "models-v1",
		RandomStateHash: "random", Revision: 1}
	store := NewMemoryCheckpointStore()
	if err = store.Save(checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.Plans[0].FinalDisposition = "mutated"
	restored, ok, err := store.Load(strings.Repeat("a", 64))
	if err != nil || !ok || restored.Plans[0].FinalDisposition != "manual_review" {
		t.Fatal("checkpoint was not atomically cloned")
	}
}

func sagaOrder(t *testing.T, planID domain.ExecutionPlanID, orderID domain.VirtualOrderID, state OrderState) Order {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	return Order{Identity: OrderIdentity{ID: orderID, PlanID: planID, ClientOrderID: orderID.Value(),
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity}, State: state, Revision: 2}
}
