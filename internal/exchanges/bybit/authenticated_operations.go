package bybit

import (
	"context"
	"net/url"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func (client *SandboxClient) walletBalance(
	ctx context.Context,
) ([]byte, error) {
	return client.execute(
		ctx,
		authenticatedWalletBalance,
		url.Values{"accountType": {"UNIFIED"}},
	)
}

func (client *SandboxClient) create(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	fields, err := demoSubmissionFields(submission)
	if err != nil {
		return nil, err
	}
	return client.execute(ctx, authenticatedCreate, fields)
}

func (client *SandboxClient) cancel(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	return client.execute(
		ctx,
		authenticatedCancel,
		demoOrderIdentityFields(submission),
	)
}

func (client *SandboxClient) query(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	return client.execute(
		ctx,
		authenticatedQuery,
		demoOrderIdentityFields(submission),
	)
}

func (client *SandboxClient) orderHistory(
	ctx context.Context,
	symbol string,
	orderLinkID string,
	cursor string,
) ([]byte, error) {
	fields := url.Values{
		"category":    {"spot"},
		"limit":       {"50"},
		"orderFilter": {"Order"},
	}
	if symbol != "" {
		fields.Set("symbol", symbol)
	}
	if orderLinkID != "" {
		fields.Set("orderLinkId", orderLinkID)
	}
	if cursor != "" {
		fields.Set("cursor", cursor)
	}
	return client.execute(ctx, authenticatedOrderHistory, fields)
}

func (client *SandboxClient) executionHistory(
	ctx context.Context,
	symbol string,
	orderID string,
	orderLinkID string,
	cursor string,
) ([]byte, error) {
	fields := url.Values{
		"category": {"spot"},
		"limit":    {"100"},
	}
	if symbol != "" {
		fields.Set("symbol", symbol)
	}
	if orderID != "" {
		fields.Set("orderId", orderID)
	}
	if orderLinkID != "" {
		fields.Set("orderLinkId", orderLinkID)
	}
	if cursor != "" {
		fields.Set("cursor", cursor)
	}
	return client.execute(ctx, authenticatedExecutionHistory, fields)
}

func demoOrderIdentityFields(submission sandbox.Submission) url.Values {
	return url.Values{
		"category":    {"spot"},
		"orderFilter": {"Order"},
		"orderLinkId": {submission.ClientOrderID},
		"symbol":      {submission.Instrument.Symbol()},
	}
}

func demoSubmissionFields(
	submission sandbox.Submission,
) (url.Values, error) {
	timeInForce, err := demoTimeInForce(submission.Style)
	if err != nil ||
		(submission.Side != domain.SideBuy &&
			submission.Side != domain.SideSell) {
		return nil, errAuthenticatedPolicy
	}
	side := "Buy"
	if submission.Side == domain.SideSell {
		side = "Sell"
	}
	return url.Values{
		"category":    {"spot"},
		"isLeverage":  {"0"},
		"orderFilter": {"Order"},
		"orderLinkId": {submission.ClientOrderID},
		"orderType":   {"Limit"},
		"price":       {submission.LimitPrice.String()},
		"qty":         {submission.Quantity.String()},
		"side":        {side},
		"symbol":      {submission.Instrument.Symbol()},
		"timeInForce": {timeInForce},
	}, nil
}

func demoTimeInForce(style sandbox.OrderStyle) (string, error) {
	switch style {
	case sandbox.OrderStyleLimitGTC:
		return "GTC", nil
	case sandbox.OrderStyleLimitIOC:
		return "IOC", nil
	case sandbox.OrderStylePostOnly:
		return "PostOnly", nil
	default:
		return "", errAuthenticatedPolicy
	}
}
