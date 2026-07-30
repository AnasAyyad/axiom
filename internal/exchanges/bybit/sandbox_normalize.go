package bybit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

// ErrDemoPayload is the generic fail-closed Demo payload error.
var ErrDemoPayload = errors.New("bybit_demo_payload_rejected")

func normalizeDemoAcknowledgement(
	body []byte,
	submission sandbox.Submission,
	state execution.OrderState,
	receivedAt time.Time,
) (sandbox.PrivateEvent, error) {
	native, occurredAt, err := decodeDemoTimed[orderAcknowledgementResult](body)
	if err != nil || native.OrderID == "" ||
		native.OrderLinkID != submission.ClientOrderID ||
		(state != execution.OrderAcknowledged &&
			state != execution.OrderCancelPending) {
		return sandbox.PrivateEvent{}, ErrDemoPayload
	}
	status := "ACK"
	if state == execution.OrderCancelPending {
		status = "CANCEL_ACK"
	}
	zero, _ := domain.ParseQuantity("0")
	orderEvent := execution.OrderEvent{
		ID:      "bybit-" + status + "-" + native.OrderID,
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: state, ExchangeStatus: status, CumulativeQuantity: zero,
		OccurredAt: occurredAt, Ordinal: uint64(occurredAt.UnixMilli()),
	}
	event := sandbox.PrivateEvent{
		Identity:  "bybit-" + status + "-" + native.OrderID,
		AccountID: submission.AccountID, AccountEpoch: submission.AccountEpoch,
		Kind: sandbox.PrivateOrderEvent, OrderID: submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: payloadHash(body), OrderEvent: &orderEvent,
		OccurredAt: occurredAt, ReceivedAt: receivedAt,
	}
	if event.Validate() != nil {
		return sandbox.PrivateEvent{}, ErrDemoPayload
	}
	return event, nil
}

func normalizeDemoOrder(
	order demoOrderPayload,
	executions []demoExecutionPayload,
	submission sandbox.Submission,
	receivedAt time.Time,
	sourceBody []byte,
) (sandbox.PrivateEvent, error) {
	if !demoOrderMatches(order, submission) {
		return sandbox.PrivateEvent{}, ErrDemoPayload
	}
	fills, fees, fillHash, latestFillAt, err := normalizeDemoExecutions(
		executions,
		order.OrderID,
		submission,
	)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	cumulative, err := domain.ParseQuantity(order.CumulativeQuantity)
	if err != nil || sumDemoFillQuantity(fills).Compare(cumulative) != 0 {
		return sandbox.PrivateEvent{}, ErrDemoPayload
	}
	occurredAt, err := demoOrderTime(order)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	if latestFillAt.After(occurredAt) {
		occurredAt = latestFillAt
	}
	orderHash := canonicalDemoHash(order)
	if len(sourceBody) != 0 {
		orderHash = payloadHash(sourceBody)
	}
	return buildNormalizedDemoOrder(
		order, submission, receivedAt, occurredAt, cumulative,
		fills, fees, fillHash, orderHash,
	)
}

func buildNormalizedDemoOrder(
	order demoOrderPayload,
	submission sandbox.Submission,
	receivedAt, occurredAt time.Time,
	cumulative domain.Quantity,
	fills []execution.FillFact,
	fees []execution.FeeFact,
	fillHash, orderHash string,
) (sandbox.PrivateEvent, error) {
	state, err := demoOrderState(order.OrderStatus)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	orderEvent := execution.OrderEvent{
		ID: fmt.Sprintf(
			"bybit-order-%s-%s-%d",
			order.OrderID,
			order.OrderStatus,
			occurredAt.UnixMilli(),
		),
		OrderID: submission.OrderID, ClientOrderID: submission.ClientOrderID,
		State: state, ExchangeStatus: order.OrderStatus,
		CumulativeQuantity: cumulative, Fees: fees, Fills: fills,
		OccurredAt: occurredAt, Ordinal: demoOrderOrdinal(order, occurredAt),
	}
	kind := sandbox.PrivateOrderEvent
	if len(fills) != 0 {
		kind = sandbox.PrivateFillEvent
	}
	event := sandbox.PrivateEvent{
		Identity: fmt.Sprintf(
			"bybit-%s-%s-%s",
			order.OrderID,
			order.OrderStatus,
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
		return sandbox.PrivateEvent{}, ErrDemoPayload
	}
	return event, nil
}

func decodeDemoTimed[T any](body []byte) (T, time.Time, error) {
	var envelope responseEnvelope[T]
	if strictDecode(body, &envelope) != nil ||
		envelope.RetCode != 0 || envelope.RetMsg != "OK" ||
		envelope.Time <= 0 {
		var zero T
		return zero, time.Time{}, ErrDemoPayload
	}
	return envelope.Result, time.UnixMilli(envelope.Time).UTC(), nil
}

func demoOrderMatches(
	native demoOrderPayload,
	submission sandbox.Submission,
) bool {
	timeInForce, err := demoTimeInForce(submission.Style)
	quantity, quantityErr := domain.ParseQuantity(native.Quantity)
	price, priceErr := domain.ParsePrice(native.Price)
	return err == nil && quantityErr == nil && priceErr == nil &&
		native.Category == "spot" &&
		native.Symbol == submission.Instrument.Symbol() &&
		native.OrderID != "" &&
		native.OrderLinkID == submission.ClientOrderID &&
		native.Side == demoSide(submission.Side) &&
		native.OrderType == "Limit" && native.TimeInForce == timeInForce &&
		native.IsLeverage == "0" && native.PositionIndex == 0 &&
		(native.OrderFilter == "" || native.OrderFilter == "Order") &&
		!native.ReduceOnly && !native.CloseOnTrigger &&
		native.ParentOrderLinkID == "" && native.BlockTradeID == "" &&
		(native.StopOrderType == "" || native.StopOrderType == "UNKNOWN") &&
		zeroOrEmpty(native.TriggerPrice) &&
		zeroOrEmpty(native.ActivationPrice) &&
		zeroOrEmpty(native.TrailingPercentage) &&
		zeroOrEmpty(native.TrailingValue) &&
		zeroOrEmpty(native.TakeProfit) && zeroOrEmpty(native.StopLoss) &&
		zeroOrEmpty(native.TPLimitPrice) && zeroOrEmpty(native.SLLimitPrice) &&
		(native.TPTriggerBy == "" || native.TPTriggerBy == "UNKNOWN") &&
		(native.SLTriggerBy == "" || native.SLTriggerBy == "UNKNOWN") &&
		(native.TriggerBy == "" || native.TriggerBy == "UNKNOWN") &&
		native.TriggerDirection == 0 &&
		(native.TPSLMode == "" || native.TPSLMode == "UNKNOWN") &&
		(native.OCOTriggerBy == "" ||
			native.OCOTriggerBy == "OcoTriggerByUnknown") &&
		(native.SlippageType == "" || native.SlippageType == "UNKNOWN") &&
		zeroOrEmpty(native.SlippageTolerance) &&
		(native.MarketUnit == "" || native.MarketUnit == "baseCoin") &&
		native.OrderIV == "" && native.PlaceType == "" &&
		native.SMPGroup == 0 && native.SMPOrderID == "" &&
		(native.SMPType == "" || native.SMPType == "None") &&
		!native.RPITakerAccess && zeroOrEmpty(native.RPIMatchedQuantity) &&
		quantity.Compare(submission.Quantity) == 0 &&
		price.Compare(submission.LimitPrice) == 0
}

func demoSide(side domain.Side) string {
	if side == domain.SideBuy {
		return "Buy"
	}
	if side == domain.SideSell {
		return "Sell"
	}
	return ""
}

func demoOrderState(status string) (execution.OrderState, error) {
	switch status {
	case "New", "Created", "Untriggered":
		return execution.OrderAcknowledged, nil
	case "PartiallyFilled":
		return execution.OrderPartiallyFilled, nil
	case "Filled":
		return execution.OrderFilled, nil
	case "PendingCancel":
		return execution.OrderCancelPending, nil
	case "Cancelled", "Deactivated":
		return execution.OrderCanceled, nil
	case "Rejected":
		return execution.OrderRejected, nil
	case "PartiallyFilledCanceled":
		return execution.OrderCanceled, nil
	default:
		return "", ErrDemoPayload
	}
}

func demoOrderTime(order demoOrderPayload) (time.Time, error) {
	value := order.UpdatedTime
	if value == "" {
		value = order.CreatedTime
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, ErrDemoPayload
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func demoOrderOrdinal(order demoOrderPayload, occurredAt time.Time) uint64 {
	value := occurredAt.UnixMilli()
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

func canonicalDemoHash(value any) string {
	encoded, _ := json.Marshal(value)
	return payloadHash(encoded)
}
