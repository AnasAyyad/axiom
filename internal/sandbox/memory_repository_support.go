package sandbox

import (
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func (repository *memoryDispatcherRepository) claimed(
	id string,
	fence uint64,
) (SubmissionOutbox, error) {
	record, exists := repository.outbox[id]
	if !exists || record.State != OutboxClaimed || record.FencingToken != fence {
		return SubmissionOutbox{}, contractError("stale_fencing_token")
	}
	return record, nil
}

func (repository *memoryDispatcherRepository) openCapacity() (map[AccountID]int, int) {
	accounts := make(map[AccountID]int)
	global := 0
	for _, record := range repository.outbox {
		if record.State != OutboxTerminal {
			accounts[record.Submission.AccountID]++
			global++
		}
	}
	return accounts, global
}

func approvedReducer(submission Submission) (*execution.OrderReducer, error) {
	reducer, err := execution.NewOrderReducer(execution.OrderIdentity{
		ID: submission.OrderID, PlanID: submission.PlanID, ClientOrderID: submission.ClientOrderID,
		Instrument: submission.Instrument, Side: submission.Side, Quantity: submission.Quantity,
	})
	if err != nil {
		return nil, err
	}
	for ordinal, state := range []execution.OrderState{
		execution.OrderValidating, execution.OrderReserved, execution.OrderApproved,
	} {
		if _, err := reducer.Reduce(orderEvent(
			submission, state, "approval", uint64(ordinal+2), submission.ApprovedAt,
		)); err != nil {
			return nil, err
		}
	}
	return reducer, nil
}

func orderEvent(
	submission Submission,
	state execution.OrderState,
	status string,
	ordinal uint64,
	at time.Time,
) execution.OrderEvent {
	zero, _ := domain.ParseQuantity("0")
	return execution.OrderEvent{
		ID:      fmt.Sprintf("%s-%02d-%s", submission.ClientOrderID, ordinal, state),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: state, ExchangeStatus: status, CumulativeQuantity: zero,
		OccurredAt: at, Ordinal: ordinal,
	}
}

func clientIdentity(submission Submission) string {
	return fmt.Sprintf(
		"%s|%d|%s",
		submission.AccountID,
		submission.AccountEpoch,
		submission.ClientOrderID,
	)
}

func exchangeForAccount(account AccountID) Exchange {
	if len(account) >= len("binance") && string(account[:len("binance")]) == "binance" {
		return ExchangeBinance
	}
	if len(account) >= len("bybit") && string(account[:len("bybit")]) == "bybit" {
		return ExchangeBybit
	}
	return ""
}

func terminalState(state execution.OrderState) bool {
	return state == execution.OrderFilled
}

func requiresTerminalReconciliation(state execution.OrderState) bool {
	switch state {
	case execution.OrderCanceled, execution.OrderRejected, execution.OrderExpired:
		return true
	default:
		return false
	}
}
