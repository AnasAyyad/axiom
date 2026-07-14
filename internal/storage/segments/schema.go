package segments

import (
	"bytes"
	"crypto/sha256"
	"fmt"
)

// Segment schema version constants identify immutable wire and canonical rows.
const (
	// WireSchemaVersion stores the immutable exchange envelope needed to replay
	// parser behavior without relying on a normalized representation.
	WireSchemaVersion = "market-wire.v1"
	// CanonicalSchemaVersion stores the parser output consumed by replay and
	// links it to the exact wire payload from which it was derived.
	CanonicalSchemaVersion = "market-canonical.v1"
)

// ColumnDefinition is a codec-independent, reviewable Parquet column contract.
// PhysicalType names Parquet physical types; LogicalType is empty when no
// logical annotation is used.
type ColumnDefinition struct {
	Name         string
	PhysicalType string
	LogicalType  string
	Required     bool
}

// SchemaDefinition fixes the ordered columns for one immutable schema version.
type SchemaDefinition struct {
	Version string
	Columns []ColumnDefinition
}

// WireSchema is the raw public-market envelope schema. UTC nanoseconds and
// exact bytes avoid timezone ambiguity and binary floating-point values.
var WireSchema = SchemaDefinition{Version: WireSchemaVersion, Columns: []ColumnDefinition{
	{Name: "ingest_ordinal", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "exchange", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "event_type", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "base_asset", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "quote_asset", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "source_session_id", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "connection_id", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "connection_generation", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "monotonic_offset_nanos", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "recorded_logical_time", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "source_sequence", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: false},
	{Name: "exchange_time_unix_nano", PhysicalType: "INT64", LogicalType: "TIMESTAMP_NANOS_UTC", Required: false},
	{Name: "received_at_unix_nano", PhysicalType: "INT64", LogicalType: "TIMESTAMP_NANOS_UTC", Required: true},
	{Name: "payload", PhysicalType: "BYTE_ARRAY", Required: true},
	{Name: "payload_sha256", PhysicalType: "FIXED_LEN_BYTE_ARRAY(32)", Required: true},
}}

// CanonicalSchema is the normalized event schema used by deterministic replay.
var CanonicalSchema = SchemaDefinition{Version: CanonicalSchemaVersion, Columns: []ColumnDefinition{
	{Name: "ingest_ordinal", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "event_id", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "exchange", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "event_type", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "base_asset", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "quote_asset", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "source_session_id", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "connection_id", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "connection_generation", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "monotonic_offset_nanos", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "recorded_logical_time", PhysicalType: "INT64", LogicalType: "UINT_64", Required: true},
	{Name: "source_sequence", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: false},
	{Name: "exchange_time_unix_nano", PhysicalType: "INT64", LogicalType: "TIMESTAMP_NANOS_UTC", Required: false},
	{Name: "received_at_unix_nano", PhysicalType: "INT64", LogicalType: "TIMESTAMP_NANOS_UTC", Required: true},
	{Name: "parser_version", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "normalization_version", PhysicalType: "BYTE_ARRAY", LogicalType: "UTF8", Required: true},
	{Name: "wire_payload_sha256", PhysicalType: "FIXED_LEN_BYTE_ARRAY(32)", Required: true},
	{Name: "canonical_event", PhysicalType: "BYTE_ARRAY", Required: true},
	{Name: "canonical_sha256", PhysicalType: "FIXED_LEN_BYTE_ARRAY(32)", Required: true},
}}

// WireRow is one immutable exchange envelope. SourceSequence remains text so an
// adapter can preserve an exchange-native identifier without lossy conversion.
type WireRow struct {
	IngestOrdinal        uint64            `parquet:"ingest_ordinal,uint(64)"`
	Exchange             string            `parquet:"exchange"`
	EventType            string            `parquet:"event_type"`
	BaseAsset            string            `parquet:"base_asset"`
	QuoteAsset           string            `parquet:"quote_asset"`
	SourceSessionID      string            `parquet:"source_session_id"`
	ConnectionID         string            `parquet:"connection_id"`
	ConnectionGeneration uint64            `parquet:"connection_generation,uint(64)"`
	MonotonicOffsetNanos uint64            `parquet:"monotonic_offset_nanos,uint(64)"`
	RecordedLogicalTime  uint64            `parquet:"recorded_logical_time,uint(64)"`
	SourceSequence       string            `parquet:"source_sequence,optional"`
	ExchangeTimeUnixNano *int64            `parquet:"exchange_time_unix_nano,timestamp(nanosecond:utc)"`
	ReceivedAtUnixNano   int64             `parquet:"received_at_unix_nano,timestamp(nanosecond:utc)"`
	Payload              []byte            `parquet:"payload"`
	PayloadSHA256        [sha256.Size]byte `parquet:"payload_sha256"`
}

// CanonicalRow is one normalized replay event linked to its source envelope.
type CanonicalRow struct {
	IngestOrdinal        uint64            `parquet:"ingest_ordinal,uint(64)"`
	EventID              string            `parquet:"event_id"`
	Exchange             string            `parquet:"exchange"`
	EventType            string            `parquet:"event_type"`
	BaseAsset            string            `parquet:"base_asset"`
	QuoteAsset           string            `parquet:"quote_asset"`
	SourceSessionID      string            `parquet:"source_session_id"`
	ConnectionID         string            `parquet:"connection_id"`
	ConnectionGeneration uint64            `parquet:"connection_generation,uint(64)"`
	MonotonicOffsetNanos uint64            `parquet:"monotonic_offset_nanos,uint(64)"`
	RecordedLogicalTime  uint64            `parquet:"recorded_logical_time,uint(64)"`
	SourceSequence       string            `parquet:"source_sequence,optional"`
	ExchangeTimeUnixNano *int64            `parquet:"exchange_time_unix_nano,timestamp(nanosecond:utc)"`
	ReceivedAtUnixNano   int64             `parquet:"received_at_unix_nano,timestamp(nanosecond:utc)"`
	ParserVersion        string            `parquet:"parser_version"`
	NormalizationVersion string            `parquet:"normalization_version"`
	WirePayloadSHA256    [sha256.Size]byte `parquet:"wire_payload_sha256"`
	CanonicalEvent       []byte            `parquet:"canonical_event"`
	CanonicalSHA256      [sha256.Size]byte `parquet:"canonical_sha256"`
}

// ValidateWireRow rejects incomplete identity, non-UTC epoch values, and
// payload mutations before a row reaches a Parquet writer.
func ValidateWireRow(row WireRow) error {
	if row.IngestOrdinal == 0 || row.Exchange == "" || row.EventType == "" || row.BaseAsset == "" ||
		row.QuoteAsset == "" || row.SourceSessionID == "" || row.ConnectionID == "" ||
		row.ConnectionGeneration == 0 || row.RecordedLogicalTime == 0 || row.ReceivedAtUnixNano <= 0 || len(row.Payload) == 0 ||
		row.BaseAsset == row.QuoteAsset || sha256.Sum256(row.Payload) != row.PayloadSHA256 {
		return fmt.Errorf("segment_wire_row_invalid")
	}
	if row.ExchangeTimeUnixNano != nil && *row.ExchangeTimeUnixNano <= 0 {
		return fmt.Errorf("segment_wire_row_invalid")
	}
	return nil
}

// ValidateCanonicalRow rejects mutation and incomplete replay identity.
func ValidateCanonicalRow(row CanonicalRow) error {
	zeroHash := [sha256.Size]byte{}
	if row.IngestOrdinal == 0 || row.EventID == "" || row.Exchange == "" || row.EventType == "" ||
		row.BaseAsset == "" || row.QuoteAsset == "" || row.SourceSessionID == "" || row.ConnectionID == "" ||
		row.ConnectionGeneration == 0 || row.RecordedLogicalTime == 0 || row.ReceivedAtUnixNano <= 0 || row.ParserVersion == "" ||
		row.NormalizationVersion == "" || row.BaseAsset == row.QuoteAsset || len(row.CanonicalEvent) == 0 ||
		bytes.Equal(row.WirePayloadSHA256[:], zeroHash[:]) || sha256.Sum256(row.CanonicalEvent) != row.CanonicalSHA256 {
		return fmt.Errorf("segment_canonical_row_invalid")
	}
	if row.ExchangeTimeUnixNano != nil && *row.ExchangeTimeUnixNano <= 0 {
		return fmt.Errorf("segment_canonical_row_invalid")
	}
	return nil
}
