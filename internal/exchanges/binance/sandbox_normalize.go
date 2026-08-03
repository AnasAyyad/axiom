package binance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

// ErrSandboxPayload is the generic fail-closed sandbox payload error.
var ErrSandboxPayload = errors.New("binance_testnet_payload_rejected")

var (
	errSandboxOrderDecode        = errors.New("binance_order_decode")
	errSandboxOrderMismatch      = errors.New("binance_order_mismatch")
	errSandboxFillDecode         = errors.New("binance_fill_decode")
	errSandboxFillIdentity       = errors.New("binance_fill_identity")
	errSandboxFillValue          = errors.New("binance_fill_value")
	errSandboxFillAggregate      = errors.New("binance_fill_aggregate")
	errSandboxCumulativeDecode   = errors.New("binance_cumulative_decode")
	errSandboxCumulativeMismatch = errors.New("binance_cumulative_mismatch")
	errSandboxOrderState         = errors.New("binance_order_state")
	errSandboxOrderEvent         = errors.New("binance_order_event")
)

// SandboxPayloadFailureCode returns only one closed, non-payload diagnostic
// category. It never includes exchange response values.
func SandboxPayloadFailureCode(err error) string {
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{errSandboxOrderDecode, "order_decode"},
		{errSandboxOrderMismatch, "order_mismatch"},
		{errSandboxFillDecode, "fill_decode"},
		{errSandboxFillIdentity, "fill_identity"},
		{errSandboxFillValue, "fill_value"},
		{errSandboxFillAggregate, "fill_aggregate"},
		{errSandboxCumulativeDecode, "cumulative_decode"},
		{errSandboxCumulativeMismatch, "cumulative_mismatch"},
		{errSandboxOrderState, "order_state"},
		{errSandboxOrderEvent, "order_event"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.code
		}
	}
	return ""
}

// SandboxFailureCode extends the payload categories with the adapter's closed
// transport and policy errors. It never returns an external error string.
func SandboxFailureCode(err error) string {
	if code := SandboxPayloadFailureCode(err); code != "" {
		return code
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{ErrSandboxStartupIdentity, "startup_identity"},
		{ErrSandboxRequest, "request"},
		{ErrSandboxAmbiguous, "ambiguous"},
		{ErrSandboxRejected, "rejected"},
		{ErrSandboxTimestamp, "timestamp"},
		{ErrSandboxRateLimited, "rate_limited"},
		{ErrSandboxOrderNotFound, "order_not_found"},
		{ErrSandboxRateBudget, "rate_budget"},
		{ErrSandboxPrivateEvent, "private_event"},
		{ErrSandboxFilter, "filter"},
		{ErrSandboxResetDetected, "reset_detected"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.code
		}
	}
	return ""
}

func sandboxPayloadFailure(category error) error {
	return errors.Join(ErrSandboxPayload, category)
}

type sandboxAccountPayload struct {
	MakerCommission            int64                    `json:"makerCommission"`
	TakerCommission            int64                    `json:"takerCommission"`
	BuyerCommission            int64                    `json:"buyerCommission"`
	SellerCommission           int64                    `json:"sellerCommission"`
	CommissionRates            sandboxCommissionPayload `json:"commissionRates"`
	CanTrade                   bool                     `json:"canTrade"`
	CanWithdraw                bool                     `json:"canWithdraw"`
	CanDeposit                 bool                     `json:"canDeposit"`
	Brokered                   bool                     `json:"brokered"`
	RequireSelfTradePrevention bool                     `json:"requireSelfTradePrevention"`
	PreventSOR                 bool                     `json:"preventSor"`
	UpdateTime                 int64                    `json:"updateTime"`
	AccountType                string                   `json:"accountType"`
	Balances                   []sandboxBalancePayload  `json:"balances"`
	Permissions                []string                 `json:"permissions"`
	UID                        json.Number              `json:"uid"`
}

type sandboxCommissionPayload struct {
	Maker  string `json:"maker"`
	Taker  string `json:"taker"`
	Buyer  string `json:"buyer"`
	Seller string `json:"seller"`
}

type sandboxBalancePayload struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

type sandboxOrderAcknowledgement struct {
	Symbol        string      `json:"symbol"`
	OrderID       json.Number `json:"orderId"`
	OrderListID   json.Number `json:"orderListId"`
	ClientOrderID string      `json:"clientOrderId"`
	TransactionAt int64       `json:"transactTime"`
}

type sandboxOrderPayload struct {
	Symbol                     string      `json:"symbol"`
	OrderID                    json.Number `json:"orderId"`
	OrderListID                json.Number `json:"orderListId"`
	ClientOrderID              string      `json:"clientOrderId"`
	Price                      string      `json:"price"`
	OriginalQuantity           string      `json:"origQty"`
	ExecutedQuantity           string      `json:"executedQty"`
	CumulativeQuoteQuantity    string      `json:"cummulativeQuoteQty"`
	Status                     string      `json:"status"`
	TimeInForce                string      `json:"timeInForce"`
	Type                       string      `json:"type"`
	Side                       string      `json:"side"`
	StopPrice                  string      `json:"stopPrice"`
	IcebergQuantity            string      `json:"icebergQty"`
	Time                       int64       `json:"time"`
	UpdateTime                 int64       `json:"updateTime"`
	IsWorking                  bool        `json:"isWorking"`
	WorkingTime                int64       `json:"workingTime"`
	OriginalQuoteOrderQuantity string      `json:"origQuoteOrderQty"`
	SelfTradePreventionMode    string      `json:"selfTradePreventionMode"`
	PreventedMatchID           json.Number `json:"preventedMatchId"`
	PreventedQuantity          string      `json:"preventedQuantity"`
	TrailingDelta              uint64      `json:"trailingDelta"`
	TrailingTime               int64       `json:"trailingTime"`
	StrategyID                 json.Number `json:"strategyId"`
	StrategyType               uint64      `json:"strategyType"`
	PegPriceType               string      `json:"pegPriceType"`
	PegOffsetType              string      `json:"pegOffsetType"`
	PegOffsetValue             uint64      `json:"pegOffsetValue"`
	PeggedPrice                string      `json:"peggedPrice"`
	UsedSOR                    bool        `json:"usedSor"`
	WorkingFloor               string      `json:"workingFloor"`
}

type sandboxFillPayload struct {
	Symbol          string      `json:"symbol"`
	ID              json.Number `json:"id"`
	OrderID         json.Number `json:"orderId"`
	OrderListID     json.Number `json:"orderListId"`
	Price           string      `json:"price"`
	Quantity        string      `json:"qty"`
	QuoteQuantity   string      `json:"quoteQty"`
	Commission      string      `json:"commission"`
	CommissionAsset string      `json:"commissionAsset"`
	Time            int64       `json:"time"`
	IsBuyer         bool        `json:"isBuyer"`
	IsMaker         bool        `json:"isMaker"`
	IsBestMatch     bool        `json:"isBestMatch"`
}

func normalizeSandboxAcknowledgement(
	body []byte,
	submission sandbox.Submission,
	receivedAt time.Time,
) (sandbox.PrivateEvent, error) {
	var native sandboxOrderAcknowledgement
	if strictDecode(body, &native) != nil || native.Symbol != submission.Instrument.Symbol() ||
		native.ClientOrderID != submission.ClientOrderID || native.OrderID.String() == "" ||
		native.TransactionAt <= 0 {
		return sandbox.PrivateEvent{}, ErrSandboxPayload
	}
	occurredAt := time.UnixMilli(native.TransactionAt).UTC()
	zero, _ := domain.ParseQuantity("0")
	orderEvent := execution.OrderEvent{
		ID:      "binance-ack-" + native.OrderID.String(),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderAcknowledged, ExchangeStatus: "ACK",
		CumulativeQuantity: zero, OccurredAt: occurredAt,
		Ordinal: uint64(native.TransactionAt),
	}
	event := sandbox.PrivateEvent{
		Identity:  "binance-ack-" + native.OrderID.String(),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID: submission.ClientOrderID, NativeOrderHash: hashBytes(body),
		OrderEvent: &orderEvent, OccurredAt: occurredAt, ReceivedAt: receivedAt,
	}
	if event.Validate() != nil {
		return sandbox.PrivateEvent{}, ErrSandboxPayload
	}
	return event, nil
}

func normalizeSandboxOrder(
	orderBody []byte,
	fillsBody []byte,
	submission sandbox.Submission,
	receivedAt time.Time,
) (sandbox.PrivateEvent, error) {
	var native sandboxOrderPayload
	if strictDecode(orderBody, &native) != nil {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxOrderDecode)
	}
	if !sandboxOrderMatches(native, submission) {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxOrderMismatch)
	}
	fills, feeFacts, fillHash, latestFillAt, err := normalizeSandboxFills(
		fillsBody,
		native.OrderID.String(),
		submission,
	)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	cumulative, err := domain.ParseQuantity(native.ExecutedQuantity)
	if err != nil {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxCumulativeDecode)
	}
	if sumFillQuantity(fills).Compare(cumulative) != 0 {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxCumulativeMismatch)
	}
	occurredAt := sandboxOrderTime(native)
	if latestFillAt.After(occurredAt) {
		occurredAt = latestFillAt
	}
	return buildNormalizedSandboxOrder(
		native, submission, receivedAt, occurredAt,
		cumulative, fills, feeFacts, fillHash, hashBytes(orderBody),
	)
}

func buildNormalizedSandboxOrder(
	native sandboxOrderPayload,
	submission sandbox.Submission,
	receivedAt, occurredAt time.Time,
	cumulative domain.Quantity,
	fills []execution.FillFact,
	feeFacts []execution.FeeFact,
	fillHash, orderHash string,
) (sandbox.PrivateEvent, error) {
	state, err := sandboxOrderState(native.Status)
	if err != nil {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxOrderState)
	}
	orderEvent := execution.OrderEvent{
		ID: fmt.Sprintf(
			"binance-order-%s-%s-%d",
			native.OrderID.String(),
			native.Status,
			occurredAt.UnixMilli(),
		),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: state, ExchangeStatus: native.Status,
		CumulativeQuantity: cumulative, Fees: feeFacts, Fills: fills,
		OccurredAt: occurredAt, Ordinal: uint64(occurredAt.UnixMilli()),
	}
	kind := normalizedSandboxEventKind(fills)
	event := sandbox.PrivateEvent{
		Identity: fmt.Sprintf(
			"binance-%s-%s-%s",
			native.OrderID.String(),
			native.Status,
			orderHash[:16],
		),
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: kind, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: orderHash, OrderEvent: &orderEvent,
		OccurredAt: occurredAt, ReceivedAt: receivedAt,
	}
	if kind == sandbox.PrivateFillEvent {
		event.NativeFillHash = fillHash
	}
	if event.Validate() != nil {
		return sandbox.PrivateEvent{},
			sandboxPayloadFailure(errSandboxOrderEvent)
	}
	return event, nil
}

func sandboxOrderMatches(
	native sandboxOrderPayload,
	submission sandbox.Submission,
) bool {
	orderType, timeInForce, err := binanceOrderStyle(submission.Style)
	if submission.Style == sandbox.OrderStylePostOnly {
		timeInForce = "GTC"
	}
	quantity, quantityErr := domain.ParseQuantity(native.OriginalQuantity)
	price, priceErr := domain.ParsePrice(native.Price)
	return err == nil && quantityErr == nil && priceErr == nil &&
		native.Symbol == submission.Instrument.Symbol() &&
		native.ClientOrderID == submission.ClientOrderID &&
		native.OrderID.String() != "" &&
		native.Side == binanceSide(submission.Side) &&
		native.Type == orderType && native.TimeInForce == timeInForce &&
		quantity.Compare(submission.Quantity) == 0 &&
		price.Compare(submission.LimitPrice) == 0
}

func sandboxOrderState(status string) (execution.OrderState, error) {
	switch status {
	case "NEW", "PENDING_NEW":
		return execution.OrderAcknowledged, nil
	case "PARTIALLY_FILLED":
		return execution.OrderPartiallyFilled, nil
	case "FILLED":
		return execution.OrderFilled, nil
	case "PENDING_CANCEL":
		return execution.OrderCancelPending, nil
	case "CANCELED":
		return execution.OrderCanceled, nil
	case "REJECTED":
		return execution.OrderRejected, nil
	case "EXPIRED", "EXPIRED_IN_MATCH":
		return execution.OrderExpired, nil
	default:
		return "", ErrSandboxPayload
	}
}

func sandboxOrderTime(native sandboxOrderPayload) time.Time {
	value := native.UpdateTime
	if value <= 0 {
		value = native.Time
	}
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}

func sumFillQuantity(fills []execution.FillFact) domain.Quantity {
	total, _ := domain.ParseQuantity("0")
	for _, fill := range fills {
		total, _ = total.Add(fill.Quantity)
	}
	return total
}

func rejectedSandboxEvent(
	submission sandbox.Submission,
	receivedAt time.Time,
	reason string,
) sandbox.PrivateEvent {
	zero, _ := domain.ParseQuantity("0")
	nativeHash := canonicalHash([]string{
		"binance", submission.ClientOrderID, submission.RequestHash, reason,
	})
	orderEvent := execution.OrderEvent{
		ID:      "binance-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: execution.OrderRejected, ExchangeStatus: "REJECTED",
		CumulativeQuantity: zero, OccurredAt: receivedAt,
		Ordinal: uint64(receivedAt.UnixMilli()),
	}
	return sandbox.PrivateEvent{
		Identity:  "binance-rejected-" + submission.ClientOrderID + "-" + nativeHash[:12],
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID: submission.ClientOrderID, NativeOrderHash: nativeHash,
		OrderEvent: &orderEvent, OccurredAt: receivedAt, ReceivedAt: receivedAt,
	}
}

func canonicalHash(value any) string {
	encoded, _ := json.Marshal(value)
	return hashBytes(encoded)
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
