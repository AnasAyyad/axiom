package simulation

import (
	"fmt"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func withSubmitting(
	leg execution.PlannedLeg,
	decision uint64,
	terminal []execution.OrderEvent,
) []execution.OrderEvent {
	zero, _ := domain.ParseQuantity("0")
	states := []execution.OrderState{execution.OrderValidating, execution.OrderReserved,
		execution.OrderApproved, execution.OrderSubmitting}
	events := make([]execution.OrderEvent, 0, len(states)+len(terminal))
	for index, state := range states {
		ordinal := decision + uint64(index)
		events = append(events, execution.OrderEvent{ID: fmt.Sprintf("%s-%s", leg.OrderID.Value(), state),
			OrderID: leg.OrderID, ClientOrderID: leg.ClientOrderID, State: state, ExchangeStatus: string(state),
			CumulativeQuantity: zero, OccurredAt: eventTime(ordinal), Ordinal: ordinal})
	}
	return append(events, terminal...)
}

func withArrival(
	leg execution.PlannedLeg,
	decision uint64,
	arrival uint64,
	terminal []execution.OrderEvent,
) []execution.OrderEvent {
	events := withSubmitting(leg, decision, nil)
	zero, _ := domain.ParseQuantity("0")
	ackOrdinal := arrival
	if arrival > decision {
		ackOrdinal = arrival - 1
	}
	ack := execution.OrderEvent{ID: fmt.Sprintf("%s-acknowledged", leg.OrderID.Value()), OrderID: leg.OrderID,
		ClientOrderID: leg.ClientOrderID, State: execution.OrderAcknowledged, ExchangeStatus: "ACKNOWLEDGED",
		CumulativeQuantity: zero, OccurredAt: eventTime(ackOrdinal), Ordinal: ackOrdinal}
	events = append(events, ack)
	return append(events, terminal...)
}

func (broker *SimulatedBroker) fillEvents(
	leg execution.PlannedLeg,
	logical uint64,
	requested domain.Quantity,
	fill simulatedFill,
) ([]execution.OrderEvent, error) {
	fees, err := broker.models.Fee.Calculate(fill.Notional, false)
	if err != nil {
		return nil, err
	}
	fillID, err := domain.NewVirtualFillID(fmt.Sprintf("%s-fill", leg.OrderID.Value()))
	if err != nil {
		return nil, simulationError("fill_identity_invalid")
	}
	fact := execution.FillFact{ID: fillID, Quantity: fill.Quantity, Price: fill.Price,
		Fee: fees.Charge, Rebate: fees.Rebate, FeeAsset: leg.Instrument.Quote, Ordinal: logical}
	feeFact := execution.FeeFact{Asset: leg.Instrument.Quote, Total: fees.Charge, Rebate: fees.Rebate}
	state := execution.OrderFilled
	if fill.Quantity.Compare(requested) < 0 {
		state = execution.OrderPartiallyFilled
	}
	event := execution.OrderEvent{ID: fmt.Sprintf("%s-%s", leg.OrderID.Value(), state), OrderID: leg.OrderID,
		ClientOrderID: leg.ClientOrderID, State: state, ExchangeStatus: string(state),
		CumulativeQuantity: fill.Quantity, Fees: []execution.FeeFact{feeFact}, Fills: []execution.FillFact{fact},
		OccurredAt: eventTime(logical), Ordinal: logical}
	if state == execution.OrderFilled {
		return []execution.OrderEvent{event}, nil
	}
	expired := event
	expired.ID = fmt.Sprintf("%s-%s", leg.OrderID.Value(), execution.OrderExpired)
	expired.State, expired.ExchangeStatus, expired.Fills = execution.OrderExpired, string(execution.OrderExpired), nil
	expired.Ordinal++
	expired.OccurredAt = eventTime(logical + 1)
	return []execution.OrderEvent{event, expired}, nil
}
