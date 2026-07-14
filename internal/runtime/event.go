package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/domain"
)

// LogicalTime is deterministic nanosecond-scale runtime time from a run epoch.
type LogicalTime uint64

// OptionalUint64 represents a source field that may be unavailable.
type OptionalUint64 struct {
	Present bool   `json:"present"`
	Value   uint64 `json:"value"`
}

// OptionalTime represents an exchange timestamp that may be unavailable.
type OptionalTime struct {
	Present bool      `json:"present"`
	Value   time.Time `json:"value"`
}

// RunMetadata fixes the versions that participate in deterministic identity.
type RunMetadata struct {
	ConfigurationHash string `json:"configuration_hash"`
	BuildCommit       string `json:"build_commit"`
	OrderingVersion   string `json:"ordering_version"`
	SchedulerVersion  string `json:"scheduler_version"`
}

type envelopeRecord struct {
	SchemaVersion        string                 `json:"schema_version"`
	ParserVersion        string                 `json:"parser_version"`
	ID                   domain.EventID         `json:"id"`
	RunID                domain.RunID           `json:"run_id"`
	SourceSessionID      domain.SourceSessionID `json:"source_session_id"`
	ConnectionID         domain.ConnectionID    `json:"connection_id"`
	ConnectionGeneration uint64                 `json:"connection_generation"`
	Exchange             string                 `json:"exchange"`
	Instrument           domain.Instrument      `json:"instrument"`
	ExchangeTime         OptionalTime           `json:"exchange_time"`
	SourceSequence       OptionalUint64         `json:"source_sequence"`
	ReceivedAt           domain.EventTime       `json:"received_at"`
	RecordedLogicalTime  LogicalTime            `json:"recorded_logical_time"`
	IngestOrdinal        uint64                 `json:"ingest_ordinal"`
	PayloadHash          string                 `json:"payload_hash"`
	Partition            string                 `json:"partition"`
	Run                  RunMetadata            `json:"run"`
}

// Envelope is an immutable canonical event envelope.
type Envelope struct{ record envelopeRecord }

// EnvelopeInput contains all mandatory evidence required to construct an event.
type EnvelopeInput = envelopeRecord

// NewEnvelope validates and defensively constructs an immutable envelope.
func NewEnvelope(input EnvelopeInput) (Envelope, error) {
	if err := validateEnvelope(input); err != nil {
		return Envelope{}, err
	}
	return Envelope{record: input}, nil
}

// CanonicalJSON returns deterministic JSON for hashing and durable recording.
func (envelope Envelope) CanonicalJSON() ([]byte, error) {
	return json.Marshal(envelope.record)
}

// ID returns the stable event identifier.
func (envelope Envelope) ID() domain.EventID { return envelope.record.ID }

// LogicalTime returns the recorded replay time.
func (envelope Envelope) LogicalTime() LogicalTime { return envelope.record.RecordedLogicalTime }

// IngestOrdinal returns the pre-fan-out session-local order.
func (envelope Envelope) IngestOrdinal() uint64 { return envelope.record.IngestOrdinal }

// ExchangeTime returns optional exchange evidence.
func (envelope Envelope) ExchangeTime() OptionalTime { return envelope.record.ExchangeTime }

// SourceSequence returns optional scoped source sequence evidence.
func (envelope Envelope) SourceSequence() OptionalUint64 { return envelope.record.SourceSequence }

// PayloadDigest calculates canonical SHA-256 payload evidence without retaining payload bytes.
func PayloadDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func validateEnvelope(input envelopeRecord) error {
	if input.SchemaVersion == "" || input.ParserVersion == "" || input.ConnectionGeneration == 0 {
		return runtimeError("invalid_event", "version")
	}
	if input.ID.Value() == "" || input.RunID.Value() == "" || input.SourceSessionID.Value() == "" || input.ConnectionID.Value() == "" {
		return runtimeError("invalid_event", "identity")
	}
	if input.Exchange == "" || !validSpotInstrument(input.Instrument) || input.Partition == "" {
		return runtimeError("invalid_event", "source")
	}
	if input.IngestOrdinal == 0 || input.RecordedLogicalTime == 0 || input.ReceivedAt.Validate() != nil {
		return runtimeError("invalid_event", "ordering")
	}
	if !validOptionalTime(input.ExchangeTime) || !validOptionalUint64(input.SourceSequence) {
		return runtimeError("invalid_event", "exchange_time")
	}
	if len(input.PayloadHash) != sha256.Size*2 || input.Run.ConfigurationHash == "" || input.Run.BuildCommit == "" {
		return runtimeError("invalid_event", "integrity")
	}
	if _, err := hex.DecodeString(input.PayloadHash); err != nil || !validDigest(input.Run.ConfigurationHash) {
		return runtimeError("invalid_event", "integrity")
	}
	if input.Run.OrderingVersion == "" || input.Run.SchedulerVersion == "" {
		return runtimeError("invalid_event", "run_versions")
	}
	return nil
}

func validSpotInstrument(instrument domain.Instrument) bool {
	validated, err := domain.NewSpotInstrument(instrument.Base, instrument.Quote)
	return err == nil && validated == instrument
}

func validOptionalTime(value OptionalTime) bool {
	if !value.Present {
		return value.Value.IsZero()
	}
	return !value.Value.IsZero() && value.Value.Location() == time.UTC
}

func validOptionalUint64(value OptionalUint64) bool {
	if !value.Present {
		return value.Value == 0
	}
	return value.Value > 0
}
