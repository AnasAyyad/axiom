package bybit

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// ObservedStream extends the common recorded stream with a bounded heartbeat.
type ObservedStream interface {
	exchangecontracts.ObservedStream
	Ping(context.Context) error
}

type publicStream struct {
	connection websocketConnection
	clock      domain.Clock
	expected   map[string]exchangecontracts.StreamKind
	id         string
	generation uint64
	instrument domain.Instrument
	monotonic  func() time.Duration
	recorder   exchangecontracts.PublicRecorder
	tickers    map[string]tickerPayload
	writeMutex sync.Mutex
	closeOnce  sync.Once
}

// Subscribe opens only compiled public topics for an approved market.
func (client *PublicClient) Subscribe(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
) (exchangecontracts.Stream, error) {
	return client.SubscribeObserved(ctx, request)
}

// SubscribeObserved exposes raw frames without requiring a recorder.
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
	recorder exchangecontracts.PublicRecorder,
) (ObservedStream, error) {
	if recorder == nil {
		return nil, streamError()
	}
	return client.subscribe(ctx, request, recorder)
}

func (client *PublicClient) subscribe(
	ctx context.Context,
	request exchangecontracts.StreamRequest,
	recorder exchangecontracts.PublicRecorder,
) (ObservedStream, error) {
	if !approvedInstrument(request.Instrument) || len(request.Kinds) == 0 || len(request.Kinds) > 4 {
		return nil, streamError()
	}
	expected, topics, err := requestedTopics(request)
	if err != nil {
		return nil, err
	}
	target := *client.wsOrigin
	if _, err = client.validateWS(&target); err != nil {
		return nil, err
	}
	connection, err := client.connector.Connect(ctx, &target)
	if err != nil {
		return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_connect_failure")
	}
	generation := client.streamGeneration.Add(1)
	stream, err := client.newPublicStream(connection, request.Instrument, generation, expected, recorder)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	command, _ := json.Marshal(subscriptionCommand{Operation: "subscribe", Arguments: topics})
	if err = stream.sendRecorded(ctx, exchangecontracts.RecordSubscription, command); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return stream, nil
}

func (client *PublicClient) newPublicStream(
	connection websocketConnection,
	instrument domain.Instrument,
	generation uint64,
	expected map[string]exchangecontracts.StreamKind,
	recorder exchangecontracts.PublicRecorder,
) (*publicStream, error) {
	if generation == 0 {
		return nil, streamError()
	}
	return &publicStream{connection: connection, clock: client.clock, expected: expected,
		id: "bybit-public-" + strconv.FormatUint(generation, 10), generation: generation,
		instrument: instrument, monotonic: client.monotonic, recorder: recorder,
		tickers: make(map[string]tickerPayload)}, nil
}

// Receive returns only the normalized event for the common contract.
func (stream *publicStream) Receive(ctx context.Context) (exchangecontracts.StreamEvent, error) {
	observed, err := stream.ReceiveObserved(ctx)
	return observed.Event, err
}

// ReceiveObserved records one exact frame before decoding it.
func (stream *publicStream) ReceiveObserved(
	ctx context.Context,
) (exchangecontracts.ObservedStreamEvent, error) {
	payload, err := stream.readFrame(ctx)
	if err != nil {
		return exchangecontracts.ObservedStreamEvent{}, err
	}
	return stream.normalizeObserved(ctx, payload)
}

func (stream *publicStream) readFrame(ctx context.Context) ([]byte, error) {
	type receiveResult struct {
		payload []byte
		err     error
	}
	completed := make(chan receiveResult, 1)
	go func() {
		payload, err := stream.connection.Receive()
		completed <- receiveResult{payload: payload, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = stream.Close()
		return nil, exchangecontracts.NewError(exchangecontracts.ErrorCanceled,
			exchangecontracts.OperationStream, 0)
	case read := <-completed:
		if read.err != nil {
			return nil, exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
				exchangecontracts.OperationStream, 0, 0, "websocket_receive_failure")
		}
		return append([]byte(nil), read.payload...), nil
	}
}

// Ping records and sends one bounded Bybit public heartbeat request.
func (stream *publicStream) Ping(ctx context.Context) error {
	return stream.sendRecorded(ctx, exchangecontracts.RecordHeartbeat, []byte(`{"op":"ping"}`))
}

// ConnectionID returns the stable local source identity.
func (stream *publicStream) ConnectionID() string { return stream.id }

// Generation returns the monotonically increasing client generation.
func (stream *publicStream) Generation() uint64 { return stream.generation }

// Close idempotently closes the public connection.
func (stream *publicStream) Close() error {
	var err error
	stream.closeOnce.Do(func() { err = stream.connection.Close() })
	return err
}

type subscriptionCommand struct {
	Operation string   `json:"op"`
	Arguments []string `json:"args"`
}
