package binance

import (
	"encoding/json"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// NormalizeCandleHistory strictly converts Binance public kline arrays.
func NormalizeCandleHistory(
	payload []byte,
	instrument domain.Instrument,
	interval string,
	receivedAt domain.EventTime,
) ([]exchangecontracts.Candle, error) {
	var native [][]json.RawMessage
	if err := strictDecode(payload, &native); err != nil || !supportedCandleInterval(interval) ||
		receivedAt.Validate() != nil || !validSpotInstrument(instrument) {
		return nil, historyError()
	}
	candles := make([]exchangecontracts.Candle, 0, len(native))
	for _, row := range native {
		candle, err := normalizeCandleRow(row, payload, instrument, interval, receivedAt)
		if err != nil {
			return nil, err
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

func normalizeCandleRow(
	row []json.RawMessage,
	payload []byte,
	instrument domain.Instrument,
	interval string,
	receivedAt domain.EventTime,
) (exchangecontracts.Candle, error) {
	if len(row) < 7 {
		return exchangecontracts.Candle{}, historyError()
	}
	openTime, openTimeErr := rawInt64(row[0])
	closeTime, closeTimeErr := rawInt64(row[6])
	open, openErr := rawPrice(row[1])
	high, highErr := rawPrice(row[2])
	low, lowErr := rawPrice(row[3])
	closeValue, closeErr := rawPrice(row[4])
	volume, volumeErr := rawQuantity(row[5])
	if openTimeErr != nil || closeTimeErr != nil || openErr != nil || highErr != nil ||
		lowErr != nil || closeErr != nil || volumeErr != nil || closeTime <= openTime ||
		high.Compare(open) < 0 || high.Compare(closeValue) < 0 ||
		low.Compare(open) > 0 || low.Compare(closeValue) > 0 {
		return exchangecontracts.Candle{}, historyError()
	}
	return exchangecontracts.Candle{
		Exchange:       "binance",
		Instrument:     instrument,
		Interval:       interval,
		OpenTime:       time.UnixMilli(openTime).UTC(),
		CloseTime:      time.UnixMilli(closeTime).UTC(),
		Open:           open,
		High:           high,
		Low:            low,
		Close:          closeValue,
		Volume:         volume,
		Closed:         true,
		ReceivedAt:     receivedAt,
		RawPayloadHash: payloadHash(payload),
	}, nil
}

func rawInt64(value json.RawMessage) (int64, error) {
	var result int64
	err := json.Unmarshal(value, &result)
	return result, err
}

func rawPrice(value json.RawMessage) (domain.Price, error) {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return domain.Price{}, err
	}
	return domain.ParsePrice(text)
}

func rawQuantity(value json.RawMessage) (domain.Quantity, error) {
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return domain.Quantity{}, err
	}
	return domain.ParseQuantity(text)
}

func historyError() error {
	return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
}
