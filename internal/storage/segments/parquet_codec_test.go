package segments

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

func TestCanonicalParquetFinalizerAndReaderRoundTrip(t *testing.T) {
	rows := canonicalRowsFixture()
	orderedHash, err := HashCanonicalRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewCanonicalParquetWriter(rows)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].CanonicalEvent[0] = '!'
	root := t.TempDir()
	spec := canonicalSpec(orderedHash)
	finalizer, _ := NewFinalizer(root, nil)
	manifest, err := finalizer.Finalize(spec, writer, func(Manifest) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(root, Compatibility{
		SchemaVersions: []string{CanonicalSchemaVersion}, ParserVersions: []string{"parser.v1"},
		NormalizationVersions: []string{"normalization.v1"},
	}, DecodeCanonicalParquet)
	if err != nil {
		t.Fatal(err)
	}
	records, err := reader.Read(Dataset{
		ID: "dataset-a", OrderingVersion: "dataset-order.v1", OrderingScope: "session-a", Segments: []Manifest{manifest},
	})
	if err != nil || len(records) != 2 || string(records[0].Canonical) != `{"price":"100.00"}` {
		t.Fatalf("records = %#v, err = %v", records, err)
	}
	assertParquetMetadataAndCompression(t, filepath.Join(root, manifest.Path), spec)
}

func TestWireParquetRoundTripPreservesOptionalValues(t *testing.T) {
	rows := wireRowsFixture()
	wantHash, err := HashWireRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := NewWireParquetWriter(rows)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	gotHash, err := writer(&encoded)
	if err != nil || gotHash != wantHash {
		t.Fatalf("write hash = %q, err = %v", gotHash, err)
	}
	path := filepath.Join(t.TempDir(), "wire.parquet")
	if err = os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadWireParquet(path)
	if err != nil || !reflect.DeepEqual(got, rows) {
		t.Fatalf("wire rows = %#v, err = %v", got, err)
	}
}

func TestCanonicalParquetRejectsMixedVersionsAndWrongSpec(t *testing.T) {
	rows := canonicalRowsFixture()
	rows[1].ParserVersion = "parser.v2"
	if _, err := NewCanonicalParquetWriter(rows); err == nil {
		t.Fatal("mixed parser versions accepted")
	}
	rows = canonicalRowsFixture()
	hash, _ := HashCanonicalRows(rows)
	writer, _ := NewCanonicalParquetWriter(rows)
	path := filepath.Join(t.TempDir(), "canonical.parquet")
	file, _ := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	_, err := writer(file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	spec := canonicalSpec(hash)
	spec.ParserVersion = "parser.v2"
	if _, err = DecodeCanonicalParquet(path, spec); err == nil {
		t.Fatal("metadata/spec mismatch accepted")
	}
}

func TestParquetStructSchemasMatchReviewableContracts(t *testing.T) {
	assertSchemaContract(t, parquet.SchemaOf(new(WireRow)), WireSchema)
	assertSchemaContract(t, parquet.SchemaOf(new(CanonicalRow)), CanonicalSchema)
}

func TestOrderedContentHashCommitsToOrderAndBytes(t *testing.T) {
	rows := canonicalRowsFixture()
	original, _ := HashCanonicalRows(rows)
	rows[0], rows[1] = rows[1], rows[0]
	reordered, _ := HashCanonicalRows(rows)
	if reordered == original {
		t.Fatal("row reordering did not change ordered-content hash")
	}
	rows = canonicalRowsFixture()
	rows[0].CanonicalEvent = []byte(`{"price":"100.01"}`)
	rows[0].CanonicalSHA256 = sha256.Sum256(rows[0].CanonicalEvent)
	mutated, _ := HashCanonicalRows(rows)
	if mutated == original {
		t.Fatal("row mutation did not change ordered-content hash")
	}
}

func BenchmarkCanonicalParquetRoundTrip(b *testing.B) {
	rows := make([]CanonicalRow, 4096)
	for index := range rows {
		rows[index] = canonicalRowsFixture()[0]
		rows[index].IngestOrdinal = uint64(index + 1)
		rows[index].RecordedLogicalTime = uint64(index + 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		writer, err := NewCanonicalParquetWriter(rows)
		if err != nil {
			b.Fatal(err)
		}
		var output bytes.Buffer
		if _, err = writer(&output); err != nil {
			b.Fatal(err)
		}
		file, err := parquet.OpenFile(bytes.NewReader(output.Bytes()), int64(output.Len()))
		if err != nil {
			b.Fatal(err)
		}
		if _, err = readParquetRows[CanonicalRow](file); err != nil {
			b.Fatal(err)
		}
	}
}

func assertSchemaContract(t *testing.T, actual *parquet.Schema, expected SchemaDefinition) {
	t.Helper()
	columns := actual.Columns()
	if len(columns) != len(expected.Columns) {
		t.Fatalf("%s columns = %d, want %d", expected.Version, len(columns), len(expected.Columns))
	}
	for index, definition := range expected.Columns {
		if len(columns[index]) != 1 || columns[index][0] != definition.Name {
			t.Fatalf("column %d = %v, want %s", index, columns[index], definition.Name)
		}
		leaf, ok := actual.Lookup(definition.Name)
		if !ok || leaf.Node.Required() != definition.Required || leaf.Node.Type().Kind().String() != physicalKind(definition.PhysicalType) {
			t.Fatalf("column %s = %s", definition.Name, leaf.Node)
		}
		if leaf.Node.Type().String() != parquetType(definition) {
			t.Fatalf("column %s type = %s, want %s", definition.Name, leaf.Node.Type(), parquetType(definition))
		}
		if definition.PhysicalType == "FIXED_LEN_BYTE_ARRAY(32)" && leaf.Node.Type().Length() != sha256.Size {
			t.Fatalf("column %s fixed length = %d", definition.Name, leaf.Node.Type().Length())
		}
	}
}

func physicalKind(value string) string {
	if value == "FIXED_LEN_BYTE_ARRAY(32)" {
		return "FIXED_LEN_BYTE_ARRAY"
	}
	return value
}

func parquetType(definition ColumnDefinition) string {
	switch definition.LogicalType {
	case "UINT_64":
		return "INT(64,false)"
	case "UTF8":
		return "STRING"
	case "TIMESTAMP_NANOS_UTC":
		return "TIMESTAMP(isAdjustedToUTC=true,unit=NANOS)"
	default:
		return definition.PhysicalType
	}
}

func assertParquetMetadataAndCompression(t *testing.T, path string, spec Spec) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, _ := file.Stat()
	parsed, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := parsed.Lookup("axiom.schema_version"); !ok || value != CanonicalSchemaVersion {
		t.Fatalf("schema metadata = %q", value)
	}
	for _, group := range parsed.Metadata().RowGroups {
		for _, column := range group.Columns {
			if column.MetaData.Codec != format.Zstd {
				t.Fatalf("compression = %s", column.MetaData.Codec)
			}
		}
	}
	if value, _ := parsed.Lookup("axiom.ordered_content_hash"); value != spec.OrderedContentHash {
		t.Fatalf("ordered hash metadata = %q", value)
	}
}

func canonicalSpec(hash string) Spec {
	return Spec{
		Name: "binance-btcusdt-20260714t09", SchemaVersion: CanonicalSchemaVersion, ParserVersion: "parser.v1",
		NormalizationVersion: "normalization.v1", OrderedContentHash: hash, FirstOrdinal: 1, LastOrdinal: 2, RecordCount: 2,
		StartedAt: time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC), EndedAt: time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC),
	}
}

func wireRowsFixture() []WireRow {
	payloadOne := []byte(`{"trade":"one"}`)
	payloadTwo := []byte(`{"trade":"two"}`)
	exchangeTime := int64(1784019600000000000)
	return []WireRow{
		{IngestOrdinal: 1, Exchange: "binance", EventType: "trade", BaseAsset: "BTC", QuoteAsset: "USDT",
			SourceSessionID: "session-a", ConnectionID: "connection-a", ConnectionGeneration: 1, MonotonicOffsetNanos: 1,
			RecordedLogicalTime: 1, SourceSequence: "100", ExchangeTimeUnixNano: &exchangeTime,
			ReceivedAtUnixNano: exchangeTime + 10, Payload: payloadOne, PayloadSHA256: sha256.Sum256(payloadOne)},
		{IngestOrdinal: 2, Exchange: "binance", EventType: "trade", BaseAsset: "BTC", QuoteAsset: "USDT",
			SourceSessionID: "session-a", ConnectionID: "connection-a", ConnectionGeneration: 1, MonotonicOffsetNanos: 2,
			RecordedLogicalTime: 2, ReceivedAtUnixNano: exchangeTime + 20, Payload: payloadTwo, PayloadSHA256: sha256.Sum256(payloadTwo)},
	}
}

func canonicalRowsFixture() []CanonicalRow {
	wire := wireRowsFixture()
	eventOne := []byte(`{"price":"100.00"}`)
	eventTwo := []byte(`{"price":"101.00"}`)
	return []CanonicalRow{
		canonicalRowFromWire(wire[0], "event-one", eventOne),
		canonicalRowFromWire(wire[1], "event-two", eventTwo),
	}
}

func canonicalRowFromWire(wire WireRow, eventID string, event []byte) CanonicalRow {
	return CanonicalRow{
		IngestOrdinal: wire.IngestOrdinal, EventID: eventID, Exchange: wire.Exchange, EventType: wire.EventType,
		BaseAsset: wire.BaseAsset, QuoteAsset: wire.QuoteAsset, SourceSessionID: wire.SourceSessionID, ConnectionID: wire.ConnectionID,
		ConnectionGeneration: wire.ConnectionGeneration, MonotonicOffsetNanos: wire.MonotonicOffsetNanos,
		RecordedLogicalTime: wire.RecordedLogicalTime, SourceSequence: wire.SourceSequence,
		ExchangeTimeUnixNano: wire.ExchangeTimeUnixNano, ReceivedAtUnixNano: wire.ReceivedAtUnixNano,
		ParserVersion: "parser.v1", NormalizationVersion: "normalization.v1", WirePayloadSHA256: wire.PayloadSHA256,
		CanonicalEvent: event, CanonicalSHA256: sha256.Sum256(event),
	}
}
