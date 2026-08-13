package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

const (
	publicBodyLimit        = 2 * 1024 * 1024
	maximumTimeUncertainty = 500 * time.Millisecond
)

// PublicClient is the credential-free Binance Spot market-data client.
type PublicClient struct {
	clock            domain.Clock
	httpClient       *http.Client
	restOrigin       *url.URL
	validateREST     func(string, *url.URL, http.Header) (publicRoute, error)
	monotonic        func() time.Duration
	budget           *exchangecontracts.RateBudget
	timeSync         *TimeSynchronizer
	descriptor       exchangecontracts.Descriptor
	metadataVersion  atomic.Uint64
	streamGeneration atomic.Uint64
	wsOrigin         *url.URL
	validateWS       func(*url.URL) (publicRoute, error)
	connector        websocketConnector
}

var (
	_ exchangecontracts.MarketDataSource  = (*PublicClient)(nil)
	_ exchangecontracts.InstrumentCatalog = (*PublicClient)(nil)
	_ exchangecontracts.HistoricalReader  = (*PublicClient)(nil)
	_ exchangecontracts.CapabilitySource  = (*PublicClient)(nil)
)

// RestoreStreamGeneration advances a brand-new client's generation fence
// before subscriptions resume a durable recorder session.
func (client *PublicClient) RestoreStreamGeneration(last uint64) error {
	if client == nil || !client.streamGeneration.CompareAndSwap(0, last) {
		return policyError(exchangecontracts.OperationStream)
	}
	return nil
}

// NewPublicClient accepts only the compiled endpoint-set identifier and has no
// credential, signer, header, route, or arbitrary-origin input.
func NewPublicClient(endpointSet string, clock domain.Clock) (*PublicClient, error) {
	return NewPublicClientWithMonotonic(endpointSet, clock, exchangecontracts.NewProcessMonotonicSource())
}

// NewTestnetPublicClient constructs the credential-free Binance Spot Testnet
// market-data client. It has no access to private account or order routes.
func NewTestnetPublicClient(clock domain.Clock) (*PublicClient, error) {
	return NewTestnetPublicClientWithMonotonic(
		clock,
		exchangecontracts.NewProcessMonotonicSource(),
	)
}

// NewTestnetPublicClientWithMonotonic binds the Testnet public client to a
// caller-owned monotonic ordering source.
func NewTestnetPublicClientWithMonotonic(
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
) (*PublicClient, error) {
	return newPublicClientWithReserve(testnetPublicEndpointSet, clock, monotonic, 100)
}

// NewRecorderPublicClientWithMonotonic reserves enough request weight for three
// simultaneous 5,000-level recovery snapshots and their clock samples.
func NewRecorderPublicClientWithMonotonic(
	endpointSet string,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
) (*PublicClient, error) {
	return newPublicClientWithReserve(endpointSet, clock, monotonic, 768)
}

// NewPublicClientWithMonotonic binds Binance to a caller-owned cross-exchange ordering epoch.
func NewPublicClientWithMonotonic(
	endpointSet string,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
) (*PublicClient, error) {
	return newPublicClientWithReserve(endpointSet, clock, monotonic, 100)
}

func newPublicClientWithReserve(
	endpointSet string,
	clock domain.Clock,
	monotonic exchangecontracts.MonotonicSource,
	recoveryReserve uint64,
) (*PublicClient, error) {
	if clock == nil || monotonic == nil {
		return nil, policyError(exchangecontracts.OperationCapability)
	}
	endpoints, err := publicEndpoints(endpointSet)
	if err != nil {
		return nil, err
	}
	origin, err := url.Parse(endpoints.restOrigin)
	if err != nil {
		return nil, policyError(exchangecontracts.OperationCapability)
	}
	websocketOrigin, err := url.Parse(endpoints.wsOrigin)
	if err != nil {
		return nil, policyError(exchangecontracts.OperationStream)
	}
	budget, err := exchangecontracts.NewRateBudget(exchangecontracts.BudgetConfig{
		Capacity: 1200, RecoveryReserve: recoveryReserve, RefillAmount: 1200, RefillInterval: time.Minute,
	}, 0)
	if err != nil {
		return nil, err
	}
	synchronizer, err := NewTimeSynchronizer(maximumTimeUncertainty)
	if err != nil {
		return nil, err
	}
	observed := clock.Now().UTC
	descriptor, err := Capabilities(observed)
	if err != nil {
		return nil, err
	}
	return &PublicClient{clock: clock, httpClient: newPublicHTTPClientFor(endpoints.host), restOrigin: origin, wsOrigin: websocketOrigin,
		validateREST: func(method string, target *url.URL, headers http.Header) (publicRoute, error) {
			return validateRESTTargetForHost(endpoints.host, method, target, headers)
		}, monotonic: monotonic,
		validateWS: func(target *url.URL) (publicRoute, error) {
			return validateWebSocketTargetForHost(strings.TrimPrefix(endpoints.wsOrigin, "wss://"), target)
		}, connector: newSecureWebsocketConnector(),
		budget: budget, timeSync: synchronizer, descriptor: descriptor}, nil
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

// Ping checks only the compiled public connectivity route.
func (client *PublicClient) Ping(ctx context.Context) error {
	_, _, err := client.get(ctx, "/api/v3/ping", nil, exchangecontracts.OperationMetadata, 1,
		exchangecontracts.BudgetPublic)
	return err
}

// SampleServerTime updates drift and uncertainty health from one public sample.
func (client *PublicClient) SampleServerTime(ctx context.Context) (TimeHealth, error) {
	health, _, err := client.sampleServerTime(ctx, domain.Instrument{}, "", 0, nil)
	return health, err
}

// SampleServerTimeRecorded records the raw public response before decoding and
// links the canonical clock-health sample to the active generation.
func (client *PublicClient) SampleServerTimeRecorded(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (TimeHealth, StreamRecordToken, error) {
	if !approvedInstrument(instrument) || connectionID == "" || generation == 0 || recorder == nil {
		return TimeHealth{}, StreamRecordToken{}, streamError()
	}
	return client.sampleServerTime(ctx, instrument, connectionID, generation, recorder)
}

func (client *PublicClient) sampleServerTime(
	ctx context.Context,
	instrument domain.Instrument,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (TimeHealth, StreamRecordToken, error) {
	sent := client.clock.Now()
	sentMonotonic := client.monotonic()
	budgetClass := exchangecontracts.BudgetPublic
	if recorder != nil {
		budgetClass = exchangecontracts.BudgetRecovery
	}
	body, received, err := client.get(ctx, "/api/v3/time", nil, exchangecontracts.OperationMetadata, 1, budgetClass)
	receivedMonotonic := client.monotonic()
	if err != nil {
		return TimeHealth{}, StreamRecordToken{}, err
	}
	token, err := client.recordRaw(ctx, recorder, PublicRawRecord{Kind: RecordClockSample,
		Raw: body, Instrument: instrument, ReceivedAt: received, ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: positiveOffset(receivedMonotonic)})
	if err != nil {
		return TimeHealth{}, StreamRecordToken{}, err
	}
	server, err := normalizeServerTime(body)
	if err != nil {
		if recordErr := client.recordDecodeFailure(ctx, recorder, token, err, "clock_normalize"); recordErr != nil {
			return TimeHealth{}, token, recorderFailure{recordErr}
		}
		return TimeHealth{}, token, err
	}
	if err = client.timeSync.Observe(sent.UTC, received.UTC, server, sentMonotonic, receivedMonotonic); err != nil {
		return TimeHealth{}, token, err
	}
	health := client.timeSync.Health()
	if recorder != nil {
		encoded, encodeErr := json.Marshal(health)
		if encodeErr != nil {
			return TimeHealth{}, token, streamError()
		}
		if err = recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordClockSample,
			Token: token, Canonical: encoded, SourceSequence: strconv.FormatInt(server.UnixMilli(), 10),
			ExchangeTime: &server}); err != nil {
			return TimeHealth{}, token, recorderFailure{err}
		}
	}
	return health, token, nil
}

// TimeHealth returns the latest fail-closed server-time estimate.
func (client *PublicClient) TimeHealth() TimeHealth { return client.timeSync.Health() }

// ClockHealth exposes the shared exchange clock through an exchange-neutral contract.
func (client *PublicClient) ClockHealth() exchangecontracts.ClockHealth { return client.TimeHealth() }

// Snapshot loads and strictly normalizes one approved public depth snapshot.
func (client *PublicClient) Snapshot(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
) (exchangecontracts.BookSnapshot, error) {
	snapshot, _, err := client.snapshot(ctx, request, "", 0, nil)
	return snapshot, err
}

// SnapshotRecorded records raw REST bytes before strict snapshot normalization.
func (client *PublicClient) SnapshotRecorded(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (exchangecontracts.BookSnapshot, StreamRecordToken, error) {
	if connectionID == "" || generation == 0 || recorder == nil {
		return exchangecontracts.BookSnapshot{}, StreamRecordToken{}, streamError()
	}
	return client.snapshot(ctx, request, connectionID, generation, recorder)
}

func (client *PublicClient) snapshot(
	ctx context.Context,
	request exchangecontracts.SnapshotRequest,
	connectionID string,
	generation uint64,
	recorder PublicRecorder,
) (exchangecontracts.BookSnapshot, StreamRecordToken, error) {
	if !approvedInstrument(request.Instrument) || !validSnapshotDepth(request.Depth) {
		return exchangecontracts.BookSnapshot{}, StreamRecordToken{}, exchangecontracts.NewError(
			exchangecontracts.ErrorValidation, exchangecontracts.OperationSnapshot, 0,
		)
	}
	query := url.Values{"limit": {strconv.FormatUint(uint64(request.Depth), 10)}, "symbol": {request.Instrument.Symbol()}}
	budgetClass := exchangecontracts.BudgetPublic
	if recorder != nil {
		budgetClass = exchangecontracts.BudgetRecovery
	}
	body, received, err := client.get(ctx, "/api/v3/depth", query, exchangecontracts.OperationSnapshot,
		snapshotWeight(request.Depth), budgetClass)
	if err != nil {
		return exchangecontracts.BookSnapshot{}, StreamRecordToken{}, err
	}
	token, err := client.recordRaw(ctx, recorder, PublicRawRecord{Kind: RecordSnapshot, Raw: body,
		Instrument: request.Instrument, ReceivedAt: received, ConnectionID: connectionID,
		ConnectionGeneration: generation, MonotonicOffsetNanos: positiveOffset(client.monotonic())})
	if err != nil {
		return exchangecontracts.BookSnapshot{}, StreamRecordToken{}, err
	}
	snapshot, err := NormalizeSnapshot(body, request.Instrument, received)
	if err != nil {
		if recordErr := client.recordDecodeFailure(ctx, recorder, token, err, "snapshot_normalize"); recordErr != nil {
			return exchangecontracts.BookSnapshot{}, token, recorderFailure{recordErr}
		}
		return exchangecontracts.BookSnapshot{}, token, err
	}
	if recorder != nil {
		encoded, encodeErr := json.Marshal(snapshot)
		if encodeErr != nil {
			return exchangecontracts.BookSnapshot{}, token, streamError()
		}
		if err = recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordSnapshot, Token: token,
			Canonical: encoded, SourceSequence: strconv.FormatUint(snapshot.LastSequence, 10)}); err != nil {
			return exchangecontracts.BookSnapshot{}, token, recorderFailure{err}
		}
	}
	return snapshot, token, nil
}

func (client *PublicClient) recordRaw(
	ctx context.Context,
	recorder PublicRecorder,
	record PublicRawRecord,
) (StreamRecordToken, error) {
	if recorder == nil {
		return StreamRecordToken{}, nil
	}
	token, err := recorder.RecordPublicRaw(ctx, record)
	if err != nil {
		return StreamRecordToken{}, recorderFailure{err}
	}
	return token, nil
}

func (client *PublicClient) recordDecodeFailure(
	ctx context.Context,
	recorder PublicRecorder,
	token StreamRecordToken,
	cause error,
	stage string,
) error {
	if recorder == nil {
		return nil
	}
	return recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordDecoderError,
		Token: token, Canonical: boundedDecoderFailureEvidence(
			cause, stage, exchangecontracts.StreamKind(""))})
}

func positiveOffset(value time.Duration) uint64 {
	if value <= 0 {
		return 1
	}
	return uint64(value)
}

// MonotonicOffset returns the process-local elapsed clock used for staleness.
func (client *PublicClient) MonotonicOffset() uint64 { return positiveOffset(client.monotonic()) }
