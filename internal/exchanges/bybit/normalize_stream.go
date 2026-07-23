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
		return "", exchangecontracts.StreamEvent{}, streamValidation("decoder_schema_rejected")
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
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("stream_topic_unsupported")
	}
}

func normalizeLifecycle(
	envelope streamEnvelope,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	state, reason := "", ""
	switch envelope.Op {
	case "subscribe":
		if envelope.Success == nil || !*envelope.Success ||
			(envelope.RetMsg != "" && envelope.RetMsg != "subscribe") ||
			envelope.ConnID == "" || envelope.RequestID != "" {
			return "", exchangecontracts.StreamEvent{},
				streamValidation("subscription_ack_invalid")
		}
		state, reason = "SUBSCRIBED", "subscription_acknowledged"
	case "ping":
		if !validSpotHeartbeat(envelope) {
			return "", exchangecontracts.StreamEvent{}, streamValidation("heartbeat_response_invalid")
		}
		state, reason = "HEALTHY", "heartbeat_pong"
	case "pong":
		if envelope.Success == nil || !*envelope.Success || envelope.RetMsg != "pong" ||
			envelope.ConnID == "" || envelope.RequestID != "" {
			return "", exchangecontracts.StreamEvent{}, streamValidation("heartbeat_response_invalid")
		}
		state, reason = "HEALTHY", "heartbeat_pong"
	default:
		return "", exchangecontracts.StreamEvent{}, streamValidation("lifecycle_operation_unsupported")
	}
	lifecycle := exchangecontracts.LifecycleEvent{Exchange: "bybit", State: state, Reason: reason,
		ConnectionID: envelope.ConnID, ObservedAt: receivedAt}
	return envelope.Op, exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamLifecycle,
		Lifecycle: &lifecycle}, nil
}

func validSpotHeartbeat(envelope streamEnvelope) bool {
	return envelope.Success != nil && *envelope.Success && envelope.RetMsg == "pong" &&
		envelope.ConnID != "" && envelope.RequestID == ""
}

func normalizeStreamBook(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	parts := strings.Split(envelope.Topic, ".")
	if len(parts) != 3 || parts[0] != "orderbook" || parts[1] != "1000" || envelope.TS <= 0 ||
		(envelope.Type != "snapshot" && envelope.Type != "delta") {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("orderbook_envelope_invalid")
	}
	var native orderBookResult
	if err := strictDecode(envelope.Data, &native); err != nil || native.Symbol != parts[2] || native.UpdateID == 0 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("orderbook_schema_rejected")
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("orderbook_symbol_invalid")
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
			return envelope.Topic, exchangecontracts.StreamEvent{},
				streamValidation("orderbook_snapshot_empty")
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
	if !ok || envelope.Type != "snapshot" || envelope.TS <= 0 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_envelope_invalid")
	}
	var native []streamTradePayload
	if strictDecode(envelope.Data, &native) != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_schema_rejected")
	}
	if len(native) == 0 || len(native) > 1024 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_batch_invalid")
	}
	instrument, err := instrumentForSymbol(symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_symbol_invalid")
	}
	trades := make([]exchangecontracts.Trade, 0, len(native))
	for _, item := range native {
		if item.Symbol != symbol || item.TradeID == "" || item.CrossSequence == 0 ||
			(item.Side != "Buy" && item.Side != "Sell") {
			return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_identity_invalid")
		}
		price, priceErr := domain.ParsePrice(item.Price)
		quantity, quantityErr := domain.ParseQuantity(item.Size)
		if priceErr != nil || quantityErr != nil || item.Timestamp <= 0 {
			return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("public_trade_numeric_invalid")
		}
		// BT and RPI are classification flags on legitimate Bybit public Spot
		// executions. They do not change the shared canonical trade economics,
		// so retain the trade instead of treating either flag as malformed.
		trades = append(trades, exchangecontracts.Trade{Exchange: "bybit", Instrument: instrument,
			NativeID: item.TradeID, SourceSequence: item.CrossSequence, Price: price, Quantity: quantity,
			BuyerIsMaker: item.Side == "Sell", ExchangeTime: time.UnixMilli(item.Timestamp).UTC(),
			ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)})
	}
	event := exchangecontracts.StreamEvent{Kind: exchangecontracts.StreamTrades}
	if len(trades) == 1 {
		event.Trade = &trades[0]
	} else {
		event.Trades = trades
	}
	return envelope.Topic, event, nil
}

func normalizeStreamTicker(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
	state map[string]tickerPayload,
) (string, exchangecontracts.StreamEvent, error) {
	symbol, ok := topicParts(envelope.Topic, "tickers.")
	if !ok || envelope.TS <= 0 || envelope.CrossSequence == 0 ||
		(envelope.Type != "snapshot" && envelope.Type != "delta") {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_envelope_invalid")
	}
	var native tickerPayload
	if strictDecode(envelope.Data, &native) != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_schema_rejected")
	}
	if native.Symbol != symbol {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_symbol_invalid")
	}
	if envelope.Type == "delta" {
		prior, exists := state[symbol]
		if !exists {
			return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_delta_without_snapshot")
		}
		native = mergeTicker(prior, native)
	}
	state[symbol] = native
	instrument, err := instrumentForSymbol(symbol)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_symbol_invalid")
	}
	ticker, err := normalizeTicker(native, instrument, time.UnixMilli(envelope.TS).UTC(), receivedAt, payloadHash(payload), false)
	if err != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("ticker_values_invalid")
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
	requireBestQuote bool,
) (exchangecontracts.Ticker, error) {
	last, lastErr := domain.ParsePrice(native.LastPrice)
	quoteFields := []string{native.BidPrice, native.BidSize, native.AskPrice, native.AskSize}
	quoteCount := 0
	for _, value := range quoteFields {
		if value != "" {
			quoteCount++
		}
	}
	if lastErr != nil || exchangeTime.IsZero() || exchangeTime.Location() != time.UTC ||
		receivedAt.Validate() != nil || (quoteCount != 0 && quoteCount != len(quoteFields)) ||
		(requireBestQuote && quoteCount != len(quoteFields)) {
		return exchangecontracts.Ticker{}, validationError(exchangecontracts.OperationTicker)
	}
	ticker := exchangecontracts.Ticker{Exchange: "bybit", Instrument: instrument, LastPrice: last,
		ExchangeTime: exchangeTime, ReceivedAt: receivedAt, RawPayloadHash: rawHash}
	if quoteCount == 0 {
		return ticker, nil
	}
	bid, bidErr := domain.ParsePrice(native.BidPrice)
	bidQuantity, bidQuantityErr := domain.ParseQuantity(native.BidSize)
	ask, askErr := domain.ParsePrice(native.AskPrice)
	askQuantity, askQuantityErr := domain.ParseQuantity(native.AskSize)
	if bidErr != nil || bidQuantityErr != nil || askErr != nil || askQuantityErr != nil || bid.Compare(ask) >= 0 {
		return exchangecontracts.Ticker{}, validationError(exchangecontracts.OperationTicker)
	}
	ticker.BidPrice, ticker.BidQuantity = bid, bidQuantity
	ticker.AskPrice, ticker.AskQuantity, ticker.BestQuotePresent = ask, askQuantity, true
	return ticker, nil
}

func normalizeStreamCandle(
	envelope streamEnvelope,
	payload []byte,
	receivedAt domain.EventTime,
) (string, exchangecontracts.StreamEvent, error) {
	parts := strings.Split(envelope.Topic, ".")
	if len(parts) != 3 || parts[0] != "kline" || envelope.Type != "snapshot" || envelope.TS <= 0 {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("candle_envelope_invalid")
	}
	var native []streamCandlePayload
	if strictDecode(envelope.Data, &native) != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("candle_schema_rejected")
	}
	if len(native) != 1 || native[0].Interval != parts[1] {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("candle_batch_invalid")
	}
	interval, ok := intervalCanonical(parts[1])
	instrument, err := instrumentForSymbol(parts[2])
	if !ok || err != nil || native[0].Start <= 0 || native[0].End <= native[0].Start {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("candle_identity_invalid")
	}
	open, openErr := domain.ParsePrice(native[0].Open)
	high, highErr := domain.ParsePrice(native[0].High)
	low, lowErr := domain.ParsePrice(native[0].Low)
	closePrice, closeErr := domain.ParsePrice(native[0].Close)
	volume, volumeErr := domain.ParseQuantity(native[0].Volume)
	if openErr != nil || highErr != nil || lowErr != nil || closeErr != nil || volumeErr != nil {
		return envelope.Topic, exchangecontracts.StreamEvent{}, streamValidation("candle_values_invalid")
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
			return formatUint(event.Trade.SourceSequence), &value
		}
		if len(event.Trades) != 0 {
			value := event.Trades[len(event.Trades)-1].ExchangeTime
			return formatUint(event.Trades[0].SourceSequence) + ":" +
				formatUint(event.Trades[len(event.Trades)-1].SourceSequence), &value
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
