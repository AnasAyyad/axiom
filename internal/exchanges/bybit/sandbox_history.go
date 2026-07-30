package bybit

import (
	"context"

	"axiom/internal/sandbox"
)

func (adapter *SandboxAdapter) completeOrderHistory(
	ctx context.Context,
	symbol string,
) ([]demoOrderPayload, error) {
	result := make([]demoOrderPayload, 0)
	cursor := ""
	for page := 0; page < demoHistoryPageLimit; page++ {
		body, err := adapter.client.orderHistory(ctx, symbol, "", cursor)
		if err != nil {
			return nil, err
		}
		native, err := decodeDemoResult[orderListResult](body)
		if err != nil || !bindDemoOrderCategories(&native) {
			return nil, ErrDemoPayload
		}
		result = append(result, native.List...)
		if native.NextPageCursor == "" {
			return result, nil
		}
		if native.NextPageCursor == cursor {
			return nil, ErrDemoPayload
		}
		cursor = native.NextPageCursor
	}
	return nil, ErrDemoPayload
}

func (adapter *SandboxAdapter) exactOrderHistory(
	ctx context.Context,
	submission sandbox.Submission,
) (demoOrderPayload, []byte, error) {
	body, err := adapter.client.orderHistory(
		ctx,
		submission.Instrument.Symbol(),
		submission.ClientOrderID,
		"",
	)
	if err != nil {
		return demoOrderPayload{}, nil, err
	}
	native, err := decodeDemoResult[orderListResult](body)
	if err != nil || !bindDemoOrderCategories(&native) ||
		len(native.List) != 1 {
		return demoOrderPayload{}, nil, ErrDemoPayload
	}
	return native.List[0], body, nil
}

func (adapter *SandboxAdapter) completeExecutionHistory(
	ctx context.Context,
	symbol string,
	orderID string,
	orderLinkID string,
) ([]demoExecutionPayload, error) {
	result := make([]demoExecutionPayload, 0)
	cursor := ""
	for page := 0; page < demoHistoryPageLimit; page++ {
		body, err := adapter.client.executionHistory(
			ctx,
			symbol,
			orderID,
			orderLinkID,
			cursor,
		)
		if err != nil {
			return nil, err
		}
		native, err := decodeDemoResult[executionListResult](body)
		if err != nil || !bindDemoExecutionCategories(&native) {
			return nil, ErrDemoPayload
		}
		result = append(result, native.List...)
		if native.NextPageCursor == "" {
			return result, nil
		}
		if native.NextPageCursor == cursor {
			return nil, ErrDemoPayload
		}
		cursor = native.NextPageCursor
	}
	return nil, ErrDemoPayload
}
