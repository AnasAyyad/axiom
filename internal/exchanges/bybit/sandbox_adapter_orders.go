package bybit

import (
	"context"
	"errors"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

func (adapter *SandboxAdapter) checkDemoSubmission(
	ctx context.Context,
	submission sandbox.Submission,
) (string, error) {
	if err := adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		10,
		demoRequestEntry,
	); err != nil {
		return "rate_budget", nil
	}
	rules, exists := adapter.rules[submission.Instrument.Symbol()]
	if !exists {
		return "instrument_filter", nil
	}
	owned, err := adapter.availableBalance(ctx, submission.Instrument.Base)
	if err != nil {
		// A failed preflight account read occurs before create construction and
		// cannot be an ambiguous submission. Reduce it as a deterministic local
		// rejection so recovery never queries a provider order that cannot
		// exist.
		return "account_unavailable", nil
	}
	if rules.validateSubmission(submission, owned) != nil {
		return "instrument_filter", nil
	}
	return "", nil
}

// Query resolves one deterministic client order ID from authoritative Demo
// order and execution history.
func (adapter *SandboxAdapter) Query(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) ([]sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return nil, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		4,
		demoRequestReconcile,
	); err != nil {
		return nil, err
	}
	body, err := adapter.client.query(ctx, submission)
	if errors.Is(err, ErrDemoOrderNotFound) {
		return adapter.normalizeDemoHistoryQuery(ctx, submission)
	}
	if err != nil {
		return nil, err
	}
	return adapter.normalizeDemoQuery(ctx, submission, body)
}

func (adapter *SandboxAdapter) normalizeDemoQuery(
	ctx context.Context,
	submission sandbox.Submission,
	body []byte,
) ([]sandbox.PrivateEvent, error) {
	result, err := decodeDemoResult[orderListResult](body)
	if err != nil || !bindDemoOrderCategories(&result) ||
		len(result.List) > 1 {
		return nil, ErrDemoPayload
	}
	if len(result.List) == 0 {
		return adapter.normalizeDemoHistoryQuery(ctx, submission)
	}
	return adapter.normalizeDemoQueryOrder(
		ctx, submission, result.List[0], body,
	)
}

func (adapter *SandboxAdapter) normalizeDemoHistoryQuery(
	ctx context.Context,
	submission sandbox.Submission,
) ([]sandbox.PrivateEvent, error) {
	order, body, err := adapter.exactOrderHistory(ctx, submission)
	if err != nil {
		return nil, err
	}
	return adapter.normalizeDemoQueryOrder(ctx, submission, order, body)
}

func (adapter *SandboxAdapter) normalizeDemoQueryOrder(
	ctx context.Context,
	submission sandbox.Submission,
	order demoOrderPayload,
	body []byte,
) ([]sandbox.PrivateEvent, error) {
	executions, err := adapter.completeExecutionHistory(
		ctx,
		submission.Instrument.Symbol(),
		order.OrderID,
		submission.ClientOrderID,
	)
	if err != nil {
		return nil, err
	}
	event, err := normalizeDemoOrder(
		order,
		executions,
		submission,
		adapter.client.now().UTC(),
		body,
	)
	if err != nil {
		return nil, err
	}
	return []sandbox.PrivateEvent{event}, nil
}

// Cancel requests cancellation without depending on entry enablement.
func (adapter *SandboxAdapter) Cancel(
	ctx context.Context,
	account sandbox.AccountID,
	epoch uint64,
	clientOrderID string,
) (sandbox.PrivateEvent, error) {
	submission, found, err := adapter.resolveSubmission(
		ctx,
		account,
		epoch,
		clientOrderID,
	)
	if err != nil || !found {
		return sandbox.PrivateEvent{}, err
	}
	if err = adapter.rateBudget.acquire(
		adapter.client.now().UTC(),
		2,
		demoRequestCancel,
	); err != nil {
		return sandbox.PrivateEvent{}, err
	}
	body, err := adapter.client.cancel(ctx, submission)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	return normalizeDemoAcknowledgement(
		body,
		submission,
		execution.OrderCancelPending,
		adapter.client.now().UTC(),
	)
}

func rejectedDemoEvent(
	submission sandbox.Submission,
	receivedAt time.Time,
	reason string,
) sandbox.PrivateEvent {
	zero, _ := domain.ParseQuantity("0")
	nativeHash := canonicalDemoHash([]string{
		"bybit", submission.ClientOrderID, submission.RequestHash, reason,
	})
	orderEvent := execution.OrderEvent{
		ID:      "bybit-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderRejected, ExchangeStatus: "REJECTED",
		CumulativeQuantity: zero, OccurredAt: receivedAt,
		Ordinal: uint64(receivedAt.UnixMilli()),
	}
	return sandbox.PrivateEvent{
		Identity:  "bybit-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: nativeHash, OrderEvent: &orderEvent,
		OccurredAt: receivedAt, ReceivedAt: receivedAt,
	}
}
