package binance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// NormalizeSnapshot strictly converts a Binance depth snapshot to canonical form.
func NormalizeSnapshot(
	payload []byte,
	instrument domain.Instrument,
	receivedAt domain.EventTime,
) (exchangecontracts.BookSnapshot, error) {
	var native depthSnapshotPayload
	if err := strictDecode(payload, &native); err != nil || native.LastUpdateID == 0 ||
		receivedAt.Validate() != nil || !validSpotInstrument(instrument) {
		return exchangecontracts.BookSnapshot{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationSnapshot, 0,
		)
	}
	bids, err := normalizeLevels(native.Bids)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	asks, err := normalizeLevels(native.Asks)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	return exchangecontracts.BookSnapshot{
		Exchange:       "binance",
		Instrument:     instrument,
		LastSequence:   native.LastUpdateID,
		ReceivedAt:     receivedAt,
		Bids:           bids,
		Asks:           asks,
		RawPayloadHash: payloadHash(payload),
	}, nil
}

// NormalizeDepth strictly converts a Binance incremental depth payload.
func NormalizeDepth(payload []byte, receivedAt domain.EventTime) (exchangecontracts.DepthUpdate, error) {
	var native depthUpdatePayload
	if err := strictDecode(payload, &native); err != nil || native.EventType != "depthUpdate" ||
		native.EventTime <= 0 || native.FirstSequence == 0 || native.LastSequence < native.FirstSequence ||
		receivedAt.Validate() != nil {
		return exchangecontracts.DepthUpdate{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0,
		)
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return exchangecontracts.DepthUpdate{}, err
	}
	bids, err := normalizeLevels(native.Bids)
	if err != nil {
		return exchangecontracts.DepthUpdate{}, err
	}
	asks, err := normalizeLevels(native.Asks)
	if err != nil {
		return exchangecontracts.DepthUpdate{}, err
	}
	return exchangecontracts.DepthUpdate{
		Exchange:       "binance",
		Instrument:     instrument,
		ExchangeTime:   time.UnixMilli(native.EventTime).UTC(),
		FirstSequence:  native.FirstSequence,
		LastSequence:   native.LastSequence,
		ReceivedAt:     receivedAt,
		Bids:           bids,
		Asks:           asks,
		RawPayloadHash: payloadHash(payload),
	}, nil
}

// NormalizeTrades strictly converts a Binance public-trade response.
func NormalizeTrades(
	payload []byte,
	instrument domain.Instrument,
	receivedAt domain.EventTime,
) ([]exchangecontracts.Trade, error) {
	var native []tradePayload
	if err := strictDecode(payload, &native); err != nil || receivedAt.Validate() != nil || !validSpotInstrument(instrument) {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationTrades, 0)
	}
	trades := make([]exchangecontracts.Trade, 0, len(native))
	for _, item := range native {
		price, priceErr := domain.ParsePrice(item.Price)
		quantity, quantityErr := domain.ParseQuantity(item.Quantity)
		if item.ID == 0 || item.Time <= 0 || priceErr != nil || quantityErr != nil {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationTrades, 0)
		}
		trades = append(trades, exchangecontracts.Trade{
			Exchange:       "binance",
			Instrument:     instrument,
			NativeID:       strconv.FormatUint(item.ID, 10),
			Price:          price,
			Quantity:       quantity,
			BuyerIsMaker:   item.BuyerIsMaker,
			ExchangeTime:   time.UnixMilli(item.Time).UTC(),
			ReceivedAt:     receivedAt,
			RawPayloadHash: payloadHash(payload),
		})
	}
	return trades, nil
}

// NormalizeCandle strictly converts one Binance public candle stream payload.
func NormalizeCandle(payload []byte, receivedAt domain.EventTime) (exchangecontracts.Candle, error) {
	var native candlePayload
	if err := strictDecode(payload, &native); err != nil || native.EventType != "kline" ||
		native.EventTime <= 0 || native.Symbol != native.Candle.Symbol || !supportedCandleInterval(native.Candle.Interval) ||
		native.Candle.OpenTime <= 0 || native.Candle.CloseTime <= native.Candle.OpenTime || receivedAt.Validate() != nil {
		return exchangecontracts.Candle{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0,
		)
	}
	instrument, err := instrumentForSymbol(native.Symbol)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	open, high, low, close, volume, err := candleValues(native.Candle)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	return exchangecontracts.Candle{
		Exchange:       "binance",
		Instrument:     instrument,
		Interval:       native.Candle.Interval,
		OpenTime:       time.UnixMilli(native.Candle.OpenTime).UTC(),
		CloseTime:      time.UnixMilli(native.Candle.CloseTime).UTC(),
		Open:           open,
		High:           high,
		Low:            low,
		Close:          close,
		Volume:         volume,
		Closed:         native.Candle.Closed,
		ReceivedAt:     receivedAt,
		RawPayloadHash: payloadHash(payload),
	}, nil
}

func normalizeLevels(native [][]string) ([]exchangecontracts.PriceLevel, error) {
	levels := make([]exchangecontracts.PriceLevel, 0, len(native))
	for _, item := range native {
		if len(item) != 2 {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
		}
		price, priceErr := domain.ParsePrice(item[0])
		quantity, quantityErr := domain.ParseQuantity(item[1])
		if priceErr != nil || quantityErr != nil {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
		}
		levels = append(levels, exchangecontracts.PriceLevel{Price: price, Quantity: quantity})
	}
	return levels, nil
}

func strictDecode(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return err
		}
		return &json.SyntaxError{}
	}
	return nil
}

func payloadHash(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
