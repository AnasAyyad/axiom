package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/sandbox"
)

// ErrSandboxPrivateEvent is the generic fail-closed private-stream boundary
// error.
var ErrSandboxPrivateEvent = errors.New("binance_testnet_private_event_rejected")

type privateEventDecoder struct {
	account sandbox.AccountID
	epoch   uint64
	lookup  sandbox.SubmissionLookup
	now     func() time.Time
}

type decodedPrivateEvent struct {
	event         sandbox.PrivateEvent
	submission    sandbox.Submission
	needsBackfill bool
	sourceHash    string
}

type privateEventEnvelope struct {
	SubscriptionID *uint64         `json:"subscriptionId"`
	Event          json.RawMessage `json:"event"`
}

type privateBalancePayload struct {
	EventType     string                   `json:"e"`
	EventTime     int64                    `json:"E"`
	LastAccountAt int64                    `json:"u"`
	Balances      []privateBalancePosition `json:"B"`
}

type privateBalancePosition struct {
	Asset  string `json:"a"`
	Free   string `json:"f"`
	Locked string `json:"l"`
}

type canonicalPrivateBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

type privateExecutionPayload struct {
	EventType                  string      `json:"e"`
	EventTime                  int64       `json:"E"`
	Symbol                     string      `json:"s"`
	ClientOrderID              string      `json:"c"`
	Side                       string      `json:"S"`
	OrderType                  string      `json:"o"`
	TimeInForce                string      `json:"f"`
	OriginalQuantity           string      `json:"q"`
	OriginalPrice              string      `json:"p"`
	StopPrice                  string      `json:"P"`
	IcebergQuantity            string      `json:"F"`
	OrderListID                json.Number `json:"g"`
	OriginalClientOrderID      string      `json:"C"`
	CurrentExecutionType       string      `json:"x"`
	CurrentOrderStatus         string      `json:"X"`
	OrderRejectReason          string      `json:"r"`
	OrderID                    json.Number `json:"i"`
	LastExecutedQuantity       string      `json:"l"`
	CumulativeExecutedQuantity string      `json:"z"`
	LastExecutedPrice          string      `json:"L"`
	Commission                 string      `json:"n"`
	CommissionAsset            string      `json:"N"`
	TransactionTime            int64       `json:"T"`
	TradeID                    json.Number `json:"t"`
	ExecutionID                json.Number `json:"I"`
	IsOnBook                   bool        `json:"w"`
	IsMaker                    bool        `json:"m"`
	Ignore                     bool        `json:"M"`
	OrderCreationTime          int64       `json:"O"`
	CumulativeQuoteQuantity    string      `json:"Z"`
	LastQuoteQuantity          string      `json:"Y"`
	QuoteOrderQuantity         string      `json:"Q"`
	WorkingTime                int64       `json:"W"`
	SelfTradePreventionMode    string      `json:"V"`
	TrailingDelta              uint64      `json:"d"`
	TrailingTime               int64       `json:"D"`
	StrategyID                 json.Number `json:"j"`
	StrategyType               uint64      `json:"J"`
	PreventedMatchID           json.Number `json:"v"`
	PreventedQuantity          string      `json:"A"`
	LastPreventedQuantity      string      `json:"B"`
	TradeGroupID               json.Number `json:"u"`
	CounterOrderID             json.Number `json:"U"`
	PreventedExecutionQuantity string      `json:"pl"`
	PreventedExecutionPrice    string      `json:"pL"`
	PreventedExecutionQuote    string      `json:"pY"`
	CounterSymbol              string      `json:"Cs"`
	WorkingFloor               string      `json:"k"`
	UsedSOR                    bool        `json:"uS"`
}

func newPrivateEventDecoder(
	account sandbox.AccountID,
	epoch uint64,
	lookup sandbox.SubmissionLookup,
	now func() time.Time,
) (*privateEventDecoder, error) {
	if account == "" || epoch == 0 || lookup == nil || now == nil {
		return nil, ErrSandboxPrivateEvent
	}
	return &privateEventDecoder{
		account: account,
		epoch:   epoch,
		lookup:  lookup,
		now:     now,
	}, nil
}

func (decoder *privateEventDecoder) decode(
	ctx context.Context,
	body []byte,
) (decodedPrivateEvent, error) {
	var envelope privateEventEnvelope
	if strictDecode(body, &envelope) != nil ||
		envelope.SubscriptionID == nil || len(envelope.Event) == 0 {
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
	kind, err := exactPrivateEventType(envelope.Event)
	if err != nil {
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
	switch kind {
	case "outboundAccountPosition":
		event, err := decoder.decodeBalance(envelope, body)
		return decodedPrivateEvent{event: event, sourceHash: hashBytes(body)}, err
	case "executionReport":
		return decoder.decodeExecution(ctx, envelope, body)
	default:
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
}

func exactPrivateEventType(body []byte) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return "", ErrSandboxPrivateEvent
	}
	raw, exists := fields["e"]
	if !exists {
		return "", ErrSandboxPrivateEvent
	}
	var eventType string
	if json.Unmarshal(raw, &eventType) != nil || eventType == "" {
		return "", ErrSandboxPrivateEvent
	}
	return eventType, nil
}

func (decoder *privateEventDecoder) decodeBalance(
	envelope privateEventEnvelope,
	body []byte,
) (sandbox.PrivateEvent, error) {
	var native privateBalancePayload
	if strictDecode(envelope.Event, &native) != nil ||
		native.EventType != "outboundAccountPosition" ||
		native.EventTime <= 0 || native.LastAccountAt <= 0 ||
		len(native.Balances) == 0 {
		return sandbox.PrivateEvent{}, fmt.Errorf(
			"%w: balance_shape",
			ErrSandboxPrivateEvent,
		)
	}
	canonical, err := canonicalPrivateBalances(native.Balances)
	if err != nil {
		return sandbox.PrivateEvent{}, err
	}
	sort.Slice(canonical, func(left, right int) bool {
		return canonical[left].Asset < canonical[right].Asset
	})
	sourceHash := hashBytes(body)
	event := sandbox.PrivateEvent{
		Identity: fmt.Sprintf(
			"binance-balance-%d-%d-%s",
			*envelope.SubscriptionID, native.LastAccountAt, sourceHash[:12],
		),
		AccountID: decoder.account, AccountEpoch: decoder.epoch,
		Kind: sandbox.PrivateBalanceEvent, NativeOrderHash: sourceHash,
		BalanceHash: canonicalHash(canonical),
		OccurredAt:  time.UnixMilli(native.EventTime).UTC(),
		ReceivedAt:  decoder.now().UTC(),
	}
	if event.Validate() != nil {
		return sandbox.PrivateEvent{}, fmt.Errorf(
			"%w: normalized_balance_invalid", ErrSandboxPrivateEvent,
		)
	}
	return event, nil
}

func canonicalPrivateBalances(
	positions []privateBalancePosition,
) ([]canonicalPrivateBalance, error) {
	canonical := make([]canonicalPrivateBalance, 0, len(positions))
	seen := make(map[domain.AssetSymbol]struct{}, len(positions))
	for _, position := range positions {
		asset, assetErr := domain.ParseAssetSymbol(position.Asset)
		_, freeErr := domain.ParseBalance(position.Free)
		_, lockedErr := domain.ParseBalance(position.Locked)
		if assetErr != nil || freeErr != nil || lockedErr != nil {
			return nil, fmt.Errorf(
				"%w: balance_value",
				ErrSandboxPrivateEvent,
			)
		}
		if _, duplicate := seen[asset]; duplicate {
			return nil, fmt.Errorf(
				"%w: balance_duplicate",
				ErrSandboxPrivateEvent,
			)
		}
		seen[asset] = struct{}{}
		canonical = append(canonical, canonicalPrivateBalance(position))
	}
	return canonical, nil
}

func (decoder *privateEventDecoder) decodeExecution(
	ctx context.Context,
	envelope privateEventEnvelope,
	body []byte,
) (decodedPrivateEvent, error) {
	var native privateExecutionPayload
	if strictDecode(envelope.Event, &native) != nil ||
		native.EventType != "executionReport" ||
		native.EventTime <= 0 || native.TransactionTime <= 0 ||
		native.OrderID.String() == "" {
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
	submission, clientOrderID, err := decoder.lookupPrivateSubmission(
		ctx, native,
	)
	if err != nil ||
		!privateExecutionMatches(native, submission, clientOrderID) {
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
	cumulative, err := domain.ParseQuantity(native.CumulativeExecutedQuantity)
	if err != nil {
		return decodedPrivateEvent{}, ErrSandboxPrivateEvent
	}
	zero, _ := domain.ParseQuantity("0")
	sourceHash := hashBytes(body)
	if cumulative.Compare(zero) > 0 {
		return decodedPrivateEvent{
			submission:    submission,
			needsBackfill: true,
			sourceHash:    sourceHash,
		}, nil
	}
	event, err := decoder.buildPrivateExecutionEvent(
		envelope, native, submission, cumulative, sourceHash,
	)
	if err != nil {
		return decodedPrivateEvent{}, err
	}
	return decodedPrivateEvent{
		event: event, submission: submission, sourceHash: sourceHash,
	}, nil
}

func (decoder *privateEventDecoder) lookupPrivateSubmission(
	ctx context.Context,
	native privateExecutionPayload,
) (sandbox.Submission, string, error) {
	clientOrderID := native.ClientOrderID
	submission, found, err := decoder.lookup.SubmissionByClientOrderID(
		ctx, decoder.account, decoder.epoch, clientOrderID,
	)
	if (err != nil || !found) && native.OriginalClientOrderID != "" {
		clientOrderID = native.OriginalClientOrderID
		submission, found, err = decoder.lookup.SubmissionByClientOrderID(
			ctx, decoder.account, decoder.epoch, clientOrderID,
		)
	}
	if err != nil || !found {
		return sandbox.Submission{}, "", ErrSandboxPrivateEvent
	}
	return submission, clientOrderID, nil
}

func (decoder *privateEventDecoder) buildPrivateExecutionEvent(
	envelope privateEventEnvelope,
	native privateExecutionPayload,
	submission sandbox.Submission,
	cumulative domain.Quantity,
	sourceHash string,
) (sandbox.PrivateEvent, error) {
	state, err := sandboxOrderState(native.CurrentOrderStatus)
	if err != nil {
		return sandbox.PrivateEvent{}, ErrSandboxPrivateEvent
	}
	occurredAt := time.UnixMilli(native.TransactionTime).UTC()
	orderEvent := execution.OrderEvent{
		ID: fmt.Sprintf(
			"binance-stream-%s-%s-%s",
			native.OrderID.String(),
			native.CurrentOrderStatus,
			native.ExecutionID.String(),
		),
		OrderID:            submission.OrderID,
		ClientOrderID:      submission.ClientOrderID,
		State:              state,
		ExchangeStatus:     native.CurrentOrderStatus,
		CumulativeQuantity: cumulative,
		OccurredAt:         occurredAt,
		Ordinal:            uint64(native.TransactionTime),
	}
	event := sandbox.PrivateEvent{
		Identity: fmt.Sprintf(
			"binance-stream-%d-%s-%s",
			*envelope.SubscriptionID,
			native.ExecutionID.String(),
			sourceHash[:12],
		),
		AccountID:       decoder.account,
		AccountEpoch:    decoder.epoch,
		Kind:            sandbox.PrivateOrderEvent,
		OrderID:         submission.OrderID,
		ClientOrderID:   submission.ClientOrderID,
		NativeOrderHash: sourceHash,
		OrderEvent:      &orderEvent,
		OccurredAt:      occurredAt,
		ReceivedAt:      decoder.now().UTC(),
	}
	if err := validatePrivateExecutionEvent(event); err != nil {
		return sandbox.PrivateEvent{}, err
	}
	return event, nil
}

func validatePrivateExecutionEvent(event sandbox.PrivateEvent) error {
	if event.Validate() != nil {
		return fmt.Errorf(
			"%w: normalized_execution_invalid",
			ErrSandboxPrivateEvent,
		)
	}
	return nil
}

func privateExecutionMatches(
	native privateExecutionPayload,
	submission sandbox.Submission,
	clientOrderID string,
) bool {
	orderType, timeInForce, err := binanceOrderStyle(submission.Style)
	if submission.Style == sandbox.OrderStylePostOnly {
		timeInForce = "GTC"
	}
	quantity, quantityErr := domain.ParseQuantity(native.OriginalQuantity)
	price, priceErr := domain.ParsePrice(native.OriginalPrice)
	return err == nil && quantityErr == nil && priceErr == nil &&
		native.Symbol == submission.Instrument.Symbol() &&
		clientOrderID == submission.ClientOrderID &&
		native.Side == binanceSide(submission.Side) &&
		native.OrderType == orderType &&
		native.TimeInForce == timeInForce &&
		quantity.Compare(submission.Quantity) == 0 &&
		price.Compare(submission.LimitPrice) == 0 &&
		validPrivateExecutionType(native.CurrentExecutionType)
}

func validPrivateExecutionType(value string) bool {
	switch value {
	case "NEW", "CANCELED", "REPLACED", "REJECTED", "TRADE",
		"EXPIRED", "TRADE_PREVENTION":
		return true
	default:
		return false
	}
}
