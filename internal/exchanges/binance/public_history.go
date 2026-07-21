package binance

import (
	"context"
	"net/url"
	"strconv"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Instruments loads only the approved V1A universe with monotonic versions.
func (client *PublicClient) Instruments(
	ctx context.Context,
	instruments []domain.Instrument,
) ([]exchangecontracts.InstrumentRecord, error) {
	if len(instruments) == 0 || len(instruments) > 2 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
	}
	version := client.metadataVersion.Add(1)
	result := make([]exchangecontracts.InstrumentRecord, 0, len(instruments))
	seen := make(map[string]struct{}, len(instruments))
	for _, instrument := range instruments {
		if !approvedInstrument(instrument) {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
		}
		if _, duplicate := seen[instrument.Symbol()]; duplicate {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
		}
		seen[instrument.Symbol()] = struct{}{}
		query := url.Values{"showPermissionSets": {"false"}, "symbol": {instrument.Symbol()}}
		body, received, err := client.get(ctx, "/api/v3/exchangeInfo", query, exchangecontracts.OperationMetadata, 20)
		if err != nil {
			return nil, err
		}
		records, normalizeErr := NormalizeInstruments(body, received.UTC, version)
		if normalizeErr != nil {
			return records, normalizeErr
		}
		result = append(result, records...)
	}
	return result, nil
}

// Trades loads bounded recent public trades for one approved instrument.
func (client *PublicClient) Trades(
	ctx context.Context,
	request exchangecontracts.HistoryRequest,
) ([]exchangecontracts.Trade, error) {
	if !approvedInstrument(request.Instrument) || request.Limit == 0 || request.Limit > 1000 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationTrades, 0)
	}
	query := url.Values{"limit": {strconv.FormatUint(uint64(request.Limit), 10)}, "symbol": {request.Instrument.Symbol()}}
	body, received, err := client.get(ctx, "/api/v3/trades", query, exchangecontracts.OperationTrades, 25)
	if err != nil {
		return nil, err
	}
	return NormalizeTrades(body, request.Instrument, received)
}

// Candles loads bounded UTC completed 4h public candles.
func (client *PublicClient) Candles(
	ctx context.Context,
	request exchangecontracts.CandleRequest,
) ([]exchangecontracts.Candle, error) {
	if !approvedInstrument(request.Instrument) || !supportedCandleInterval(request.Interval) || request.Limit == 0 ||
		request.Limit > 1000 || request.Start.IsZero() || request.End.Before(request.Start) {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
	}
	query := url.Values{"endTime": {strconv.FormatInt(request.End.UnixMilli(), 10)}, "interval": {request.Interval},
		"limit": {strconv.FormatUint(uint64(request.Limit), 10)}, "startTime": {strconv.FormatInt(request.Start.UnixMilli(), 10)},
		"symbol": {request.Instrument.Symbol()}, "timeZone": {"0"}}
	body, received, err := client.get(ctx, "/api/v3/klines", query, exchangecontracts.OperationCandles, 2)
	if err != nil {
		return nil, err
	}
	return NormalizeCandleHistory(body, request.Instrument, request.Interval, received)
}
