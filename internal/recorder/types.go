package recorder

import (
	"errors"
	"syscall"
	"time"

	"axiom/internal/domain"
	"axiom/internal/storage/segments"
)

const (
	datasetSchemaVersion = "axiom.dataset.v1"
	maximumEventBytes    = 2 * 1024 * 1024
	maximumPendingBytes  = 512 * 1024 * 1024
	recordMemoryOverhead = 1024
)

// EventType identifies one recorded public market or lifecycle fact.
type EventType string

// A7 recorder event classes.
const (
	EventDepth         EventType = "depth"
	EventTrade         EventType = "trade"
	EventCandle        EventType = "candle"
	EventSnapshot      EventType = "snapshot"
	EventLifecycle     EventType = "lifecycle"
	EventSubscription  EventType = "subscription"
	EventHeartbeat     EventType = "heartbeat"
	EventGap           EventType = "gap"
	EventRebuild       EventType = "rebuild"
	EventDecoderError  EventType = "decoder_error"
	EventClockSample   EventType = "clock_sample"
	EventStreamFrame   EventType = "stream_frame"
	EventDecisionInput EventType = "decision_input"
)

// DecisionInput is one exact in-process strategy/model input. Its raw and
// canonical bytes are identical because no external wire parser is involved.
type DecisionInput struct {
	Instrument  domain.Instrument
	EventID     string
	LogicalTime uint64
	ReceivedAt  time.Time
	Payload     []byte
}

// DecisionInputBuilder binds the payload's ordinal to the recorder-assigned ordinal.
type DecisionInputBuilder func(uint64) ([]byte, error)

// RawInput is one immutable wire envelope recorded before normalization.
type RawInput struct {
	Exchange             string
	EventType            EventType
	Instrument           domain.Instrument
	SessionID            string
	ConnectionID         string
	ConnectionGeneration uint64
	MonotonicOffsetNanos uint64
	RecordedLogicalTime  uint64
	SourceSequence       string
	ExchangeTime         *time.Time
	ReceivedAt           time.Time
	Payload              []byte
}

// RawLink identifies the exact source row for canonical normalization.
type RawLink struct {
	IngestOrdinal uint64   `json:"ingest_ordinal"`
	PayloadHash   [32]byte `json:"payload_hash"`
}

// CanonicalInput links normalized bytes to an existing immutable raw row.
type CanonicalInput struct {
	Link                 RawLink
	EventID              string
	ParserVersion        string
	NormalizationVersion string
	Canonical            []byte
	SourceSequence       string
	ExchangeTime         *time.Time
}

// Gap is an explicit source-sequence hole or recorder loss interval.
type Gap struct {
	Exchange             string            `json:"exchange"`
	Instrument           domain.Instrument `json:"instrument"`
	ConnectionGeneration uint64            `json:"connection_generation"`
	FirstSourceSequence  uint64            `json:"first_source_sequence"`
	LastSourceSequence   uint64            `json:"last_source_sequence"`
	StartedAt            time.Time         `json:"started_at"`
	EndedAt              time.Time         `json:"ended_at"`
	Reason               string            `json:"reason"`
}

// SegmentReference binds one dataset revision to an immutable segment proof.
type SegmentReference struct {
	Kind     string            `json:"kind"`
	Manifest segments.Manifest `json:"manifest"`
}

// DatasetManifest is one immutable cumulative dataset revision.
type DatasetManifest struct {
	SchemaVersion  string             `json:"schema_version"`
	DatasetID      string             `json:"dataset_id"`
	SessionID      string             `json:"session_id"`
	Exchange       string             `json:"exchange"`
	Revision       uint64             `json:"revision"`
	PreviousHash   string             `json:"previous_hash,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	Segments       []SegmentReference `json:"segments"`
	Gaps           []Gap              `json:"gaps"`
	RawRecordCount uint64             `json:"raw_record_count"`
	CanonicalCount uint64             `json:"canonical_record_count"`
	Complete       bool               `json:"complete"`
	Hash           string             `json:"hash"`
}

// Error is a bounded recorder failure without paths, payloads, or arbitrary
// operating-system messages. Stage and Cause retain the safe failure boundary
// needed by qualification evidence.
type Error struct {
	Code  string `json:"code"`
	Stage string `json:"stage,omitempty"`
	Cause string `json:"cause,omitempty"`
	Class string `json:"class,omitempty"`
	Errno int    `json:"errno,omitempty"`
}

// Error returns a bounded recorder reason code.
func (failure *Error) Error() string { return "recorder:" + failure.Code }

func recorderError(code string) error { return &Error{Code: code} }

func recorderStageError(code, stage, cause, class string, errno int) error {
	return &Error{Code: code, Stage: stage, Cause: cause, Class: class, Errno: errno}
}

// FailureDetail returns a defensive, sanitized recorder failure description.
func FailureDetail(err error) (Error, bool) {
	var failure *Error
	if !errors.As(err, &failure) || failure == nil {
		return Error{}, false
	}
	return *failure, true
}

func recorderIOError(code, stage string, err error) error {
	cause := "filesystem_failure"
	switch {
	case errors.Is(err, syscall.ENOSPC):
		cause = "disk_full"
	case errors.Is(err, syscall.EDQUOT):
		cause = "quota_exceeded"
	case errors.Is(err, syscall.EIO):
		cause = "io_failure"
	case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE):
		cause = "file_descriptor_exhausted"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		cause = "permission_denied"
	case errors.Is(err, syscall.EROFS):
		cause = "read_only_filesystem"
	case errors.Is(err, syscall.EEXIST):
		cause = "path_collision"
	case errors.Is(err, syscall.ENOENT):
		cause = "path_unavailable"
	}
	var errno syscall.Errno
	errnoValue := 0
	if errors.As(err, &errno) {
		errnoValue = int(errno)
	}
	return recorderStageError(code, stage, cause, "filesystem", errnoValue)
}

func recorderFinalizeError(code, stage string, err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return recorderIOError(code, stage, err)
	}
	return recorderStageError(code, stage, boundedCause(err), "storage", 0)
}
