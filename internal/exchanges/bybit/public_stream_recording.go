package bybit

import (
	"context"
	"encoding/json"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

func (stream *publicStream) normalizeObserved(
	ctx context.Context,
	payload []byte,
) (exchangecontracts.ObservedStreamEvent, error) {
	received := stream.clock.Now()
	receivedOffset := positiveOffset(stream.monotonic())
	token, err := stream.recordIncomingRaw(ctx, payload, received, receivedOffset)
	if err != nil {
		return exchangecontracts.ObservedStreamEvent{}, err
	}
	decodeStarted := time.Now()
	name, event, normalizeErr := normalizeStream(payload, received, stream.tickers)
	observed := exchangecontracts.ObservedStreamEvent{Raw: payload, StreamName: name,
		ReceivedAt: received, ConnectionID: stream.id, ConnectionGeneration: stream.generation,
		Event: event, RecordToken: token, DecodeNanos: positiveOffset(time.Since(decodeStarted)),
		ReceivedOffsetNanos: receivedOffset}
	if normalizeErr != nil {
		return observed, stream.recordDecoderFailure(ctx, token, normalizeErr)
	}
	if event.Kind != exchangecontracts.StreamLifecycle {
		if _, ok := stream.expected[name]; !ok {
			return observed, stream.recordDecoderFailure(ctx, token, streamError())
		}
	}
	return stream.completeObserved(ctx, observed, name, event, token)
}

func (stream *publicStream) recordIncomingRaw(
	ctx context.Context,
	payload []byte,
	receivedAt domain.EventTime,
	receivedOffset uint64,
) (exchangecontracts.StreamRecordToken, error) {
	if stream.recorder == nil {
		return exchangecontracts.StreamRecordToken{}, nil
	}
	token, err := stream.recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
		Kind: exchangecontracts.RecordStreamFrame, Raw: append([]byte(nil), payload...),
		Instrument: stream.instrument, ReceivedAt: receivedAt, ConnectionID: stream.id,
		ConnectionGeneration: stream.generation, MonotonicOffsetNanos: receivedOffset})
	if err != nil {
		return exchangecontracts.StreamRecordToken{}, recorderFailure{err}
	}
	return token, nil
}

func (stream *publicStream) completeObserved(
	ctx context.Context,
	observed exchangecontracts.ObservedStreamEvent,
	name string,
	event exchangecontracts.StreamEvent,
	token exchangecontracts.StreamRecordToken,
) (exchangecontracts.ObservedStreamEvent, error) {
	if event.Lifecycle != nil {
		event.Lifecycle.ConnectionID = stream.id
		event.Lifecycle.ConnectionGeneration = stream.generation
		event.Lifecycle.Instrument = stream.instrument
		observed.Event = event
	}
	if stream.recorder == nil {
		return observed, nil
	}
	canonical, err := json.Marshal(event)
	if err != nil {
		return observed, streamError()
	}
	sequence, exchangeTime := canonicalStreamEvidence(event)
	kind := incomingRecordKind(name)
	err = stream.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
		Kind: kind, Token: token, Canonical: canonical, SourceSequence: sequence,
		ExchangeTime: exchangeTime})
	if err != nil {
		return observed, recorderFailure{err}
	}
	return observed, err
}

func incomingRecordKind(name string) exchangecontracts.PublicRecordKind {
	switch name {
	case "subscribe":
		return exchangecontracts.RecordSubscription
	case "ping", "pong":
		return exchangecontracts.RecordHeartbeat
	default:
		return exchangecontracts.RecordStreamFrame
	}
}

func (stream *publicStream) recordDecoderFailure(
	ctx context.Context,
	token exchangecontracts.StreamRecordToken,
	cause error,
) error {
	if stream.recorder == nil {
		return cause
	}
	if err := stream.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
		Kind: exchangecontracts.RecordDecoderError, Token: token,
		Canonical: []byte(`{"kind":"decoder_error"}`)}); err != nil {
		return recorderFailure{err}
	}
	return cause
}

func (stream *publicStream) sendRecorded(
	ctx context.Context,
	kind exchangecontracts.PublicRecordKind,
	payload []byte,
) error {
	stream.writeMutex.Lock()
	defer stream.writeMutex.Unlock()
	if err := stream.recordOutgoing(ctx, kind, payload); err != nil {
		return err
	}
	if err := stream.connection.Send(payload); err != nil {
		return exchangecontracts.NewDetailedError(exchangecontracts.ErrorTransient,
			exchangecontracts.OperationStream, 0, 0, "websocket_send_failure")
	}
	return nil
}

func (stream *publicStream) recordOutgoing(
	ctx context.Context,
	kind exchangecontracts.PublicRecordKind,
	payload []byte,
) error {
	if stream.recorder == nil {
		return nil
	}
	token, err := stream.recorder.RecordPublicRaw(ctx, exchangecontracts.PublicRawRecord{
		Kind: kind, Raw: payload, Instrument: stream.instrument, ReceivedAt: stream.clock.Now(),
		ConnectionID: stream.id, ConnectionGeneration: stream.generation,
		MonotonicOffsetNanos: positiveOffset(stream.monotonic())})
	if err != nil {
		return recorderFailure{err}
	}
	if err = stream.recorder.RecordPublicCanonical(ctx, exchangecontracts.PublicCanonicalRecord{
		Kind: kind, Token: token, Canonical: append([]byte(nil), payload...)}); err != nil {
		return recorderFailure{err}
	}
	return nil
}
