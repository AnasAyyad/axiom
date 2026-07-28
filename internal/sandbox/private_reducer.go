package sandbox

import (
	"sort"

	"axiom/internal/execution"
)

// ReducePrivateOrderEvents rebuilds one order from its immutable submission
// and durable private-event history. Recovery always uses the same canonical
// reducer as simulation rather than trusting an exchange status string.
func ReducePrivateOrderEvents(
	submission Submission,
	events []execution.OrderEvent,
) (execution.Order, error) {
	reducer, err := approvedReducer(submission)
	if err != nil {
		return execution.Order{}, err
	}
	if _, err = reducer.Reduce(orderEvent(
		submission, execution.OrderSubmitting, "dispatch", 5, submission.ApprovedAt,
	)); err != nil {
		return execution.Order{}, err
	}
	ordered := append([]execution.OrderEvent(nil), events...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].Ordinal != ordered[right].Ordinal {
			return ordered[left].Ordinal < ordered[right].Ordinal
		}
		if !ordered[left].OccurredAt.Equal(ordered[right].OccurredAt) {
			return ordered[left].OccurredAt.Before(ordered[right].OccurredAt)
		}
		return ordered[left].ID < ordered[right].ID
	})
	for _, event := range ordered {
		if _, err = reducer.Reduce(event); err != nil {
			return execution.Order{}, err
		}
	}
	return reducer.Snapshot(), nil
}
