package binance

import (
	"context"
	"net/url"
	"strconv"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Instruments loads only the approved initial trend universe with monotonic versions.
func (client *PublicClient) Instruments(
	ctx context.Context,
	instruments []domain.Instrument,
) ([]exchangecontracts.InstrumentRecord, error) {
	if len(instruments) == 0 || len(instruments) > 3 {
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
		body, received, err := client.get(ctx, "/api/v3/exchangeInfo", query,
			exchangecontracts.OperationMetadata, 20, exchangecontracts.BudgetPublic)
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
	body, received, err := client.get(ctx, "/api/v3/trades", query,
		exchangecontracts.OperationTrades, 25, exchangecontracts.BudgetPublic)
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
	page, err := client.CandlePage(ctx, request)
	if err != nil {
		return nil, err
	}
	return page.Candles, nil
}

// CandlePage preserves the official response bytes beside strict normalized
// rows for the historical evaluation importer.
func (client *PublicClient) CandlePage(
	ctx context.Context,
	request exchangecontracts.CandleRequest,
) (exchangecontracts.HistoricalCandlePage, error) {
	if !approvedInstrument(request.Instrument) || !supportedCandleInterval(request.Interval) || request.Limit == 0 ||
		request.Limit > 1000 || request.Start.IsZero() || request.End.Before(request.Start) {
		return exchangecontracts.HistoricalCandlePage{}, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
	}
	query := url.Values{"endTime": {strconv.FormatInt(request.End.UnixMilli(), 10)}, "interval": {request.Interval},
		"limit": {strconv.FormatUint(uint64(request.Limit), 10)}, "startTime": {strconv.FormatInt(request.Start.UnixMilli(), 10)},
		"symbol": {request.Instrument.Symbol()}, "timeZone": {"0"}}
	body, received, err := client.get(ctx, "/api/v3/klines", query,
		exchangecontracts.OperationCandles, 2, exchangecontracts.BudgetPublic)
	if err != nil {
		return exchangecontracts.HistoricalCandlePage{}, err
	}
	candles, err := NormalizeCandleHistory(body, request.Instrument, request.Interval, received)
	if err != nil {
		return exchangecontracts.HistoricalCandlePage{}, err
	}
	page := exchangecontracts.HistoricalCandlePage{Exchange: "binance", Instrument: request.Instrument,
		Interval: request.Interval, Start: request.Start, End: request.End, ReceivedAt: received,
		RawPayload: append([]byte(nil), body...), RawPayloadHash: payloadHash(body), Candles: candles}
	if !page.Valid() {
		return exchangecontracts.HistoricalCandlePage{}, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
	}
	return page, nil
}
