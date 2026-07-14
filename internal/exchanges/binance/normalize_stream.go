package binance

import (
	"encoding/json"
	"strconv"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func normalizeCombinedStream(
	payload []byte,
	expected map[string]exchangecontracts.StreamKind,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	var wrapper combinedStreamPayload
	if err := strictDecode(payload, &wrapper); err != nil || len(wrapper.Data) == 0 {
		return "", exchangecontracts.StreamEvent{}, streamError()
	}
	kind, ok := expected[wrapper.Stream]
	if !ok {
		return "", exchangecontracts.StreamEvent{}, streamError()
	}
	switch kind {
	case exchangecontracts.StreamDepth:
		depth, err := NormalizeDepth(wrapper.Data, receivedAt)
		if err != nil {
			return wrapper.Stream, exchangecontracts.StreamEvent{}, err
		}
		return wrapper.Stream, exchangecontracts.StreamEvent{Kind: kind, Depth: &depth}, nil
	case exchangecontracts.StreamTrades:
		trade, err := normalizeStreamTrade(wrapper.Data, receivedAt)
		if err != nil {
			return wrapper.Stream, exchangecontracts.StreamEvent{}, err
		}
		return wrapper.Stream, exchangecontracts.StreamEvent{Kind: kind, Trade: &trade}, nil
	case exchangecontracts.StreamCandle:
		candle, err := NormalizeCandle(wrapper.Data, receivedAt)
		if err != nil {
			return wrapper.Stream, exchangecontracts.StreamEvent{}, err
		}
		return wrapper.Stream, exchangecontracts.StreamEvent{Kind: kind, Candle: &candle}, nil
	default:
		return wrapper.Stream, exchangecontracts.StreamEvent{}, streamError()
	}
}

func normalizeStreamTrade(payload json.RawMessage, receivedAt domain.EventTime) (exchangecontracts.Trade, error) {
	var native streamTradePayload
	if err := strictDecode(payload, &native); err != nil || native.EventType != "trade" || native.EventTime <= 0 ||
		native.TradeTime <= 0 || native.TradeID == 0 {
		return exchangecontracts.Trade{}, streamError()
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return exchangecontracts.Trade{}, err
	}
	price, priceErr := domain.ParsePrice(native.Price)
	quantity, quantityErr := domain.ParseQuantity(native.Quantity)
	if priceErr != nil || quantityErr != nil {
		return exchangecontracts.Trade{}, streamError()
	}
	return exchangecontracts.Trade{Exchange: "binance", Instrument: instrument,
		NativeID: strconv.FormatUint(native.TradeID, 10), Price: price, Quantity: quantity,
		BuyerIsMaker: native.BuyerIsMaker, ExchangeTime: time.UnixMilli(native.TradeTime).UTC(),
		ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)}, nil
}

func streamError() error {
	return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
}
