package bybit

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Snapshot loads a bounded public order-book replacement.
func (client *PublicClient) Snapshot(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	snapshot, _, err := client.snapshot(ctx, request, "", 0, nil)
	return snapshot, err
}

// SnapshotRecorded records the raw public response before canonical decoding.
func (client *PublicClient) SnapshotRecorded(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
	connectionID string,
	generation uint64,
	recorder exchangecontracts.PublicRecorder,
) (exchangecontracts.BookSnapshot, exchangecontracts.StreamRecordToken, error) {
	if connectionID == "" || generation == 0 || recorder == nil {
		return exchangecontracts.BookSnapshot{}, exchangecontracts.StreamRecordToken{}, streamError()
	}
	return client.snapshot(ctx, request, connectionID, generation, recorder)
}

func (client *PublicClient) snapshot(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
	connectionID string,
	generation uint64,
	recorder exchangecontracts.PublicRecorder,
) (exchangecontracts.BookSnapshot, exchangecontracts.StreamRecordToken, error) {
	if !approvedInstrument(request.Instrument) || !validSnapshotDepth(request.Depth) {
		return exchangecontracts.BookSnapshot{}, exchangecontracts.StreamRecordToken{},
			validationError(exchangecontracts.OperationSnapshot)
	}
	query := url.Values{"category": {"spot"}, "limit": {strconv.FormatUint(uint64(request.Depth), 10)},
		"symbol": {request.Instrument.Symbol()}}
	body, received, err := client.get(ctx, "/v5/market/orderbook", query,
		exchangecontracts.OperationSnapshot, 5)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, exchangecontracts.StreamRecordToken{}, err
	}
	token, err := client.recordRaw(ctx, recorder, exchangecontracts.PublicRawRecord{
		Kind: exchangecontracts.RecordSnapshot, Raw: body, Instrument: request.Instrument,
		ReceivedAt: received, ConnectionID: connectionID, ConnectionGeneration: generation,
		MonotonicOffsetNanos: client.MonotonicOffset()})
	if err != nil {
		return exchangecontracts.BookSnapshot{}, token, err
	}
	snapshot, err := NormalizeSnapshot(body, request.Instrument, received)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, token, client.recordDecodeFailure(ctx, recorder, token, err)
	}
	if recorder != nil {
		canonical, _ := jsonMarshal(snapshot)
		if err = recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
			Kind: exchangecontracts.RecordSnapshot, Token: token, Canonical: canonical,
			SourceSequence: strconv.FormatUint(snapshot.LastSequence, 10)}); err != nil {
			return exchangecontracts.BookSnapshot{}, token, err
		}
	}
	return snapshot, token, nil
}

// Instruments loads only the approved B1 universe with monotonic versions.
func (client *PublicClient) Instruments(
	ctx context.Context,
	instruments []domain.Instrument,
) ([]exchangecontracts.InstrumentRecord, error) {
	if len(instruments) == 0 || len(instruments) > 3 {
		return nil, validationError(exchangecontracts.OperationMetadata)
	}
	version := client.metadataVersion.Add(1)
	result := make([]exchangecontracts.InstrumentRecord, 0, len(instruments))
	seen := make(map[string]struct{}, len(instruments))
	for _, instrument := range instruments {
		if !approvedInstrument(instrument) {
			return nil, validationError(exchangecontracts.OperationMetadata)
		}
		if _, duplicate := seen[instrument.Symbol()]; duplicate {
			return nil, validationError(exchangecontracts.OperationMetadata)
		}
		seen[instrument.Symbol()] = struct{}{}
		query := url.Values{"category": {"spot"}, "symbol": {instrument.Symbol()}}
		body, received, err := client.get(ctx, "/v5/market/instruments-info", query,
			exchangecontracts.OperationMetadata, 1)
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
		return nil, validationError(exchangecontracts.OperationTrades)
	}
	query := url.Values{"category": {"spot"}, "limit": {strconv.FormatUint(uint64(request.Limit), 10)},
		"symbol": {request.Instrument.Symbol()}}
	body, received, err := client.get(ctx, "/v5/market/recent-trade", query,
		exchangecontracts.OperationTrades, 1)
	if err != nil {
		return nil, err
	}
	return NormalizeTrades(body, request.Instrument, received)
}

// Candles loads bounded UTC public candles for a configured B1 interval.
func (client *PublicClient) Candles(
	ctx context.Context,
	request exchangecontracts.CandleRequest,
) ([]exchangecontracts.Candle, error) {
	nativeInterval, ok := intervalNative(request.Interval)
	if !approvedInstrument(request.Instrument) || !ok || request.Limit == 0 || request.Limit > 1000 ||
		request.Start.IsZero() || request.End.IsZero() || request.Start.Location() != time.UTC ||
		request.End.Location() != time.UTC || request.End.Before(request.Start) {
		return nil, validationError(exchangecontracts.OperationCandles)
	}
	query := url.Values{"category": {"spot"}, "end": {strconv.FormatInt(request.End.UnixMilli(), 10)},
		"interval": {nativeInterval}, "limit": {strconv.FormatUint(uint64(request.Limit), 10)},
		"start": {strconv.FormatInt(request.Start.UnixMilli(), 10)}, "symbol": {request.Instrument.Symbol()}}
	body, received, err := client.get(ctx, "/v5/market/kline", query,
		exchangecontracts.OperationCandles, 1)
	if err != nil {
		return nil, err
	}
	return NormalizeCandleHistory(body, request.Instrument, request.Interval, received)
}

// Ticker loads one complete public best-price observation.
func (client *PublicClient) Ticker(
	ctx context.Context,
	instrument domain.Instrument,
) (exchangecontracts.Ticker, error) {
	if !approvedInstrument(instrument) {
		return exchangecontracts.Ticker{}, validationError(exchangecontracts.OperationTicker)
	}
	query := url.Values{"category": {"spot"}, "symbol": {instrument.Symbol()}}
	body, received, err := client.get(ctx, "/v5/market/tickers", query,
		exchangecontracts.OperationTicker, 1)
	if err != nil {
		return exchangecontracts.Ticker{}, err
	}
	return NormalizeTicker(body, instrument, received)
}

func validSnapshotDepth(depth uint32) bool {
	return depth == 1 || depth == 50 || depth == 200 || depth == 1000
}

func jsonMarshal(value any) ([]byte, error) { return json.Marshal(value) }
