package recorder

import (
	"context"
	"strconv"

	"axiom/internal/exchanges/binance"
)

const (
	binanceParserVersion     = "binance-public-parser.v1"
	binanceNormalizerVersion = "binance-public-normalizer.v1"
)

// BinanceStreamSink adapts append-before-normalize frames to Recorder.
type BinanceStreamSink struct{ recorder *Recorder }

var _ binance.PublicRecorder = (*BinanceStreamSink)(nil)

// NewBinanceStreamSink constructs a session-scoped public frame sink.
func NewBinanceStreamSink(recorder *Recorder) (*BinanceStreamSink, error) {
	if recorder == nil {
		return nil, recorderError("recorder_missing")
	}
	return &BinanceStreamSink{recorder: recorder}, nil
}

// RecordPublicRaw persists immutable wire bytes before the adapter decodes them.
func (sink *BinanceStreamSink) RecordPublicRaw(
	ctx context.Context,
	record binance.PublicRawRecord,
) (binance.StreamRecordToken, error) {
	if err := ctx.Err(); err != nil {
		return binance.StreamRecordToken{}, recorderError("record_canceled")
	}
	eventType, err := publicEventType(record.Kind)
	if err != nil {
		return binance.StreamRecordToken{}, err
	}
	link, err := sink.recorder.RecordRaw(RawInput{Exchange: sink.recorder.exchange, EventType: eventType,
		Instrument: record.Instrument, SessionID: sink.recorder.sessionID, ConnectionID: record.ConnectionID,
		ConnectionGeneration: record.ConnectionGeneration, MonotonicOffsetNanos: record.MonotonicOffsetNanos,
		RecordedLogicalTime: record.MonotonicOffsetNanos, ReceivedAt: record.ReceivedAt.UTC,
		Payload: append([]byte(nil), record.Raw...)})
	if err != nil {
		return binance.StreamRecordToken{}, err
	}
	return binance.StreamRecordToken{IngestOrdinal: link.IngestOrdinal, PayloadHash: link.PayloadHash}, nil
}

// RecordPublicCanonical links normalized bytes to their exact wire record.
func (sink *BinanceStreamSink) RecordPublicCanonical(
	_ context.Context,
	record binance.PublicCanonicalRecord,
) error {
	// Once raw append succeeds, its bounded local outcome must be completed even
	// if the source context is canceled between decode and canonical append.
	return sink.recorder.RecordCanonical(CanonicalInput{Link: RawLink(record.Token),
		EventID:       sink.recorder.sessionID + ":" + strconv.FormatUint(record.Token.IngestOrdinal, 10),
		ParserVersion: binanceParserVersion, NormalizationVersion: binanceNormalizerVersion,
		Canonical: append([]byte(nil), record.Canonical...), SourceSequence: record.SourceSequence,
		ExchangeTime: record.ExchangeTime})
}

// RecordSourceGap appends a manifest-visible missing source interval.
func (sink *BinanceStreamSink) RecordSourceGap(ctx context.Context, gap binance.SourceGap) error {
	if err := ctx.Err(); err != nil {
		return recorderError("record_canceled")
	}
	return sink.recorder.RecordGap(Gap{Exchange: sink.recorder.exchange, Instrument: gap.Instrument,
		ConnectionGeneration: gap.ConnectionGeneration, FirstSourceSequence: gap.FirstSequence,
		LastSourceSequence: gap.LastSequence, StartedAt: gap.StartedAt, EndedAt: gap.EndedAt,
		Reason: gap.Reason})
}

func publicEventType(kind binance.PublicRecordKind) (EventType, error) {
	switch kind {
	case binance.RecordStreamFrame:
		return EventStreamFrame, nil
	case binance.RecordSnapshot:
		return EventSnapshot, nil
	case binance.RecordClockSample:
		return EventClockSample, nil
	case binance.RecordLifecycle:
		return EventLifecycle, nil
	case binance.RecordSubscription:
		return EventSubscription, nil
	case binance.RecordRebuild:
		return EventRebuild, nil
	case binance.RecordGap:
		return EventGap, nil
	case binance.RecordDecoderError:
		return EventDecoderError, nil
	default:
		return "", recorderError("record_kind_invalid")
	}
}
