package bybit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const (
	publicBodyLimit        = 2 * 1024 * 1024
	maximumTimeUncertainty = 500 * time.Millisecond
)

// ClockHealth is the shared immutable server-clock estimate.
type ClockHealth = exchangecontracts.ClockHealth

// RateBudgetTelemetry is bounded public request-budget evidence.
type RateBudgetTelemetry struct {
	Remaining  uint64        `json:"remaining"`
	RetryAfter time.Duration `json:"retry_after"`
	Granted    bool          `json:"granted"`
}

// PublicClient is the credential-free production-public Bybit Spot client.
type PublicClient struct {
	clock            domain.Clock
	httpClient       *http.Client
	restOrigin       *url.URL
	validateREST     func(string, *url.URL, http.Header) (publicRoute, error)
	monotonic        func() time.Duration
	budget           *exchangecontracts.RateBudget
	descriptor       exchangecontracts.Descriptor
	metadataVersion  atomic.Uint64
	streamGeneration atomic.Uint64
	wsOrigin         *url.URL
	validateWS       func(*url.URL) (publicRoute, error)
	connector        websocketConnector
	telemetryMutex   sync.RWMutex
	clockEstimator   *exchangecontracts.ClockEstimator
	budgetTelemetry  RateBudgetTelemetry
}

var (
	_ exchangecontracts.MarketDataSource  = (*PublicClient)(nil)
	_ exchangecontracts.InstrumentCatalog = (*PublicClient)(nil)
	_ exchangecontracts.HistoricalReader  = (*PublicClient)(nil)
	_ exchangecontracts.CapabilitySource  = (*PublicClient)(nil)
)

// NewPublicClient accepts only the compiled Bybit public endpoint-set identifier.
func NewPublicClient(endpointSet string, clock domain.Clock) (*PublicClient, error) {
	return NewPublicClientWithMonotonic(endpointSet, clock, exchangecontracts.NewProcessMonotonicSource())
}

// NewPublicClientWithMonotonic binds Bybit to a caller-owned cross-exchange ordering epoch.
func NewPublicClientWithMonotonic(
	endpointSet string,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
) (*PublicClient, error) {
	if endpointSet != publicEndpointSet || clock == nil || monotonic == nil {
		return nil, policyError(exchangecontracts.OperationCapability)
	}
	origin, err := url.Parse(publicRESTOrigin)
	if err != nil {
		return nil, policyError(exchangecontracts.OperationCapability)
	}
	websocketOrigin, err := url.Parse(publicWSOrigin)
	if err != nil {
		return nil, policyError(exchangecontracts.OperationStream)
	}
	budget, err := exchangecontracts.NewRateBudget(exchangecontracts.BudgetConfig{
		Capacity: 600, RecoveryReserve: 50, RefillAmount: 600, RefillInterval: 5 * time.Second,
	}, 0)
	if err != nil {
		return nil, err
	}
	estimator, err := exchangecontracts.NewClockEstimator(maximumTimeUncertainty)
	if err != nil {
		return nil, err
	}
	descriptor, err := Capabilities(clock.Now().UTC)
	if err != nil {
		return nil, err
	}
	return &PublicClient{clock: clock, httpClient: newPublicHTTPClient(), restOrigin: origin,
		validateREST: validateRESTTarget, monotonic: monotonic,
		budget: budget, descriptor: descriptor, clockEstimator: estimator, wsOrigin: websocketOrigin,
		validateWS: validateWebSocketTarget, connector: newSecureWebsocketConnector()}, nil
}

// Capabilities returns a defensive public-only descriptor copy.
func (client *PublicClient) Capabilities() exchangecontracts.Descriptor {
	copy := client.descriptor
	copy.Capabilities = append([]exchangecontracts.Capability(nil), client.descriptor.Capabilities...)
	for index := range copy.Capabilities {
		constraints := client.descriptor.Capabilities[index].Constraints
		copy.Capabilities[index].Constraints = append([]exchangecontracts.Constraint(nil), constraints...)
		for constraint := range copy.Capabilities[index].Constraints {
			values := constraints[constraint].Values
			copy.Capabilities[index].Constraints[constraint].Values = append([]string(nil), values...)
		}
	}
	return copy
}

// Health returns the latest fail-closed server-time estimate.
func (client *PublicClient) Health() ClockHealth {
	return client.clockEstimator.Health()
}

// RateBudget returns the latest bounded admission result.
func (client *PublicClient) RateBudget() RateBudgetTelemetry {
	client.telemetryMutex.RLock()
	defer client.telemetryMutex.RUnlock()
	return client.budgetTelemetry
}

// MonotonicOffset returns a positive process-local duration for age checks.
func (client *PublicClient) MonotonicOffset() uint64 { return positiveOffset(client.monotonic()) }

// SampleServerTime measures midpoint offset and uncertainty from a bounded public request.
func (client *PublicClient) SampleServerTime(ctx context.Context) (ClockHealth, error) {
	health, _, err := client.sampleServerTime(ctx, domain.Instrument{}, "", 0, nil)
	return health, err
}

// SampleServerTimeRecorded records raw time evidence before its canonical estimate.
func (client *PublicClient) SampleServerTimeRecorded(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder exchangecontracts.PublicRecorder,
) (ClockHealth, exchangecontracts.StreamRecordToken, error) {
	if !approvedInstrument(instrument) || connectionID == "" || generation == 0 || recorder == nil {
		return ClockHealth{}, exchangecontracts.StreamRecordToken{}, streamError()
	}
	return client.sampleServerTime(ctx, instrument, connectionID, generation, recorder)
}

func (client *PublicClient) sampleServerTime(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder exchangecontracts.PublicRecorder,
) (ClockHealth, exchangecontracts.StreamRecordToken, error) {
	sent, sentMonotonic := client.clock.Now(), client.monotonic()
	body, received, err := client.get(ctx, "/v5/market/time", nil,
		exchangecontracts.OperationMetadata, 1)
	receivedMonotonic := client.monotonic()
	if err != nil {
		return ClockHealth{}, exchangecontracts.StreamRecordToken{}, err
	}
	token, err := client.recordRaw(ctx, recorder, exchangecontracts.PublicRawRecord{
		Kind: exchangecontracts.RecordClockSample, Raw: body, Instrument: instrument,
		ReceivedAt: received, ConnectionID: connectionID, ConnectionGeneration: generation,
		MonotonicOffsetNanos: positiveOffset(receivedMonotonic)})
	if err != nil {
		return ClockHealth{}, token, err
	}
	server, err := NormalizeServerTime(body)
	if err != nil {
		return ClockHealth{}, token, client.recordDecodeFailure(ctx, recorder, token, err)
	}
	health, estimateErr := client.clockEstimator.Observe(sent.UTC, received.UTC, server,
		sentMonotonic, receivedMonotonic)
	if estimateErr != nil {
		return ClockHealth{}, token, estimateErr
	}
	if recorder != nil {
		canonical, _ := json.Marshal(health)
		if err = recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
			Kind: exchangecontracts.RecordClockSample, Token: token, Canonical: canonical,
			ExchangeTime: &server}); err != nil {
			return ClockHealth{}, token, recorderFailure{err}
		}
	}
	return health, token, nil
}

func (client *PublicClient) recordRaw(
	ctx context.Context,
	recorder exchangecontracts.PublicRecorder,
	record exchangecontracts.PublicRawRecord,
) (exchangecontracts.StreamRecordToken, error) {
	if recorder == nil {
		return exchangecontracts.StreamRecordToken{}, nil
	}
	token, err := recorder.RecordPublicRaw(ctx, record)
	if err != nil {
		return exchangecontracts.StreamRecordToken{}, recorderFailure{err}
	}
	return token, nil
}

func (client *PublicClient) recordDecodeFailure(
	ctx context.Context,
	recorder exchangecontracts.PublicRecorder,
	token exchangecontracts.StreamRecordToken,
	cause error,
) error {
	if recorder != nil {
		if err := recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
			Kind: exchangecontracts.RecordDecoderError, Token: token,
			Canonical: boundedDecoderFailureEvidence(cause)}); err != nil {
			return recorderFailure{err}
		}
	}
	return cause
}

func positiveOffset(value time.Duration) uint64 {
	if value <= 0 {
		return 1
	}
	return uint64(value)
}
