package emulator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	exchangecontracts "axiom/internal/exchanges/contracts"

	"golang.org/x/net/websocket"
)

const responseLimit = 2 * 1024 * 1024

// Adapter is the credential-free conformance implementation backed by Server.
type Adapter struct {
	server     *Server
	client     *http.Client
	clock      domain.Clock
	descriptor exchangecontracts.Descriptor
}

var (
	_ exchangecontracts.MarketDataSource  = (*Adapter)(nil)
	_ exchangecontracts.InstrumentCatalog = (*Adapter)(nil)
	_ exchangecontracts.HistoricalReader  = (*Adapter)(nil)
	_ exchangecontracts.CapabilitySource  = (*Adapter)(nil)
)

// NewAdapter constructs an emulator-only public adapter without arbitrary URLs.
func NewAdapter(server *Server, clock domain.Clock, observedAt time.Time) (*Adapter, error) {
	if server == nil || clock == nil {
		return nil, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationCapability, 0,
		)
	}
	descriptor, err := binance.Capabilities(observedAt)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: rejectRedirect}
	return &Adapter{server: server, client: client, clock: clock, descriptor: descriptor}, nil
}

// Capabilities returns a defensive copy of the public-only descriptor.
func (adapter *Adapter) Capabilities() exchangecontracts.Descriptor {
	descriptor := adapter.descriptor
	descriptor.Capabilities = append([]exchangecontracts.Capability(nil), adapter.descriptor.Capabilities...)
	for index := range descriptor.Capabilities {
		constraints := adapter.descriptor.Capabilities[index].Constraints
		descriptor.Capabilities[index].Constraints = append([]exchangecontracts.Constraint(nil), constraints...)
		for constraintIndex := range descriptor.Capabilities[index].Constraints {
			values := constraints[constraintIndex].Values
			descriptor.Capabilities[index].Constraints[constraintIndex].Values = append([]string(nil), values...)
		}
	}
	return descriptor
}

// Snapshot loads and immediately normalizes one scripted public depth response.
func (adapter *Adapter) Snapshot(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	if !validDepth(request.Depth) || !validInstrument(request.Instrument) {
		return exchangecontracts.BookSnapshot{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationSnapshot, 0,
		)
	}
	query := url.Values{"limit": {strconv.FormatUint(uint64(request.Depth), 10)}, "symbol": {request.Instrument.Symbol()}}
	body, err := adapter.get(ctx, "/api/v3/depth", query, exchangecontracts.OperationSnapshot)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, err
	}
	return binance.NormalizeSnapshot(body, request.Instrument, adapter.clock.Now())
}

// Instruments loads and immediately normalizes requested public metadata.
func (adapter *Adapter) Instruments(
	ctx context.Context,
	instruments []domain.Instrument,
) ([]exchangecontracts.InstrumentRecord, error) {
	if len(instruments) == 0 {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
	}
	records := make([]exchangecontracts.InstrumentRecord, 0, len(instruments))
	for _, instrument := range instruments {
		if !validInstrument(instrument) {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationMetadata, 0)
		}
		query := url.Values{"showPermissionSets": {"false"}, "symbol": {instrument.Symbol()}}
		body, err := adapter.get(ctx, "/api/v3/exchangeInfo", query, exchangecontracts.OperationMetadata)
		if err != nil {
			return nil, err
		}
		normalized, err := binance.NormalizeInstruments(body, adapter.clock.Now().UTC, 1)
		if err != nil {
			return normalized, err
		}
		records = append(records, normalized...)
	}
	return records, nil
}

// Trades loads and immediately normalizes bounded public trade history.
func (adapter *Adapter) Trades(
	ctx context.Context,
	request exchangecontracts.HistoryRequest,
) ([]exchangecontracts.Trade, error) {
	if err := validateHistory(request); err != nil {
		return nil, err
	}
	query := url.Values{"limit": {strconv.FormatUint(uint64(request.Limit), 10)}, "symbol": {request.Instrument.Symbol()}}
	body, err := adapter.get(ctx, "/api/v3/trades", query, exchangecontracts.OperationTrades)
	if err != nil {
		return nil, err
	}
	return binance.NormalizeTrades(body, request.Instrument, adapter.clock.Now())
}

// Candles loads and immediately normalizes bounded completed public candles.
func (adapter *Adapter) Candles(
	ctx context.Context,
	request exchangecontracts.CandleRequest,
) ([]exchangecontracts.Candle, error) {
	if err := validateHistory(request.HistoryRequest); err != nil || request.Interval != "4h" {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationCandles, 0)
	}
	query := url.Values{
		"endTime": {strconv.FormatInt(request.End.UnixMilli(), 10)}, "interval": {request.Interval},
		"limit": {strconv.FormatUint(uint64(request.Limit), 10)}, "startTime": {strconv.FormatInt(request.Start.UnixMilli(), 10)},
		"symbol": {request.Instrument.Symbol()}, "timeZone": {"0"},
	}
	body, err := adapter.get(ctx, "/api/v3/klines", query, exchangecontracts.OperationCandles)
	if err != nil {
		return nil, err
	}
	return binance.NormalizeCandleHistory(body, request.Instrument, request.Interval, adapter.clock.Now())
}

// Subscribe opens one normalized public stream from the local emulator.
func (adapter *Adapter) Subscribe(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	if len(request.Kinds) != 1 || request.Kinds[0] != exchangecontracts.StreamDepth ||
		!validInstrument(request.Instrument) {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationStream, 0)
	}
	path := "/ws/" + lowerSymbol(request.Instrument.Symbol()) + "@depth"
	connection, err := websocket.Dial(adapter.server.WebSocketURL()+path, "", adapter.server.URL())
	if err != nil {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient, exchangecontracts.OperationStream, 0)
	}
	return &stream{connection: connection, clock: adapter.clock, kind: request.Kinds[0]}, nil
}

func (adapter *Adapter) get(
	ctx context.Context,
	path string,
	query url.Values,
	operation exchangecontracts.Operation,
) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, adapter.server.URL()+path+"?"+query.Encode(), nil)
	if err != nil {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, exchangecontracts.NewError(exchangecontracts.ErrorCanceled, operation, 0)
		}
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorTransient, operation, 0)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, responseLimit+1))
	if readErr != nil || len(body) > responseLimit {
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, statusError(response, operation)
	}
	return body, nil
}

func validateHistory(request exchangecontracts.HistoryRequest) error {
	if !validInstrument(request.Instrument) || request.Limit == 0 || request.Limit > 1000 ||
		request.Start.IsZero() || request.End.IsZero() || request.Start.Location() != time.UTC ||
		request.End.Location() != time.UTC || !request.Start.Before(request.End) {
		return exchangecontracts.NewError(exchangecontracts.ErrorValidation, exchangecontracts.OperationTrades, 0)
	}
	return nil
}

func validInstrument(instrument domain.Instrument) bool {
	validated, err := domain.NewSpotInstrument(instrument.Base, instrument.Quote)
	return err == nil && validated == instrument
}

func validDepth(depth uint32) bool {
	return depth == 100 || depth == 500 || depth == 1000 || depth == 5000
}

func statusError(response *http.Response, operation exchangecontracts.Operation) error {
	if response.StatusCode == http.StatusTooManyRequests {
		retryAfter, _ := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64)
		return exchangecontracts.NewError(exchangecontracts.ErrorRateLimit, operation, time.Duration(retryAfter)*time.Second)
	}
	if response.StatusCode >= 500 {
		return exchangecontracts.NewError(exchangecontracts.ErrorTransient, operation, 0)
	}
	return exchangecontracts.NewError(exchangecontracts.ErrorValidation, operation, 0)
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error { return fmt.Errorf("redirect_rejected") }

func lowerSymbol(symbol string) string {
	result := make([]byte, len(symbol))
	for index := range symbol {
		value := symbol[index]
		if value >= 'A' && value <= 'Z' {
			value += 'a' - 'A'
		}
		result[index] = value
	}
	return string(result)
}
