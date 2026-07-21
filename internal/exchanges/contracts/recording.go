package exchangecontracts

import (
	"context"
	"time"

	"axiom/internal/domain"
)

// StreamRecordToken is the opaque link from one wire fact to its canonical outcome.
type StreamRecordToken struct {
	IngestOrdinal uint64
	PayloadHash   [32]byte
}

// PublicRecordKind classifies public wire and lifecycle evidence.
type PublicRecordKind string

// Public recording classes are shared by every credential-free adapter.
const (
	RecordStreamFrame  PublicRecordKind = "stream_frame"
	RecordSnapshot     PublicRecordKind = "snapshot"
	RecordClockSample  PublicRecordKind = "clock_sample"
	RecordLifecycle    PublicRecordKind = "lifecycle"
	RecordSubscription PublicRecordKind = "subscription"
	RecordHeartbeat    PublicRecordKind = "heartbeat"
	RecordRebuild      PublicRecordKind = "rebuild"
	RecordGap          PublicRecordKind = "gap"
	RecordDecoderError PublicRecordKind = "decoder_error"
)

// PublicRawRecord is captured before payload decoding or normalization.
type PublicRawRecord struct {
	Kind                 PublicRecordKind
	Raw                  []byte
	Instrument           domain.Instrument
	ReceivedAt           domain.EventTime
	ConnectionID         string
	ConnectionGeneration uint64
	MonotonicOffsetNanos uint64
}

// PublicCanonicalRecord links canonical bytes and native ordering evidence.
type PublicCanonicalRecord struct {
	Kind           PublicRecordKind
	Token          StreamRecordToken
	Canonical      []byte
	SourceSequence string
	ExchangeTime   *time.Time
}

// PublicRecorder persists raw bytes before their canonical outcome.
type PublicRecorder interface {
	RecordPublicRaw(context.Context, PublicRawRecord) (StreamRecordToken, error)
	RecordPublicCanonical(context.Context, PublicCanonicalRecord) error
	RecordSourceGap(context.Context, SourceGap) error
}

// SourceGap is a bounded exact-or-conservative missing source interval.
type SourceGap struct {
	Instrument           domain.Instrument
	ConnectionGeneration uint64
	FirstSequence        uint64
	LastSequence         uint64
	StartedAt            time.Time
	EndedAt              time.Time
	Reason               string
}

// ObservedStreamEvent retains exact wire and connection evidence beside a normalized event.
type ObservedStreamEvent struct {
	Raw                  []byte            `json:"raw"`
	StreamName           string            `json:"stream_name"`
	ReceivedAt           domain.EventTime  `json:"received_at"`
	ConnectionID         string            `json:"connection_id"`
	ConnectionGeneration uint64            `json:"connection_generation"`
	Event                StreamEvent       `json:"event"`
	RecordToken          StreamRecordToken `json:"record_token"`
	DecodeNanos          uint64            `json:"decode_nanos"`
	ReceivedOffsetNanos  uint64            `json:"received_offset_nanos"`
}

// ObservedStream is a bounded raw-plus-canonical public source.
type ObservedStream interface {
	Stream
	ReceiveObserved(context.Context) (ObservedStreamEvent, error)
	ConnectionID() string
	Generation() uint64
}
