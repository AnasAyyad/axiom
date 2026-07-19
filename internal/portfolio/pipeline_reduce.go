package portfolio

import (
	"context"
	"encoding/json"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/execution"
)

// ReduceSimulation validates canonical order events, settles exact fills, and
// returns the authoritative in-process virtual portfolio projection.
func (adapter *PipelineAllocator) ReduceSimulation(_ context.Context, allocated backtest.AllocatedIntent,
	plan execution.SimulatedPlan, events []execution.OrderEvent) (json.RawMessage, json.RawMessage, error) {
	var allocation Allocation
	if json.Unmarshal(allocated.Payload, &allocation) != nil || len(plan.Legs) != 1 || len(events) == 0 {
		return nil, nil, portfolioError("pipeline_reduction_invalid")
	}
	leg := plan.Legs[0]
	identity := execution.OrderIdentity{ID: leg.OrderID, PlanID: plan.ID, ClientOrderID: leg.ClientOrderID,
		Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity}
	order, applied, err := execution.ReduceOrderEvents(identity, events)
	if err != nil {
		return nil, nil, err
	}
	current, settled := allocation, false
	for _, event := range applied {
		for index, fill := range event.Fills {
			final := event.State == execution.OrderFilled && index == len(event.Fills)-1
			current, err = adapter.allocator.ApplyFill(current, fill, final)
			if err != nil {
				return nil, nil, err
			}
			settled = settled || final
		}
	}
	if !settled {
		if err = closeUnfilledAllocation(adapter.allocator, current, order.State); err != nil {
			return nil, nil, err
		}
	}
	orders, _ := json.Marshal([]execution.Order{order})
	balances, _ := json.Marshal(adapter.allocator.portfolio.Snapshot())
	return orders, balances, nil
}

func closeUnfilledAllocation(allocator *Allocator, allocation Allocation, state execution.OrderState) error {
	switch state {
	case execution.OrderExpired, execution.OrderCanceled:
		return allocator.Close(allocation, accounting.ReservationExpired)
	case execution.OrderRejected:
		return allocator.Close(allocation, accounting.ReservationReleased)
	default:
		return portfolioError("pipeline_order_nonterminal")
	}
}
