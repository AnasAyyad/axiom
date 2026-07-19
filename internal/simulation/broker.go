package simulation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	runtimecore "axiom/internal/runtime"
)

// BrokerModels binds every versioned model used by one simulated broker.
type BrokerModels struct {
	Fee     FeeModel
	Price   PriceModel
	Latency LatencyModel
	Fill    FillModel
}

// SimulatedBroker performs no network I/O and owns no external transport.
type SimulatedBroker struct {
	randomness *runtimecore.Randomness
	timeline   MarketTimeline
	metadata   MetadataSource
	guard      BoundaryGuard
	liquidity  *LiquidityLedger
	models     BrokerModels
	mutex      sync.Mutex
	orders     map[string]execution.OrderEvent
}

// NewBroker constructs a fail-closed simulation boundary.
func NewBroker(
	randomness *runtimecore.Randomness,
	timeline MarketTimeline,
	metadata MetadataSource,
	guard BoundaryGuard,
	liquidity *LiquidityLedger,
	models BrokerModels,
) (*SimulatedBroker, error) {
	if randomness == nil || timeline == nil || metadata == nil || guard == nil || liquidity == nil ||
		models.Fee.Version == "" || models.Price.Version == "" || models.Latency.Version == "" || models.Fill.Version == "" {
		return nil, simulationError("broker_configuration_invalid")
	}
	return &SimulatedBroker{randomness: randomness, timeline: timeline, metadata: metadata,
		guard: guard, liquidity: liquidity, models: models, orders: make(map[string]execution.OrderEvent)}, nil
}

// Submit simulates each leg using only a post-latency market observation.
func (broker *SimulatedBroker) Submit(ctx context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	events := make([]execution.OrderEvent, 0, len(plan.Legs)*2)
	for _, leg := range plan.Legs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		legEvents, err := broker.simulateLeg(plan, leg)
		if err != nil {
			return nil, err
		}
		events = append(events, legEvents...)
		broker.remember(legEvents)
	}
	return events, nil
}

// Cancel emits only a local canonical cancellation fact.
func (broker *SimulatedBroker) Cancel(
	ctx context.Context,
	orderID domain.VirtualOrderID,
	reason string,
) ([]execution.OrderEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if orderID.Value() == "" || reason == "" {
		return nil, simulationError("cancel_invalid")
	}
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	prior, exists := broker.orders[orderID.String()]
	if !exists || terminalOrderState(prior.State) {
		return nil, simulationError("order_not_cancelable")
	}
	pending := prior
	pending.ID = fmt.Sprintf("%s-cancel-pending", orderID.Value())
	pending.State, pending.ExchangeStatus, pending.Fills = execution.OrderCancelPending, "CANCEL_PENDING", nil
	pending.Ordinal++
	pending.OccurredAt = pending.OccurredAt.Add(time.Nanosecond)
	canceled := pending
	canceled.ID = fmt.Sprintf("%s-canceled", orderID.Value())
	canceled.State, canceled.ExchangeStatus = execution.OrderCanceled, "CANCELED"
	canceled.Ordinal++
	canceled.OccurredAt = canceled.OccurredAt.Add(time.Nanosecond)
	broker.orders[orderID.String()] = canceled
	return []execution.OrderEvent{pending, canceled}, nil
}

func (broker *SimulatedBroker) simulateLeg(
	plan execution.SimulatedPlan,
	leg execution.PlannedLeg,
) ([]execution.OrderEvent, error) {
	key := modelKey(plan, leg)
	arrival, err := broker.arrivalTime(plan.DecisionLogicalTime, key)
	if err != nil {
		return nil, err
	}
	if arrival > leg.ExpiresAt {
		return withSubmitting(leg, plan.DecisionLogicalTime,
			terminalWithoutFill(leg, execution.OrderExpired, arrival)), nil
	}
	state, ok, err := broker.timeline.AtOrAfter(leg.Instrument, arrival)
	if err != nil || !ok || state.LogicalTime < arrival || state.LogicalTime < plan.DecisionLogicalTime {
		return withSubmitting(leg, plan.DecisionLogicalTime,
			terminalWithoutFill(leg, execution.OrderExpired, arrival)), nil
	}
	owned, err := broker.guard.Authorize(leg, cloneBook(state))
	if err != nil {
		return nil, simulationError("broker_boundary_rejected")
	}
	metadata, err := broker.metadata.Metadata(cloneBook(state))
	if err != nil {
		return nil, simulationError("metadata_unavailable")
	}
	quantity, err := filterQuantity(leg, owned, metadata)
	if err != nil {
		return nil, err
	}
	if leg.Maker {
		return withArrival(leg, plan.DecisionLogicalTime, state.LogicalTime, nil), nil
	}
	disposition, err := broker.models.Fill.Disposition(broker.randomness, key)
	if err != nil {
		return nil, err
	}
	maximum, err := broker.models.Fill.Limit(quantity, disposition)
	if err != nil || maximum.String() == "0" {
		return withArrival(leg, plan.DecisionLogicalTime, state.LogicalTime,
			terminalWithoutFill(leg, execution.OrderExpired, state.LogicalTime)), nil
	}
	fill, err := broker.consume(plan.Namespace, leg, state, maximum)
	if err != nil || fill.Quantity.String() == "0" {
		return withArrival(leg, plan.DecisionLogicalTime, state.LogicalTime,
			terminalWithoutFill(leg, execution.OrderExpired, state.LogicalTime)), err
	}
	filled, err := broker.fillEvents(leg, state.LogicalTime, quantity, fill)
	return withArrival(leg, plan.DecisionLogicalTime, state.LogicalTime, filled), err
}

func (broker *SimulatedBroker) arrivalTime(
	decision uint64,
	key runtimecore.RandomKey,
) (uint64, error) {
	latency, err := broker.models.Latency.Sample(broker.randomness, key)
	if err != nil || latency < 0 || uint64(latency) > ^uint64(0)-decision {
		return 0, simulationError("arrival_time_invalid")
	}
	return decision + uint64(latency), nil
}

func validatePlan(plan execution.SimulatedPlan) error {
	if plan.ID.Value() == "" || plan.Intent.DecisionID.Value() == "" || plan.Intent.ApprovalHash == "" ||
		plan.Intent.PolicyHash == "" || plan.Namespace == "" || plan.DecisionLogicalTime == 0 || len(plan.Legs) == 0 {
		return simulationError("plan_invalid")
	}
	for index, leg := range plan.Legs {
		if leg.Index != uint32(index) || leg.OrderID.Value() == "" || leg.ClientOrderID == "" ||
			leg.Quantity.String() == "0" || leg.LimitPrice.String() == "0" || leg.ExpiresAt <= plan.DecisionLogicalTime {
			return simulationError("plan_invalid")
		}
	}
	return nil
}

func modelKey(plan execution.SimulatedPlan, leg execution.PlannedLeg) runtimecore.RandomKey {
	return runtimecore.RandomKey{RunID: plan.Namespace, ComponentID: "simulation-models",
		DecisionID: plan.Intent.DecisionID.String(), OrderLegID: leg.OrderID.String(),
		EventID: fmt.Sprintf("leg-%d", leg.Index)}
}

var _ execution.Broker = (*SimulatedBroker)(nil)

func eventTime(logical uint64) time.Time { return time.Unix(0, int64(logical)).UTC() }

func (broker *SimulatedBroker) remember(events []execution.OrderEvent) {
	if len(events) == 0 {
		return
	}
	broker.mutex.Lock()
	defer broker.mutex.Unlock()
	last := events[len(events)-1]
	broker.orders[last.OrderID.String()] = last
}

func terminalOrderState(state execution.OrderState) bool {
	return state == execution.OrderFilled || state == execution.OrderCanceled || state == execution.OrderRejected ||
		state == execution.OrderExpired || state == execution.OrderRecovered
}
