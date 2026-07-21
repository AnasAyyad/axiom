package bybit

import (
	"sort"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// NormalizeServerTime converts one Bybit public time response.
func NormalizeServerTime(payload []byte) (time.Time, error) {
	result, _, err := unwrap[serverTimeResult](payload)
	if err != nil || len(result.TimeNano) != 19 {
		return time.Time{}, validationError(exchangecontracts.OperationMetadata)
	}
	nanoseconds, err := millisecondString(result.TimeNano[:len(result.TimeNano)-6])
	if err != nil || result.TimeSecond == "" ||
		nanoseconds.Unix() <= 0 {
		return time.Time{}, validationError(exchangecontracts.OperationMetadata)
	}
	return time.Unix(0, nanoseconds.UnixNano()).UTC(), nil
}

// NormalizeSnapshot converts one Bybit REST order-book replacement.
func NormalizeSnapshot(
	payload []byte,
	instrument domain.Instrument,
	receivedAt domain.EventTime,
) (exchangecontracts.BookSnapshot, error) {
	result, _, err := unwrap[orderBookResult](payload)
	if err != nil || receivedAt.Validate() != nil || !approvedInstrument(instrument) ||
		result.Symbol != instrument.Symbol() || result.UpdateID == 0 {
		return exchangecontracts.BookSnapshot{}, validationError(exchangecontracts.OperationSnapshot)
	}
	bids, err := normalizeLevels(result.Bids, false)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	asks, err := normalizeLevels(result.Asks, false)
	if err != nil || len(bids) == 0 || len(asks) == 0 {
		return exchangecontracts.BookSnapshot{}, validationError(exchangecontracts.OperationSnapshot)
	}
	return exchangecontracts.BookSnapshot{Exchange: "bybit", Instrument: instrument,
		LastSequence: result.UpdateID, ReceivedAt: receivedAt, Bids: bids, Asks: asks,
		RawPayloadHash: payloadHash(payload)}, nil
}

// NormalizeInstruments converts one bounded Bybit Spot metadata response.
func NormalizeInstruments(
	payload []byte,
	observedAt time.Time,
	version uint64,
) ([]exchangecontracts.InstrumentRecord, error) {
	result, _, err := unwrap[instrumentsResult](payload)
	if err != nil || result.Category != "spot" || result.NextPageCursor != "" || version == 0 ||
		observedAt.IsZero() || observedAt.Location() != time.UTC {
		return nil, validationError(exchangecontracts.OperationMetadata)
	}
	records := make([]exchangecontracts.InstrumentRecord, 0, len(result.List))
	unknown := false
	for _, native := range result.List {
		record, normalizeErr := normalizeInstrument(native, observedAt, version, payloadHash(payload))
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		records = append(records, record)
		unknown = unknown || native.Status != "Trading"
	}
	if len(records) == 0 || unknown {
		return records, validationError(exchangecontracts.OperationMetadata)
	}
	return records, nil
}

func normalizeInstrument(
	native instrumentPayload,
	observedAt time.Time,
	version uint64,
	rawHash string,
) (exchangecontracts.InstrumentRecord, error) {
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil || string(instrument.Base) != native.BaseCoin || string(instrument.Quote) != native.QuoteCoin {
		return exchangecontracts.InstrumentRecord{}, validationError(exchangecontracts.OperationMetadata)
	}
	tick, tickErr := domain.ParsePrice(native.PriceFilter.TickSize)
	step, stepErr := domain.ParseQuantity(native.LotSizeFilter.BasePrecision)
	minimum, minimumErr := domain.ParseQuantity(native.LotSizeFilter.MinimumOrderQty)
	minimumNotional, notionalErr := domain.ParseNotional(native.LotSizeFilter.MinimumOrderAmount)
	if tickErr != nil || stepErr != nil || minimumErr != nil || notionalErr != nil {
		return exchangecontracts.InstrumentRecord{}, validationError(exchangecontracts.OperationMetadata)
	}
	metadata := domain.InstrumentMetadata{Instrument: instrument, Version: version,
		EffectiveAt: observedAt, PriceTick: tick, QuantityStep: step,
		MinimumQuantity: minimum, MinimumNotional: minimumNotional}
	if metadata.Validate() != nil {
		return exchangecontracts.InstrumentRecord{}, validationError(exchangecontracts.OperationMetadata)
	}
	return exchangecontracts.InstrumentRecord{Exchange: "bybit", NativeSymbol: native.Symbol,
		NativeStatus: native.Status, Metadata: metadata, RawPayloadHash: rawHash}, nil
}

// NormalizeTrades converts one bounded Bybit recent-trade response.
func NormalizeTrades(
	payload []byte,
	instrument domain.Instrument,
	receivedAt domain.EventTime,
) ([]exchangecontracts.Trade, error) {
	result, _, err := unwrap[tradesResult](payload)
	if err != nil || result.Category != "spot" || !approvedInstrument(instrument) || receivedAt.Validate() != nil {
		return nil, validationError(exchangecontracts.OperationTrades)
	}
	trades := make([]exchangecontracts.Trade, 0, len(result.List))
	for _, native := range result.List {
		if native.Symbol != instrument.Symbol() || native.ExecutionID == "" || native.BlockTrade || native.RPITrade ||
			(native.Side != "Buy" && native.Side != "Sell") {
			return nil, validationError(exchangecontracts.OperationTrades)
		}
		price, priceErr := domain.ParsePrice(native.Price)
		quantity, quantityErr := domain.ParseQuantity(native.Size)
		exchangeTime, timeErr := millisecondString(native.Time)
		if priceErr != nil || quantityErr != nil || timeErr != nil {
			return nil, validationError(exchangecontracts.OperationTrades)
		}
		trades = append(trades, exchangecontracts.Trade{Exchange: "bybit", Instrument: instrument,
			NativeID: native.ExecutionID, Price: price, Quantity: quantity, BuyerIsMaker: native.Side == "Sell",
			ExchangeTime: exchangeTime, ReceivedAt: receivedAt, RawPayloadHash: payloadHash(payload)})
	}
	return trades, nil
}

// NormalizeCandleHistory converts reverse-ordered Bybit candle rows to chronological candles.
func NormalizeCandleHistory(
	payload []byte,
	instrument domain.Instrument,
	interval string,
	receivedAt domain.EventTime,
) ([]exchangecontracts.Candle, error) {
	result, _, err := unwrap[candlesResult](payload)
	nativeInterval, validInterval := intervalNative(interval)
	if err != nil || !validInterval || result.Category != "spot" || result.Symbol != instrument.Symbol() ||
		!approvedInstrument(instrument) || receivedAt.Validate() != nil {
		return nil, validationError(exchangecontracts.OperationCandles)
	}
	candles := make([]exchangecontracts.Candle, 0, len(result.List))
	for _, row := range result.List {
		candle, normalizeErr := normalizeHistoryCandle(row, instrument, interval, receivedAt, payloadHash(payload))
		if normalizeErr != nil || nativeInterval == "" {
			return nil, validationError(exchangecontracts.OperationCandles)
		}
		candles = append(candles, candle)
	}
	sort.Slice(candles, func(left, right int) bool { return candles[left].OpenTime.Before(candles[right].OpenTime) })
	return candles, nil
}

func normalizeHistoryCandle(
	row []string,
	instrument domain.Instrument,
	interval string,
	receivedAt domain.EventTime,
	rawHash string,
) (exchangecontracts.Candle, error) {
	if len(row) != 7 {
		return exchangecontracts.Candle{}, validationError(exchangecontracts.OperationCandles)
	}
	openTime, timeErr := millisecondString(row[0])
	open, openErr := domain.ParsePrice(row[1])
	high, highErr := domain.ParsePrice(row[2])
	low, lowErr := domain.ParsePrice(row[3])
	closePrice, closeErr := domain.ParsePrice(row[4])
	volume, volumeErr := domain.ParseQuantity(row[5])
	duration, durationErr := candleDuration(interval)
	if timeErr != nil || openErr != nil || highErr != nil || lowErr != nil || closeErr != nil ||
		volumeErr != nil || durationErr != nil {
		return exchangecontracts.Candle{}, validationError(exchangecontracts.OperationCandles)
	}
	return exchangecontracts.Candle{Exchange: "bybit", Instrument: instrument, Interval: interval,
		OpenTime: openTime, CloseTime: openTime.Add(duration - time.Millisecond), Open: open,
		High: high, Low: low, Close: closePrice, Volume: volume, Closed: true,
		ReceivedAt: receivedAt, RawPayloadHash: rawHash}, nil
}

func candleDuration(interval string) (time.Duration, error) {
	switch interval {
	case "15m":
		return 15 * time.Minute, nil
	case "1h":
		return time.Hour, nil
	case "4h":
		return 4 * time.Hour, nil
	default:
		return 0, validationError(exchangecontracts.OperationCandles)
	}
}

// NormalizeTicker converts one complete Bybit public ticker response.
func NormalizeTicker(
	payload []byte,
	instrument domain.Instrument,
	receivedAt domain.EventTime,
) (exchangecontracts.Ticker, error) {
	result, exchangeMillis, err := unwrap[tickersResult](payload)
	if err != nil || result.Category != "spot" || len(result.List) != 1 ||
		result.List[0].Symbol != instrument.Symbol() {
		return exchangecontracts.Ticker{}, validationError(exchangecontracts.OperationTicker)
	}
	return normalizeTicker(result.List[0], instrument, time.UnixMilli(exchangeMillis).UTC(), receivedAt, payloadHash(payload))
}
