package execution

import (
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestOrderReducerAppliesDuplicatesStaleAndFillsIdempotently(t *testing.T) {
	reducer := testReducer(t)
	ordinal := uint64(1)
	for _, state := range []OrderState{OrderValidating, OrderReserved, OrderApproved,
		OrderSubmitting, OrderAcknowledged} {
		result, err := reducer.Reduce(testEvent(t, state, ordinal, "0", nil))
		if err != nil || !result.Applied {
			t.Fatalf("state %s = %#v, %v", state, result, err)
		}
		ordinal++
	}
	partial := testFill(t, "fill-one", "0.4", ordinal)
	event := testEvent(t, OrderPartiallyFilled, ordinal, "0.4", []FillFact{partial})
	result, err := reducer.Reduce(event)
	if err != nil || !result.Applied || len(result.Order.Fills) != 1 {
		t.Fatalf("partial = %#v, %v", result, err)
	}
	duplicate, err := reducer.Reduce(event)
	if err != nil || !duplicate.Duplicate || duplicate.Order.Revision != result.Order.Revision {
		t.Fatalf("duplicate = %#v, %v", duplicate, err)
	}
	stale := testEvent(t, OrderAcknowledged, ordinal-1, "0.4", nil)
	stale.ID = "late-stale-event"
	stale.Fees = cumulativeFees(t)
	stale.ExchangeStatus = "PARTIALLY_FILLED"
	staleResult, err := reducer.Reduce(stale)
	if err != nil || !staleResult.Stale {
		t.Fatalf("stale = %#v, %v", staleResult, err)
	}
	finalFill := testFill(t, "fill-two", "0.6", ordinal+1)
	filled := testEvent(t, OrderFilled, ordinal+1, "1", []FillFact{finalFill})
	filled.Fees = cumulativeFees(t)
	filledResult, err := reducer.Reduce(filled)
	if err != nil || !filledResult.Applied || len(filledResult.Order.Fills) != 2 ||
		filledResult.Order.CumulativeQuantity.String() != "1" {
		t.Fatalf("filled = %#v, %v", filledResult, err)
	}
}

func TestOrderReducerRejectsImpossibleAndConflictingFacts(t *testing.T) {
	reducer := testReducer(t)
	advanceToAcknowledged(t, reducer)
	partial := testFill(t, "fill-one", "0.4", 6)
	event := testEvent(t, OrderPartiallyFilled, 6, "0.4", []FillFact{partial})
	if _, err := reducer.Reduce(event); err != nil {
		t.Fatal(err)
	}
	decreased := testEvent(t, OrderPartiallyFilled, 7, "0.3", nil)
	if codeOfReduction(reducer.Reduce(decreased)) != "cumulative_fill_decreased" {
		t.Fatal("decreasing cumulative fill was accepted")
	}
	conflict := event
	conflict.State = OrderFilled
	if codeOfReduction(reducer.Reduce(conflict)) != "event_identity_conflict" {
		t.Fatal("conflicting event identity was accepted")
	}
	wrongOrder, _ := domain.NewVirtualOrderID("other")
	identity := testEvent(t, OrderFilled, 8, "1", []FillFact{testFill(t, "fill-two", "0.6", 8)})
	identity.OrderID = wrongOrder
	if codeOfReduction(reducer.Reduce(identity)) != "immutable_identity_conflict" {
		t.Fatal("conflicting immutable identity was accepted")
	}
}

func TestOrderReducerHandlesCancelFillRaceAndLateFill(t *testing.T) {
	reducer := testReducer(t)
	advanceToAcknowledged(t, reducer)
	if _, err := reducer.Reduce(testEvent(t, OrderCancelPending, 6, "0", nil)); err != nil {
		t.Fatal(err)
	}
	partial := testEvent(t, OrderPartiallyFilled, 7, "0.4", []FillFact{testFill(t, "fill-one", "0.4", 7)})
	if _, err := reducer.Reduce(partial); err != nil {
		t.Fatal(err)
	}
	canceled := testEvent(t, OrderCanceled, 8, "0.4", nil)
	canceled.Fees = cumulativeFees(t)
	if _, err := reducer.Reduce(canceled); err != nil {
		t.Fatal(err)
	}
	late := testEvent(t, OrderFilled, 9, "1", []FillFact{testFill(t, "fill-two", "0.6", 9)})
	late.Fees = cumulativeFees(t)
	result, err := reducer.Reduce(late)
	if err != nil || result.Order.State != OrderFilled || len(result.Order.Fills) != 2 {
		t.Fatalf("late fill = %#v, %v", result, err)
	}
}

func TestCanonicalTransitionMatrix(t *testing.T) {
	states := []OrderState{OrderCreated, OrderValidating, OrderReserved, OrderApproved,
		OrderSubmitting, OrderAcknowledged, OrderPartiallyFilled, OrderFilled,
		OrderCancelPending, OrderCanceled, OrderRejected, OrderExpired, OrderUnknown,
		OrderRecoveryRequired, OrderRecovered}
	for _, from := range states {
		if !transitionAllowed(from, from) {
			t.Fatalf("same-state transition rejected: %s", from)
		}
		for _, to := range allowedTransitions[from] {
			if !transitionAllowed(from, to) {
				t.Fatalf("declared transition rejected: %s -> %s", from, to)
			}
		}
	}
	if transitionAllowed(OrderFilled, OrderAcknowledged) || transitionAllowed(OrderRecovered, OrderCreated) {
		t.Fatal("terminal regression accepted")
	}
}

func TestClientOrderIDIsStableAndTraceDimensionsChangeIt(t *testing.T) {
	strategy, _ := domain.NewStrategyID("trend")
	decision, _ := domain.NewDecisionID("decision-one")
	identity := ClientOrderIdentity{Mode: "replay", StrategyID: strategy, DecisionID: decision, Leg: 1, Attempt: 1}
	first, err := GenerateClientOrderID(identity)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := GenerateClientOrderID(identity)
	identity.Attempt = 2
	third, _ := GenerateClientOrderID(identity)
	if first != second || first == third || len(first) != 35 || !strings.HasPrefix(first, "ax-") {
		t.Fatalf("client IDs = %q/%q/%q", first, second, third)
	}
}

func testReducer(t *testing.T) *OrderReducer {
	t.Helper()
	orderID, _ := domain.NewVirtualOrderID("order-one")
	planID, _ := domain.NewExecutionPlanID("plan-one")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	quantity, _ := domain.ParseQuantity("1")
	reducer, err := NewOrderReducer(OrderIdentity{ID: orderID, PlanID: planID, ClientOrderID: "ax-client-one",
		Instrument: instrument, Side: domain.SideBuy, Quantity: quantity})
	if err != nil {
		t.Fatal(err)
	}
	return reducer
}

func advanceToAcknowledged(t *testing.T, reducer *OrderReducer) {
	t.Helper()
	for index, state := range []OrderState{OrderValidating, OrderReserved, OrderApproved,
		OrderSubmitting, OrderAcknowledged} {
		if _, err := reducer.Reduce(testEvent(t, state, uint64(index+1), "0", nil)); err != nil {
			t.Fatal(err)
		}
	}
}

func testEvent(t *testing.T, state OrderState, ordinal uint64, cumulative string, fills []FillFact) OrderEvent {
	t.Helper()
	orderID, _ := domain.NewVirtualOrderID("order-one")
	quantity, _ := domain.ParseQuantity(cumulative)
	status := string(state)
	fees := []FeeFact(nil)
	if cumulative != "0" {
		fees = cumulativeFees(t)
	}
	return OrderEvent{ID: "event-" + strings.ToLower(string(state)) + "-" + stateString(ordinal),
		OrderID: orderID, ClientOrderID: "ax-client-one",
		State: state, ExchangeStatus: status, CumulativeQuantity: quantity, Fees: fees, Fills: fills,
		OccurredAt: time.Unix(1_700_000_000+int64(ordinal), 0).UTC(), Ordinal: ordinal}
}

func testFill(t *testing.T, id, quantityText string, ordinal uint64) FillFact {
	t.Helper()
	fillID, _ := domain.NewVirtualFillID(id)
	quantity, _ := domain.ParseQuantity(quantityText)
	price, _ := domain.ParsePrice("100")
	fee, _ := domain.ParseFee("0.01")
	asset, _ := domain.ParseAssetSymbol("USDT")
	return FillFact{ID: fillID, Quantity: quantity, Price: price, Fee: fee, FeeAsset: asset, Ordinal: ordinal}
}

func cumulativeFees(t *testing.T) []FeeFact {
	t.Helper()
	asset, _ := domain.ParseAssetSymbol("USDT")
	fee, _ := domain.ParseFee("0.01")
	return []FeeFact{{Asset: asset, Total: fee}}
}

func codeOfReduction(_ Reduction, err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}

func stateString(value uint64) string {
	const digits = "0123456789"
	if value < 10 {
		return string(digits[value])
	}
	return "many"
}
