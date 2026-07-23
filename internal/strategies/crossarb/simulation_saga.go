package crossarb

import (
	"strconv"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func newConcurrentSaga(
	candidate Candidate,
) (*execution.SagaReducer, domain.ExecutionPlanID, error) {
	planID, err := domain.NewExecutionPlanID("b5-" + candidate.ID[:24])
	if err != nil {
		return nil, domain.ExecutionPlanID{}, err
	}
	reservation, _ := domain.NewReservationID("b5-claims-" + candidate.ID[:20])
	legs := make([]execution.SagaLeg, 2)
	for index := range legs {
		orderID, _ := domain.NewVirtualOrderID(
			"b5-" + candidate.ID[:16] + "-leg-" + strconv.Itoa(index+1),
		)
		legs[index] = execution.SagaLeg{
			Index: uint32(index), OrderID: orderID, State: execution.OrderCreated,
		}
	}
	saga, err := execution.NewSaga(planID, execution.DispatchConcurrent, legs,
		[]domain.ReservationID{reservation})
	return saga, planID, err
}

func applyConcurrentSaga(
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	candidate Candidate,
	events []scheduledLeg,
	result *SimulationResult,
) error {
	for _, event := range events {
		leg := result.Legs[event.index]
		quantityFilled := quantity("0")
		if leg.Result != nil {
			quantityFilled = leg.Result.TradeQuantity
		}
		order := simulationOrder(planID, candidate, event.index, leg.FinalState, quantityFilled)
		exposure := sagaExposure(candidate, result.Legs, event.index)
		if err := saga.ApplyOrder(order, exposure); err != nil {
			return err
		}
	}
	return nil
}

func simulationOrder(
	planID domain.ExecutionPlanID,
	candidate Candidate,
	index int,
	state execution.OrderState,
	filled domain.Quantity,
) execution.Order {
	leg := candidate.Buy
	if index == 1 {
		leg = candidate.Sell
	}
	orderID, _ := domain.NewVirtualOrderID(
		"b5-" + candidate.ID[:16] + "-leg-" + strconv.Itoa(index+1),
	)
	return execution.Order{
		Identity: execution.OrderIdentity{
			ID: orderID, PlanID: planID, ClientOrderID: "b5-sim-" + strconv.Itoa(index+1),
			Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.TradeQuantity,
		},
		State: state, CumulativeQuantity: filled, Revision: 1,
	}
}

func sagaExposure(
	candidate Candidate,
	legs []LegSimulation,
	throughIndex int,
) []execution.Exposure {
	acquired, removed := quantity("0"), quantity("0")
	for index, leg := range legs {
		if index > throughIndex || leg.Result == nil {
			continue
		}
		if index == 0 {
			acquired = leg.Result.NetOutput
		} else {
			removed, _ = leg.Result.Input.Subtract(leg.Result.SourceDust)
		}
	}
	var difference domain.Quantity
	if acquired.Compare(removed) >= 0 {
		difference, _ = acquired.Subtract(removed)
	} else {
		difference, _ = removed.Subtract(acquired)
	}
	if difference.Compare(quantity("0")) == 0 {
		return nil
	}
	return []execution.Exposure{{
		Asset: candidate.Instrument.Base, Quantity: balance(difference.String()),
	}}
}
