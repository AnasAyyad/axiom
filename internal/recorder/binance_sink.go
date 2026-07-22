package recorder

import (
	"context"
	"strconv"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

const (
	binanceParserVersion     = "binance-public-parser.v1"
	binanceNormalizerVersion = "binance-public-normalizer.v1"
)

// PublicStreamSink adapts exchange-neutral append-before-normalize facts to Recorder.
type PublicStreamSink struct {
	recorder          *Recorder
	parserVersion     string
	normalizerVersion string
}

var _ exchangecontracts.PublicRecorder = (*PublicStreamSink)(nil)

// BinanceStreamSink is the V1A compatibility name for PublicStreamSink.
type BinanceStreamSink = PublicStreamSink

// NewPublicStreamSink constructs one session-scoped exchange-neutral public sink.
func NewPublicStreamSink(recorder *Recorder, parserVersion, normalizerVersion string) (*PublicStreamSink, error) {
	if recorder == nil || parserVersion == "" || normalizerVersion == "" {
		return nil, recorderError("recorder_missing")
	}
	return &PublicStreamSink{recorder: recorder, parserVersion: parserVersion,
		normalizerVersion: normalizerVersion}, nil
}

// NewBinanceStreamSink constructs a session-scoped public frame sink.
func NewBinanceStreamSink(recorder *Recorder) (*BinanceStreamSink, error) {
	return NewPublicStreamSink(recorder, binanceParserVersion, binanceNormalizerVersion)
}

// RecordPublicRaw persists immutable wire bytes before the adapter decodes them.
func (sink *PublicStreamSink) RecordPublicRaw(
	ctx context.Context,
	record exchangecontracts.PublicRawRecord,
) (exchangecontracts.StreamRecordToken, error) {
	if err := ctx.Err(); err != nil {
		return exchangecontracts.StreamRecordToken{}, recorderError("record_canceled")
	}
	eventType, err := publicEventType(record.Kind)
	if err != nil {
		return exchangecontracts.StreamRecordToken{}, err
	}
	link, err := sink.recorder.RecordRaw(RawInput{Exchange: sink.recorder.exchange, EventType: eventType,
		Instrument: record.Instrument, SessionID: sink.recorder.sessionID, ConnectionID: record.ConnectionID,
		ConnectionGeneration: record.ConnectionGeneration, MonotonicOffsetNanos: record.MonotonicOffsetNanos,
		RecordedLogicalTime: record.MonotonicOffsetNanos, ReceivedAt: record.ReceivedAt.UTC,
		Payload: append([]byte(nil), record.Raw...)})
	if err != nil {
		return exchangecontracts.StreamRecordToken{}, err
	}
	return exchangecontracts.StreamRecordToken{IngestOrdinal: link.IngestOrdinal, PayloadHash: link.PayloadHash,
		ReceivedAt: record.ReceivedAt, MonotonicOffsetNanos: record.MonotonicOffsetNanos,
		ConnectionGeneration: record.ConnectionGeneration}, nil
}

// RecordPublicCanonical links normalized bytes to their exact wire record.
func (sink *PublicStreamSink) RecordPublicCanonical(
	_ context.Context,
	record exchangecontracts.PublicCanonicalRecord,
) error {
	// Once raw append succeeds, its bounded local outcome must be completed even
	// if the source context is canceled between decode and canonical append.
	return sink.recorder.RecordCanonical(CanonicalInput{Link: RawLink{IngestOrdinal: record.Token.IngestOrdinal,
		PayloadHash: record.Token.PayloadHash},
		EventID:       sink.recorder.sessionID + ":" + strconv.FormatUint(record.Token.IngestOrdinal, 10),
		ParserVersion: sink.parserVersion, NormalizationVersion: sink.normalizerVersion,
		Canonical: append([]byte(nil), record.Canonical...), SourceSequence: record.SourceSequence,
		ExchangeTime: record.ExchangeTime})
}

// RecordSourceGap appends a manifest-visible missing source interval.
func (sink *PublicStreamSink) RecordSourceGap(ctx context.Context, gap exchangecontracts.SourceGap) error {
	if err := ctx.Err(); err != nil {
		return recorderError("record_canceled")
	}
	return sink.recorder.RecordGap(Gap{Exchange: sink.recorder.exchange, Instrument: gap.Instrument,
		ConnectionGeneration: gap.ConnectionGeneration, FirstSourceSequence: gap.FirstSequence,
		LastSourceSequence: gap.LastSequence, StartedAt: gap.StartedAt, EndedAt: gap.EndedAt,
		Reason: gap.Reason})
}

func publicEventType(kind exchangecontracts.PublicRecordKind) (EventType, error) {
	switch kind {
	case exchangecontracts.RecordStreamFrame:
		return EventStreamFrame, nil
	case exchangecontracts.RecordSnapshot:
		return EventSnapshot, nil
	case exchangecontracts.RecordClockSample:
		return EventClockSample, nil
	case exchangecontracts.RecordLifecycle:
		return EventLifecycle, nil
	case exchangecontracts.RecordSubscription:
		return EventSubscription, nil
	case exchangecontracts.RecordHeartbeat:
		return EventHeartbeat, nil
	case exchangecontracts.RecordRebuild:
		return EventRebuild, nil
	case exchangecontracts.RecordGap:
		return EventGap, nil
	case exchangecontracts.RecordDecoderError:
		return EventDecoderError, nil
	default:
		return "", recorderError("record_kind_invalid")
	}
}
