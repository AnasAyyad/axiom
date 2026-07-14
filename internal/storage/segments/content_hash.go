package segments

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const orderedHashVersion = "axiom.segment.ordered-content.v1"

// HashWireRows returns a versioned SHA-256 over validated rows and their exact
// order. Every variable field is length-framed to prevent ambiguous encodings.
func HashWireRows(rows []WireRow) (string, error) {
	if len(rows) == 0 {
		return "", fmt.Errorf("segment_wire_rows_empty")
	}
	return hashOrderedRows(WireSchemaVersion, rows, ValidateWireRow, encodeWireRow)
}

// HashCanonicalRows returns a versioned SHA-256 over validated canonical rows
// and their exact order.
func HashCanonicalRows(rows []CanonicalRow) (string, error) {
	if len(rows) == 0 {
		return "", fmt.Errorf("segment_canonical_rows_empty")
	}
	return hashOrderedRows(CanonicalSchemaVersion, rows, ValidateCanonicalRow, encodeCanonicalRow)
}

func hashOrderedRows[T any](schema string, rows []T, validate func(T) error, encode func(*bytes.Buffer, T)) (string, error) {
	digest := sha256.New()
	writeHashFrame(digest, []byte(orderedHashVersion))
	writeHashFrame(digest, []byte(schema))
	writeHashUint64(digest, uint64(len(rows)))
	for _, row := range rows {
		if err := validate(row); err != nil {
			return "", err
		}
		var frame bytes.Buffer
		encode(&frame, row)
		writeHashFrame(digest, frame.Bytes())
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func encodeWireRow(buffer *bytes.Buffer, row WireRow) {
	writeBufferUint64(buffer, row.IngestOrdinal)
	writeBufferStrings(buffer, row.Exchange, row.EventType, row.BaseAsset, row.QuoteAsset, row.SourceSessionID, row.ConnectionID)
	writeBufferUint64(buffer, row.ConnectionGeneration)
	writeBufferUint64(buffer, row.MonotonicOffsetNanos)
	writeBufferUint64(buffer, row.RecordedLogicalTime)
	writeBufferString(buffer, row.SourceSequence)
	writeOptionalInt64(buffer, row.ExchangeTimeUnixNano)
	writeBufferInt64(buffer, row.ReceivedAtUnixNano)
	writeBufferBytes(buffer, row.Payload)
	_, _ = buffer.Write(row.PayloadSHA256[:])
}

func encodeCanonicalRow(buffer *bytes.Buffer, row CanonicalRow) {
	writeBufferUint64(buffer, row.IngestOrdinal)
	writeBufferStrings(buffer, row.EventID, row.Exchange, row.EventType, row.BaseAsset, row.QuoteAsset, row.SourceSessionID, row.ConnectionID)
	writeBufferUint64(buffer, row.ConnectionGeneration)
	writeBufferUint64(buffer, row.MonotonicOffsetNanos)
	writeBufferUint64(buffer, row.RecordedLogicalTime)
	writeBufferString(buffer, row.SourceSequence)
	writeOptionalInt64(buffer, row.ExchangeTimeUnixNano)
	writeBufferInt64(buffer, row.ReceivedAtUnixNano)
	writeBufferStrings(buffer, row.ParserVersion, row.NormalizationVersion)
	_, _ = buffer.Write(row.WirePayloadSHA256[:])
	writeBufferBytes(buffer, row.CanonicalEvent)
	_, _ = buffer.Write(row.CanonicalSHA256[:])
}

func writeHashFrame(digest interface{ Write([]byte) (int, error) }, value []byte) {
	writeHashUint64(digest, uint64(len(value)))
	_, _ = digest.Write(value)
}

func writeHashUint64(writer interface{ Write([]byte) (int, error) }, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}

func writeBufferStrings(buffer *bytes.Buffer, values ...string) {
	for _, value := range values {
		writeBufferString(buffer, value)
	}
}

func writeBufferString(buffer *bytes.Buffer, value string) {
	writeBufferBytes(buffer, []byte(value))
}

func writeBufferBytes(buffer *bytes.Buffer, value []byte) {
	writeBufferUint64(buffer, uint64(len(value)))
	_, _ = buffer.Write(value)
}

func writeBufferUint64(buffer *bytes.Buffer, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = buffer.Write(encoded[:])
}

func writeBufferInt64(buffer *bytes.Buffer, value int64) {
	writeBufferUint64(buffer, uint64(value))
}

func writeOptionalInt64(buffer *bytes.Buffer, value *int64) {
	if value == nil {
		_ = buffer.WriteByte(0)
		return
	}
	_ = buffer.WriteByte(1)
	writeBufferInt64(buffer, *value)
}
