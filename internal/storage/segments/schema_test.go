package segments

import (
	"crypto/sha256"
	"testing"
)

func TestMarketSchemasHaveStableOrderedColumnsAndNoFloatTypes(t *testing.T) {
	for _, schema := range []SchemaDefinition{WireSchema, CanonicalSchema} {
		if schema.Version == "" || len(schema.Columns) == 0 {
			t.Fatalf("empty schema: %#v", schema)
		}
		seen := make(map[string]struct{}, len(schema.Columns))
		for _, column := range schema.Columns {
			if column.Name == "" || column.PhysicalType == "" {
				t.Fatalf("invalid column: %#v", column)
			}
			if column.PhysicalType == "FLOAT" || column.PhysicalType == "DOUBLE" {
				t.Fatalf("binary floating-point column: %#v", column)
			}
			if _, duplicate := seen[column.Name]; duplicate {
				t.Fatalf("duplicate column: %s", column.Name)
			}
			seen[column.Name] = struct{}{}
		}
	}
}

func TestWireAndCanonicalRowsValidateHashesAndIdentity(t *testing.T) {
	payload := []byte(`{"stream":"book","sequence":"42"}`)
	canonical := []byte(`{"kind":"book_delta","ordinal":1}`)
	wire := WireRow{
		IngestOrdinal: 1, Exchange: "binance", EventType: "book_delta", BaseAsset: "BTC", QuoteAsset: "USDT",
		SourceSessionID: "session-a", ConnectionID: "connection-a", ConnectionGeneration: 1,
		MonotonicOffsetNanos: 10, RecordedLogicalTime: 20,
		SourceSequence: "42", ReceivedAtUnixNano: 1784023200000000000, Payload: payload,
		PayloadSHA256: sha256.Sum256(payload),
	}
	if err := ValidateWireRow(wire); err != nil {
		t.Fatal(err)
	}
	row := CanonicalRow{
		IngestOrdinal: wire.IngestOrdinal, EventID: "event-a", Exchange: wire.Exchange, EventType: wire.EventType,
		BaseAsset: wire.BaseAsset, QuoteAsset: wire.QuoteAsset, SourceSessionID: wire.SourceSessionID,
		ConnectionID: wire.ConnectionID, ConnectionGeneration: wire.ConnectionGeneration,
		MonotonicOffsetNanos: wire.MonotonicOffsetNanos, RecordedLogicalTime: wire.RecordedLogicalTime,
		SourceSequence: wire.SourceSequence, ReceivedAtUnixNano: wire.ReceivedAtUnixNano,
		ParserVersion: "binance-parser.v1", NormalizationVersion: "market-normalization.v1",
		WirePayloadSHA256: wire.PayloadSHA256, CanonicalEvent: canonical, CanonicalSHA256: sha256.Sum256(canonical),
	}
	if err := ValidateCanonicalRow(row); err != nil {
		t.Fatal(err)
	}
	wire.Payload[0] = '!'
	if err := ValidateWireRow(wire); err == nil {
		t.Fatal("mutated wire payload accepted")
	}
	row.CanonicalEvent[0] = '!'
	if err := ValidateCanonicalRow(row); err == nil {
		t.Fatal("mutated canonical event accepted")
	}
}

func TestMarketRowsRejectIncompleteOrAmbiguousIdentity(t *testing.T) {
	payload := []byte("payload")
	wire := WireRow{
		IngestOrdinal: 1, Exchange: "binance", EventType: "trade", BaseAsset: "BTC", QuoteAsset: "BTC",
		SourceSessionID: "session-a", ConnectionID: "connection-a", ConnectionGeneration: 1,
		RecordedLogicalTime: 1, ReceivedAtUnixNano: 1, Payload: payload, PayloadSHA256: sha256.Sum256(payload),
	}
	if err := ValidateWireRow(wire); err == nil {
		t.Fatal("same-asset market accepted")
	}
	canonical := []byte("canonical")
	row := CanonicalRow{
		IngestOrdinal: 1, EventID: "event-a", Exchange: "binance", EventType: "trade", BaseAsset: "BTC", QuoteAsset: "USDT",
		SourceSessionID: "session-a", ConnectionID: "connection-a", ConnectionGeneration: 1,
		RecordedLogicalTime: 1, ReceivedAtUnixNano: 1, ParserVersion: "parser.v1", NormalizationVersion: "normalization.v1",
		CanonicalEvent: canonical, CanonicalSHA256: sha256.Sum256(canonical),
	}
	if err := ValidateCanonicalRow(row); err == nil {
		t.Fatal("canonical row without wire linkage accepted")
	}
}
