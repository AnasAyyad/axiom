package bybit

import (
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func normalizeStream(
	payload []byte,
	receivedAt domain.EventTime,
	tickerState map[string]tickerPayload,
) (string, exchangecontracts.StreamEvent, error) {
	var envelope streamEnvelope
	if err := strictDecode(payload, &envelope); err != nil || receivedAt.Validate() != nil {
		return "", exchangecontracts.StreamEvent{}, streamError()
	}
	if envelope.Op != "" {
		return normalizeLifecycle(envelope, receivedAt)
	}
	switch {
	case strings.HasPrefix(envelope.Topic, "orderbook."):
		return normalizeStreamBook(envelope, payload, receivedAt)
	case strings.HasPrefix(envelope.Topic, "publicTrade."):
		return normalizeStreamTrade(envelope, payload, receivedAt)
	case strings.HasPrefix(envelope.Topic, "tickers."):
		return normalizeStreamTicker(envelope, payload, receivedAt, tickerState)
	case strings.HasPrefix(envelope.Topic, "kline."):
		return normalizeStreamCandle(envelope, payload, receivedAt)
	default:
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
}

func normalizeLifecycle(
	envelope streamEnvelope,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	state, reason := "", ""
	switch envelope.Op {
	case "subscribe":
		if envelope.Success == nil || !*envelope.Success || envelope.RetMsg != "subscribe" || envelope.ConnID == "" {
			return "", exchangecontracts.StreamEvent{}, streamError()
		}
		state, reason = "SUBSCRIBED", "subscription_acknowledged"
	case "pong":
		if envelope.Success == nil || !*envelope.Success || envelope.RetMsg != "pong" || envelope.ConnID == "" {
			return "", exchangecontracts.StreamEvent{}, streamError()
		}
		state, reason = "HEALTHY", "heartbeat_pong"
	default:
		return "", exchangecontracts.StreamEvent{}, streamError()
	}
	lifecycle := exchangecontracts.LifecycleEvent{Exchange: "bybit", State: state, Reason: reason,
		ConnectionID: envelope.ConnID, ObservedAt: receivedAt}
	return envelope.Op, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamLifecycle,
		Lifecycle: &lifecycle}, nil
}

func normalizeStreamBook(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	parts := strings.Split(envelope.Topic, ".")
	if len(parts) != 3 || parts[0] != "orderbook" || parts[1] != "1000" || envelope.TS <= 0 ||
		(envelope.Type != "snapshot" && envelope.Type != "delta") {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	var native orderBookResult
	if err := strictDecode(envelope.Data, &native); err != nil || native.Symbol != parts[2] || native.UpdateID == 0 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	bids, err := normalizeLevels(native.Bids, envelope.Type == "delta" && native.UpdateID != 1)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	asks, err := normalizeLevels(native.Asks, envelope.Type == "delta" && native.UpdateID != 1)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	if envelope.Type == "snapshot" || native.UpdateID == 1 {
		if len(bids) == 0 || len(asks) == 0 {
			return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
		}
		snapshot := exchangecontracts.BookSnapshot{Exchange: "bybit", Instrument: instrument,
			LastSequence: native.UpdateID, ReceivedAt: receivedAt, Bids: bids, Asks: asks,
			RawPayloadHash: payloadHash(payload)}
		return envelope.Topic, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamDepth,
			Snapshot: &snapshot}, nil
	}
	exchangeTime := envelope.CTS
	if exchangeTime <= 0 {
		exchangeTime = envelope.TS
	}
	update := exchangecontracts.DepthUpdate{Exchange: "bybit", Instrument: instrument,
		ExchangeTime: time.UnixMilli(exchangeTime).UTC(), FirstSequence: native.UpdateID,
		LastSequence: native.UpdateID, ReceivedAt: receivedAt, Bids: bids, Asks: asks,
		RawPayloadHash: payloadHash(payload)}
	return envelope.Topic, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamDepth, Depth: &update}, nil
}

func normalizeStreamTrade(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	symbol, ok := topicParts(envelope.Topic, "publicTrade.")
	var native []streamTradePayload
	if !ok || envelope.Type != "snapshot" || envelope.TS <= 0 || strictDecode(envelope.Data, &native) != nil ||
		len(native) != 1 || native[0].Symbol != symbol || native[0].TradeID == "" || native[0].BlockTrade ||
		native[0].RPITrade || (native[0].Side != "Buy" && native[0].Side != "Sell") {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	instrument, err := instrumentForSymbol(symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	price, priceErr := domain.ParsePrice(native[0].Price)
	quantity, quantityErr := domain.ParseQuantity(native[0].Size)
	if priceErr != nil || quantityErr != nil || native[0].Timestamp <= 0 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	trade := exchangecontracts.Trade{Exchange: "bybit", Instrument: instrument,
		NativeID: native[0].TradeID, Price: price, Quantity: quantity,
		BuyerIsMaker: native[0].Side == "Sell", ExchangeTime: time.UnixMilli(native[0].Timestamp).UTC(),
		ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)}
	return envelope.Topic, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamTrades, Trade: &trade}, nil
}

func normalizeStreamTicker(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
	state map[string]tickerPayload,
) (string, exchangecontracts.StreamEvent, error) {
	symbol, ok := topicParts(envelope.Topic, "tickers.")
	var native tickerPayload
	if !ok || envelope.TS <= 0 || (envelope.Type != "snapshot" && envelope.Type != "delta") ||
		strictDecode(envelope.Data, &native) != nil || native.Symbol != symbol {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	if envelope.Type == "delta" {
		prior, exists := state[symbol]
		if !exists {
			return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
		}
		native = mergeTicker(prior, native)
	}
	state[symbol] = native
	instrument, err := instrumentForSymbol(symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	ticker, err := normalizeTicker(native, instrument, time.UnixMilli(envelope.TS).UTC(), receivedAt, payloadHash(payload))
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, err
	}
	return envelope.Topic, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamTicker, Ticker: &ticker}, nil
}

func mergeTicker(prior, update tickerPayload) tickerPayload {
	if update.BidPrice == "" {
		update.BidPrice = prior.BidPrice
	}
	if update.BidSize == "" {
		update.BidSize = prior.BidSize
	}
	if update.AskPrice == "" {
		update.AskPrice = prior.AskPrice
	}
	if update.AskSize == "" {
		update.AskSize = prior.AskSize
	}
	if update.LastPrice == "" {
		update.LastPrice = prior.LastPrice
	}
	return update
}

func normalizeTicker(
	native tickerPayload,
	instrument domain.Instrument,
	exchangeTime time.Time,
	receivedAt domain.EventTime,
	rawHash string,
) (exchangecontracts.Ticker, error) {
	bid, bidErr := domain.ParsePrice(native.BidPrice)
	bidQuantity, bidQuantityErr := domain.ParseQuantity(native.BidSize)
	ask, askErr := domain.ParsePrice(native.AskPrice)
	askQuantity, askQuantityErr := domain.ParseQuantity(native.AskSize)
	last, lastErr := domain.ParsePrice(native.LastPrice)
	if bidErr != nil || bidQuantityErr != nil || askErr != nil || askQuantityErr != nil || lastErr != nil ||
		exchangeTime.IsZero() || exchangeTime.Location() != time.UTC || receivedAt.Validate() != nil ||
		bid.Compare(ask) >= 0 {
		return exchangecontracts.Ticker{}, validationError(exchangecontracts.OperationTicker)
	}
	return exchangecontracts.Ticker{Exchange: "bybit", Instrument: instrument, BidPrice: bid,
		BidQuantity: bidQuantity, AskPrice: ask, AskQuantity: askQuantity, LastPrice: last,
		ExchangeTime: exchangeTime, ReceivedAt: receivedAt, RawPayloadHash: rawHash}, nil
}

func normalizeStreamCandle(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	parts := strings.Split(envelope.Topic, ".")
	var native []streamCandlePayload
	if len(parts) != 3 || parts[0] != "kline" || envelope.Type != "snapshot" || envelope.TS <= 0 ||
		strictDecode(envelope.Data, &native) != nil || len(native) != 1 || native[0].Interval != parts[1] {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	interval, ok := intervalCanonical(parts[1])
	instrument, err := instrumentForSymbol(parts[2])
	if !ok || err != nil || native[0].Start <= 0 || native[0].End <= native[0].Start {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	open, openErr := domain.ParsePrice(native[0].Open)
	high, highErr := domain.ParsePrice(native[0].High)
	low, lowErr := domain.ParsePrice(native[0].Low)
	closePrice, closeErr := domain.ParsePrice(native[0].Close)
	volume, volumeErr := domain.ParseQuantity(native[0].Volume)
	if openErr != nil || highErr != nil || lowErr != nil || closeErr != nil || volumeErr != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamError()
	}
	candle := exchangecontracts.Candle{Exchange: "bybit", Instrument: instrument, Interval: interval,
		OpenTime: time.UnixMilli(native[0].Start).UTC(), CloseTime: time.UnixMilli(native[0].End).UTC(),
		Open: open, High: high, Low: low, Close: closePrice, Volume: volume, Closed: native[0].Confirm,
		ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)}
	return envelope.Topic, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamCandle, Candle: &candle}, nil
}

func canonicalStreamEvidence(event exchangecontracts.StreamEvent) (string, *time.Time) {
	switch event.Kind {
	case exchangecontracts.StreamDepth:
		if event.Snapshot != nil {
			return formatUint(event.Snapshot.LastSequence), nil
		}
		if event.Depth != nil {
			value := event.Depth.ExchangeTime
			return formatUint(event.Depth.LastSequence), &value
		}
	case exchangecontracts.StreamTrades:
		if event.Trade != nil {
			value := event.Trade.ExchangeTime
			return event.Trade.NativeID, &value
		}
	case exchangecontracts.StreamTicker:
		if event.Ticker != nil {
			value := event.Ticker.ExchangeTime
			return formatInt(value.UnixMilli()), &value
		}
	case exchangecontracts.StreamCandle:
		if event.Candle != nil {
			value := event.Candle.CloseTime
			return formatInt(value.UnixMilli()), &value
		}
	}
	return "", nil
}

func formatUint(value uint64) string { return strconv.FormatUint(value, 10) }

func formatInt(value int64) string { return strconv.FormatInt(value, 10) }
