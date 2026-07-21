package binance

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// Compatibility aliases preserve the V1A Binance API while recording facts are
// now owned by the exchange-neutral consumer contract.
type ObservedStreamEvent = exchangecontracts.ObservedStreamEvent

// StreamRecordToken is the V1A name for the common recording link.
type StreamRecordToken = exchangecontracts.StreamRecordToken

// PublicRecordKind is the V1A name for a common public recording class.
type PublicRecordKind = exchangecontracts.PublicRecordKind

// PublicRawRecord is the V1A name for a common raw public fact.
type PublicRawRecord = exchangecontracts.PublicRawRecord

// PublicCanonicalRecord is the V1A name for a common canonical public fact.
type PublicCanonicalRecord = exchangecontracts.PublicCanonicalRecord

// PublicRecorder is the V1A name for the common public recorder contract.
type PublicRecorder = exchangecontracts.PublicRecorder

// SourceGap is the V1A name for a common public source gap.
type SourceGap = exchangecontracts.SourceGap

// ObservedStream is the V1A name for the common raw-plus-canonical stream.
type ObservedStream = exchangecontracts.ObservedStream

// V1A public recording constants remain aliases of the common contract.
const (
	// RecordStreamFrame preserves the V1A stream-frame constant.
	RecordStreamFrame = exchangecontracts.RecordStreamFrame
	// RecordSnapshot preserves the V1A snapshot constant.
	RecordSnapshot = exchangecontracts.RecordSnapshot
	// RecordClockSample preserves the V1A clock-sample constant.
	RecordClockSample = exchangecontracts.RecordClockSample
	// RecordLifecycle preserves the V1A lifecycle constant.
	RecordLifecycle = exchangecontracts.RecordLifecycle
	// RecordSubscription preserves the V1A subscription constant.
	RecordSubscription = exchangecontracts.RecordSubscription
	// RecordHeartbeat exposes the common heartbeat recording class.
	RecordHeartbeat = exchangecontracts.RecordHeartbeat
	// RecordRebuild preserves the V1A rebuild constant.
	RecordRebuild = exchangecontracts.RecordRebuild
	// RecordGap preserves the V1A gap constant.
	RecordGap = exchangecontracts.RecordGap
	// RecordDecoderError preserves the V1A decoder-error constant.
	RecordDecoderError = exchangecontracts.RecordDecoderError
)

type publicStream struct {
	connection websocketConnection
	clock      domain.Clock
	expected   map[string]exchangecontracts.StreamKind
	id         string
	generation uint64
	instrument domain.Instrument
	monotonic  func() time.Duration
	recorder   PublicRecorder
	closeOnce  sync.Once
}

// Subscribe opens only compiled public stream names for an approved market.
func (client *PublicClient) Subscribe(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	return client.SubscribeObserved(ctx, request)
}

// SubscribeObserved exposes raw frames for append-before-normalize recording.
func (client *PublicClient) SubscribeObserved(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
) (ObservedStream, error) {
	return client.subscribe(ctx, request, nil)
}

// SubscribeRecorded requires raw persistence before normalization.
func (client *PublicClient) SubscribeRecorded(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
	recorder PublicRecorder,
) (ObservedStream, error) {
	if recorder == nil {
		return nil, streamError()
	}
	return client.subscribe(ctx, request, recorder)
}

func (client *PublicClient) subscribe(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
	recorder PublicRecorder,
) (ObservedStream, error) {
	if !approvedInstrument(request.Instrument) || len(request.Kinds) == 0 || len(request.Kinds) > 4 {
		return nil, streamError()
	}
	expected, names, err := requestedStreams(request)
	if err != nil {
		return nil, err
	}
	target := *client.wsOrigin
	target.Path = "/stream"
	target.RawQuery = url.Values{"streams": {strings.Join(names, "/")}}.Encode()
	if _, err = client.validateWS(&target); err != nil {
		return nil, err
	}
	connection, err := client.connector.Connect(ctx, &target)
	if err != nil {
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_connect_failure")
	}
	generation := client.streamGeneration.Add(1)
	if generation == 0 {
		_ = connection.Close()
		return nil, streamError()
	}
	return &publicStream{connection: connection, clock: client.clock, expected: expected,
		id: "binance-public-" + strconv.FormatUint(generation, 10), generation: generation,
		instrument: request.Instrument, monotonic: client.monotonic, recorder: recorder}, nil
}

// Receive returns only the normalized event for the shared A6 contract.
func (stream *publicStream) Receive(ctx context.Context) (exchangecontracts.StreamEvent, error) {
	observed, err := stream.ReceiveObserved(ctx)
	return observed.Event, err
}

// ReceiveObserved reads one bounded frame and normalizes it immediately.
func (stream *publicStream) ReceiveObserved(ctx context.Context) (ObservedStreamEvent, error) {
	read, err := stream.readFrame(ctx)
	if err != nil {
		return ObservedStreamEvent{}, err
	}
	return stream.normalizeRead(ctx, read)
}

func (stream *publicStream) readFrame(ctx context.Context) (streamRead, error) {
	result := make(chan streamRead, 1)
	go func() {
		payload, err := stream.connection.Receive()
		if err != nil {
			result <- streamRead{err: err}
			return
		}
		result <- streamRead{payload: append([]byte(nil), payload...)}
	}()
	select {
	case <-ctx.Done():
		_ = stream.Close()
		return streamRead{}, exchangecontracts.NewError(exchangecontracts.ErrorCanceled, exchangecontracts.OperationStream, 0)
	case read := <-result:
		if read.err != nil {
			return streamRead{}, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
				exchangecontracts.OperationStream, 0, 0, "websocket_receive_failure")
		}
		return read, nil
	}
}

func (stream *publicStream) normalizeRead(ctx context.Context, read streamRead) (ObservedStreamEvent, error) {
	received, receivedOffset := stream.clock.Now(), positiveOffset(stream.monotonic())
	var token StreamRecordToken
	if stream.recorder != nil {
		var err error
		token, err = stream.recorder.RecordPublicRaw(ctx, PublicRawRecord{Kind: RecordStreamFrame,
			Raw: append([]byte(nil), read.payload...), Instrument: stream.instrument, ReceivedAt: received,
			ConnectionID: stream.id, ConnectionGeneration: stream.generation, MonotonicOffsetNanos: receivedOffset})
		if err != nil {
			return ObservedStreamEvent{}, recorderFailure{err}
		}
	}
	decodeStarted := time.Now()
	name, event, err := normalizeCombinedStream(read.payload, stream.expected, received)
	decodeNanos := positiveOffset(time.Since(decodeStarted))
	observed := ObservedStreamEvent{Raw: read.payload, StreamName: name, ReceivedAt: received,
		ConnectionID: stream.id, ConnectionGeneration: stream.generation, Event: event,
		RecordToken: token, DecodeNanos: decodeNanos, ReceivedOffsetNanos: receivedOffset}
	if err != nil {
		if stream.recorder != nil {
			if recordErr := stream.recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordDecoderError,
				Token: token, Canonical: []byte(`{"kind":"decoder_error"}`)}); recordErr != nil {
				return observed, recorderFailure{recordErr}
			}
		}
		return observed, err
	}
	if stream.recorder == nil {
		return observed, nil
	}
	encoded, encodeErr := json.Marshal(event)
	if encodeErr != nil {
		return ObservedStreamEvent{}, streamError()
	}
	sequence, exchangeTime := canonicalStreamEvidence(event)
	if err = stream.recorder.RecordPublicCanonical(ctx, PublicCanonicalRecord{Kind: RecordStreamFrame,
		Token: token, Canonical: encoded, SourceSequence: sequence, ExchangeTime: exchangeTime}); err != nil {
		return ObservedStreamEvent{}, recorderFailure{err}
	}
	return observed, nil
}

func canonicalStreamEvidence(event exchangecontracts.StreamEvent) (string, *time.Time) {
	switch event.Kind {
	case exchangecontracts.StreamDepth:
		if event.Depth != nil {
			value := event.Depth.ExchangeTime
			return strconv.FormatUint(event.Depth.LastSequence, 10), &value
		}
	case exchangecontracts.StreamTrades:
		if event.Trade != nil {
			value := event.Trade.ExchangeTime
			return event.Trade.NativeID, &value
		}
	case exchangecontracts.StreamTicker:
		if event.Ticker != nil {
			value := event.Ticker.ExchangeTime
			return strconv.FormatInt(value.UnixMilli(), 10), &value
		}
	case exchangecontracts.StreamCandle:
		if event.Candle != nil {
			value := event.Candle.CloseTime
			return strconv.FormatInt(event.Candle.CloseTime.UnixMilli(), 10), &value
		}
	}
	return "", nil
}

// ConnectionID returns the stable local source connection identity.
func (stream *publicStream) ConnectionID() string { return stream.id }

// Generation returns the monotonically increasing client generation.
func (stream *publicStream) Generation() uint64 { return stream.generation }

// Close idempotently closes the public connection.
func (stream *publicStream) Close() error {
	var err error
	stream.closeOnce.Do(func() { err = stream.connection.Close() })
	return err
}

type streamRead struct {
	payload []byte
	err     error
}

func requestedStreams(request exchangecontracts.StreamRequest) (
	map[string]exchangecontracts.StreamKind,
	[]string,
	error,
) {
	prefix := strings.ToLower(request.Instrument.Symbol())
	expected := make(map[string]exchangecontracts.StreamKind, len(request.Kinds)+len(request.CandleIntervals))
	names := make([]string, 0, len(request.Kinds)+len(request.CandleIntervals))
	for _, kind := range request.Kinds {
		var suffixes []string
		switch kind {
		case exchangecontracts.StreamDepth:
			suffixes = []string{"depth@100ms"}
		case exchangecontracts.StreamTrades:
			suffixes = []string{"trade"}
		case exchangecontracts.StreamTicker:
			suffixes = []string{"bookTicker"}
		case exchangecontracts.StreamCandle:
			intervals := request.CandleIntervals
			if len(intervals) == 0 {
				intervals = []string{"4h"}
			}
			if !validCandleIntervals(intervals) {
				return nil, nil, streamError()
			}
			for _, interval := range intervals {
				suffixes = append(suffixes, "kline_"+interval)
			}
		default:
			return nil, nil, streamError()
		}
		for _, suffix := range suffixes {
			name := prefix + "@" + suffix
			if _, duplicate := expected[name]; duplicate {
				return nil, nil, streamError()
			}
			expected[name] = kind
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return expected, names, nil
}
