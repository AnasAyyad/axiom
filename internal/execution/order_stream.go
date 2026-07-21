package execution

// ReduceOrderEvents applies one raw adapter stream and returns only events that
// changed the canonical aggregate. Duplicate and stale transport facts remain
// excluded from durable state-machine history.
func ReduceOrderEvents(identity OrderIdentity, events []OrderEvent) (Order, []OrderEvent, error) {
	reducer, err := NewOrderReducer(identity)
	if err != nil {
		return Order{}, nil, err
	}
	applied := make([]OrderEvent, 0, len(events))
	for _, event := range events {
		reduction, reduceErr := reducer.Reduce(event)
		if reduceErr != nil {
			return Order{}, nil, reduceErr
		}
		if reduction.Applied {
			applied = append(applied, cloneOrderEvent(event))
		}
	}
	return reducer.Snapshot(), applied, nil
}

func cloneOrderEvent(event OrderEvent) OrderEvent {
	event.Fees = append([]FeeFact(nil), event.Fees...)
	event.Fills = append([]FillFact(nil), event.Fills...)
	return event
}
