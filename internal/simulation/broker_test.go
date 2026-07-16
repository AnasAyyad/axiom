package simulation

import (
	"context"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	runtimecore "axiom/internal/runtime"
)

func TestBrokerUsesPostLatencyBookAndProducesExactFill(t *testing.T) {
	broker := testBroker(t, bookState(t, 110, "100", "1"), balance(t, "0"))
	plan := testPlan(t, "combined", false)
	events, err := broker.Submit(context.Background(), plan)
	if err != nil || len(events) != 3 || events[2].State != execution.OrderFilled ||
		events[2].Fills[0].Price.String() != "100" || events[2].Fees[0].Total.String() != "0.1" {
		t.Fatalf("events = %#v, %v", events, err)
	}
	reducer := approvedReducer(t, plan.Legs[0])
	for _, event := range events {
		if _, err = reducer.Reduce(event); err != nil {
			t.Fatal(err)
		}
	}
	if reducer.Snapshot().State != execution.OrderFilled {
		t.Fatalf("state = %s", reducer.Snapshot().State)
	}
}

func TestBrokerRejectsSignalStateAndUnownedSell(t *testing.T) {
	stale := testBroker(t, bookState(t, 100, "100", "1"), balance(t, "0"))
	events, err := stale.Submit(context.Background(), testPlan(t, "combined", false))
	if err != nil || events[len(events)-1].State != execution.OrderExpired || len(events[len(events)-1].Fills) != 0 {
		t.Fatalf("stale events = %#v, %v", events, err)
	}
	broker := testBroker(t, bookState(t, 110, "100", "1"), balance(t, "0"))
	plan := testPlan(t, "combined", false)
	plan.Legs[0].Side = domain.SideSell
	plan.Legs[0].LimitPrice, _ = domain.ParsePrice("99")
	if codeOfBroker(broker.Submit(context.Background(), plan)) != "quantity_filter_invalid" {
		t.Fatal("unowned sell was accepted")
	}
}

func TestBrokerSharedAndIndependentLiquidityNamespaces(t *testing.T) {
	broker := testBroker(t, bookState(t, 110, "100", "0.4"), balance(t, "0"))
	first, err := broker.Submit(context.Background(), testPlan(t, "combined", false))
	if err != nil || first[len(first)-2].State != execution.OrderPartiallyFilled {
		t.Fatalf("first = %#v, %v", first, err)
	}
	secondPlan := testPlan(t, "combined", false)
	secondPlan.Legs[0].OrderID, _ = domain.NewVirtualOrderID("order-two")
	secondPlan.Legs[0].ClientOrderID = "ax-order-two"
	second, err := broker.Submit(context.Background(), secondPlan)
	if err != nil || second[len(second)-1].State != execution.OrderExpired || len(second[len(second)-1].Fills) != 0 {
		t.Fatalf("second = %#v, %v", second, err)
	}
	independentPlan := secondPlan
	independentPlan.Namespace = "independent"
	independentPlan.Legs[0].OrderID, _ = domain.NewVirtualOrderID("order-three")
	independentPlan.Legs[0].ClientOrderID = "ax-order-three"
	independent, err := broker.Submit(context.Background(), independentPlan)
	if err != nil || independent[len(independent)-2].State != execution.OrderPartiallyFilled {
		t.Fatalf("independent = %#v, %v", independent, err)
	}
}

func TestBrokerMakerOrderCanCancelWithoutExternalTransport(t *testing.T) {
	broker := testBroker(t, bookState(t, 110, "100", "1"), balance(t, "0"))
	plan := testPlan(t, "combined", true)
	events, err := broker.Submit(context.Background(), plan)
	if err != nil || events[len(events)-1].State != execution.OrderAcknowledged {
		t.Fatalf("maker events = %#v, %v", events, err)
	}
	cancel, err := broker.Cancel(context.Background(), plan.Legs[0].OrderID, "owner_request")
	if err != nil || len(cancel) != 2 || cancel[0].State != execution.OrderCancelPending ||
		cancel[1].State != execution.OrderCanceled {
		t.Fatalf("cancel = %#v, %v", cancel, err)
	}
}

func testBroker(t *testing.T, state BookState, owned domain.Balance) *SimulatedBroker {
	t.Helper()
	randomness, _ := newTestRandomness()
	zeroRate, _ := domain.ParseRate("0")
	feeRate, _ := domain.ParseRate("0.001")
	zeroPercent, _ := domain.ParsePercent("0")
	partialRatio, _ := domain.ParsePercent("0.5")
	models := BrokerModels{
		Fee: FeeModel{Version: "fee-v1", TakerRate: feeRate, MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 8},
		Price: PriceModel{Version: "price-v1", Spread: zeroPercent, Slippage: zeroPercent,
			Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 8},
		Latency: LatencyModel{Version: "latency-v1", Samples: []time.Duration{10}},
		Fill:    FillModel{Version: "fill-v1", PartialRatio: partialRatio, QuantityScale: 8},
	}
	broker, err := NewBroker(randomness, staticTimeline{state: state}, staticMetadata{value: metadata(t)},
		staticGuard{owned: owned}, NewLiquidityLedger(), models)
	if err != nil {
		t.Fatal(err)
	}
	return broker
}

func testPlan(t *testing.T, namespace string, maker bool) execution.SimulatedPlan {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("plan-one")
	orderID, _ := domain.NewVirtualOrderID("order-one")
	decisionID, _ := domain.NewDecisionID("decision-one")
	quantity, _ := domain.ParseQuantity("1")
	limit, _ := domain.ParsePrice("102")
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	return execution.SimulatedPlan{ID: planID,
		Intent: execution.ApprovedIntent{DecisionID: decisionID, ApprovalHash: strings.Repeat("a", 64),
			PolicyHash: strings.Repeat("b", 64)}, Namespace: namespace, DecisionLogicalTime: 100,
		Legs: []execution.PlannedLeg{{Index: 0, OrderID: orderID, ClientOrderID: "ax-order-one",
			Instrument: instrument, Side: domain.SideBuy, Quantity: quantity, LimitPrice: limit, ExpiresAt: 200, Maker: maker}}}
}

func metadata(t *testing.T) domain.InstrumentMetadata {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	tick, _ := domain.ParsePrice("0.01")
	step, _ := domain.ParseQuantity("0.001")
	minimumQuantity, _ := domain.ParseQuantity("0.001")
	minimumNotional, _ := domain.ParseNotional("1")
	return domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: time.Unix(1, 0).UTC(),
		PriceTick: tick, QuantityStep: step, MinimumQuantity: minimumQuantity, MinimumNotional: minimumNotional}
}

func bookState(t *testing.T, logical uint64, askPrice, askQuantity string) BookState {
	t.Helper()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	ask, _ := domain.ParsePrice(askPrice)
	bid, _ := domain.ParsePrice("99")
	quantity, _ := domain.ParseQuantity(askQuantity)
	return BookState{Exchange: "binance", Instrument: instrument, Version: 1, LogicalTime: logical,
		Bids: []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: ask, Quantity: quantity}}}
}

func approvedReducer(t *testing.T, leg execution.PlannedLeg) *execution.OrderReducer {
	t.Helper()
	planID, _ := domain.NewExecutionPlanID("plan-one")
	reducer, err := execution.NewOrderReducer(execution.OrderIdentity{ID: leg.OrderID, PlanID: planID,
		ClientOrderID: leg.ClientOrderID, Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity})
	if err != nil {
		t.Fatal(err)
	}
	zero, _ := domain.ParseQuantity("0")
	for index, state := range []execution.OrderState{execution.OrderValidating, execution.OrderReserved, execution.OrderApproved} {
		event := execution.OrderEvent{ID: "approval-" + string(state), OrderID: leg.OrderID,
			ClientOrderID: leg.ClientOrderID, State: state, ExchangeStatus: string(state), CumulativeQuantity: zero,
			OccurredAt: time.Unix(0, int64(index+1)).UTC(), Ordinal: uint64(index + 1)}
		if _, err = reducer.Reduce(event); err != nil {
			t.Fatal(err)
		}
	}
	return reducer
}

type staticTimeline struct{ state BookState }

func (timeline staticTimeline) AtOrAfter(domain.Instrument, uint64) (BookState, bool, error) {
	return cloneBook(timeline.state), true, nil
}

type staticMetadata struct{ value domain.InstrumentMetadata }

func (metadata staticMetadata) Metadata(BookState) (domain.InstrumentMetadata, error) {
	return metadata.value, nil
}

type staticGuard struct{ owned domain.Balance }

func (guard staticGuard) Authorize(execution.PlannedLeg, BookState) (domain.Balance, error) {
	return guard.owned, nil
}

func balance(t *testing.T, value string) domain.Balance {
	t.Helper()
	balance, _ := domain.ParseBalance(value)
	return balance
}

func newTestRandomness() (*runtimecore.Randomness, error) {
	return runtimecore.NewRandomness(make([]byte, 32))
}

func codeOfBroker(_ []execution.OrderEvent, err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}
