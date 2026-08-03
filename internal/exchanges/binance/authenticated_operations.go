package binance

import (
	"context"
	"net/url"
	"strconv"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
)

func authenticatedRouteName(route authenticatedRoute) string {
	switch route {
	case authenticatedAccount:
		return "account"
	case authenticatedOpenOrders:
		return "open_orders"
	case authenticatedOrderHistory:
		return "order_history"
	case authenticatedFills:
		return "fills"
	case authenticatedTestCreate:
		return "test_create"
	case authenticatedCreate:
		return "create"
	case authenticatedQuery:
		return "query"
	case authenticatedCancel:
		return "cancel"
	default:
		return "unknown"
	}
}

func (client *SandboxClient) account(ctx context.Context) ([]byte, error) {
	return client.execute(ctx, authenticatedAccount, client.commonFields())
}

func (client *SandboxClient) openOrders(
	ctx context.Context,
	symbol string,
) ([]byte, error) {
	fields := client.commonFields()
	if symbol != "" {
		fields.Set("symbol", symbol)
	}
	return client.execute(ctx, authenticatedOpenOrders, fields)
}

func (client *SandboxClient) orderHistory(
	ctx context.Context,
	symbol string,
) ([]byte, error) {
	fields := client.commonFields()
	fields.Set("symbol", symbol)
	fields.Set("limit", "1000")
	return client.execute(ctx, authenticatedOrderHistory, fields)
}

func (client *SandboxClient) orderHistoryFrom(
	ctx context.Context,
	symbol string,
	orderID uint64,
) ([]byte, error) {
	fields := client.commonFields()
	fields.Set("symbol", symbol)
	fields.Set("limit", "1000")
	fields.Set("orderId", strconv.FormatUint(orderID, 10))
	return client.execute(ctx, authenticatedOrderHistory, fields)
}

func (client *SandboxClient) fills(
	ctx context.Context,
	symbol string,
) ([]byte, error) {
	fields := client.commonFields()
	fields.Set("symbol", symbol)
	fields.Set("limit", "1000")
	return client.execute(ctx, authenticatedFills, fields)
}

func (client *SandboxClient) fillsFrom(
	ctx context.Context,
	symbol string,
	fillID uint64,
) ([]byte, error) {
	fields := client.commonFields()
	fields.Set("symbol", symbol)
	fields.Set("limit", "1000")
	fields.Set("fromId", strconv.FormatUint(fillID, 10))
	return client.execute(ctx, authenticatedFills, fields)
}

func (client *SandboxClient) fillsForOrder(
	ctx context.Context,
	submission sandbox.Submission,
	orderID string,
) ([]byte, error) {
	fields := client.commonFields()
	fields.Set("symbol", submission.Instrument.Symbol())
	fields.Set("orderId", orderID)
	fields.Set("limit", "1000")
	return client.execute(ctx, authenticatedFills, fields)
}

func (client *SandboxClient) testCreate(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	fields, err := submissionFields(submission)
	if err != nil {
		return nil, err
	}
	client.addRequestTime(fields)
	return client.execute(ctx, authenticatedTestCreate, fields)
}

func (client *SandboxClient) create(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	fields, err := submissionFields(submission)
	if err != nil {
		return nil, err
	}
	client.addRequestTime(fields)
	return client.execute(ctx, authenticatedCreate, fields)
}

func (client *SandboxClient) query(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	fields := client.orderIdentityFields(submission)
	return client.execute(ctx, authenticatedQuery, fields)
}

func (client *SandboxClient) cancel(
	ctx context.Context,
	submission sandbox.Submission,
) ([]byte, error) {
	fields := client.orderIdentityFields(submission)
	return client.execute(ctx, authenticatedCancel, fields)
}

func (client *SandboxClient) commonFields() url.Values {
	fields := make(url.Values, 2)
	client.addRequestTime(fields)
	return fields
}

func (client *SandboxClient) addRequestTime(fields url.Values) {
	fields.Set("recvWindow", sandboxReceiveWindow)
	health := client.clock.Health()
	timestamp := client.now().UTC().
		Add(health.Offset).
		Add(-health.Uncertainty).
		Add(-sandboxRESTTimeGuard).
		UnixMilli()
	fields.Set("timestamp", strconv.FormatInt(timestamp, 10))
}

func (client *SandboxClient) orderIdentityFields(
	submission sandbox.Submission,
) url.Values {
	fields := client.commonFields()
	fields.Set("origClientOrderId", submission.ClientOrderID)
	fields.Set("symbol", submission.Instrument.Symbol())
	return fields
}

func submissionFields(submission sandbox.Submission) (url.Values, error) {
	orderType, timeInForce, err := binanceOrderStyle(submission.Style)
	if err != nil || (submission.Side != domain.SideBuy && submission.Side != domain.SideSell) {
		return nil, errAuthenticatedPolicy
	}
	fields := url.Values{
		"newClientOrderId": {submission.ClientOrderID},
		"newOrderRespType": {"ACK"},
		"price":            {submission.LimitPrice.String()},
		"quantity":         {submission.Quantity.String()},
		"side":             {binanceSide(submission.Side)},
		"symbol":           {submission.Instrument.Symbol()},
		"type":             {orderType},
	}
	if timeInForce != "" {
		fields.Set("timeInForce", timeInForce)
	}
	return fields, nil
}

func binanceOrderStyle(style sandbox.OrderStyle) (string, string, error) {
	switch style {
	case sandbox.OrderStyleLimitGTC:
		return "LIMIT", "GTC", nil
	case sandbox.OrderStyleLimitIOC:
		return "LIMIT", "IOC", nil
	case sandbox.OrderStylePostOnly:
		return "LIMIT_MAKER", "", nil
	default:
		return "", "", errAuthenticatedPolicy
	}
}

func binanceSide(side domain.Side) string {
	if side == domain.SideBuy {
		return "BUY"
	}
	return "SELL"
}
