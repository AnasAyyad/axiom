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
	case exchangecontracts.StreamTicker:
		ticker, err := normalizeStreamTicker(wrapper.Data, receivedAt)
		if err != nil {
			return wrapper.Stream, exchangecontracts.StreamEvent{}, err
		}
		return wrapper.Stream, exchangecontracts.StreamEvent{Kind: kind, Ticker: &ticker}, nil
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

func normalizeStreamTicker(payload json.RawMessage, receivedAt domain.EventTime) (exchangecontracts.Ticker, error) {
	var native streamTickerPayload
	if err := strictDecode(payload, &native); err != nil || native.UpdateID == 0 || receivedAt.Validate() != nil {
		return exchangecontracts.Ticker{}, streamError()
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return exchangecontracts.Ticker{}, err
	}
	bid, bidErr := domain.ParsePrice(native.BidPrice)
	bidQuantity, bidQuantityErr := domain.ParseQuantity(native.BidQuantity)
	ask, askErr := domain.ParsePrice(native.AskPrice)
	askQuantity, askQuantityErr := domain.ParseQuantity(native.AskQuantity)
	if bidErr != nil || bidQuantityErr != nil || askErr != nil || askQuantityErr != nil || bid.Compare(ask) >= 0 {
		return exchangecontracts.Ticker{}, streamError()
	}
	return exchangecontracts.Ticker{Exchange: "binance", Instrument: instrument, BidPrice: bid,
		BidQuantity: bidQuantity, AskPrice: ask, AskQuantity: askQuantity, BestQuotePresent: true, LastPrice: bid,
		ExchangeTime: receivedAt.UTC, ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)}, nil
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
