package segments

import (
	"fmt"
	"io"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

const parquetMaxRowsPerGroup = 64 * 1024

// NewWireParquetWriter snapshots validated wire rows and returns a finalizer
// writer that emits Parquet with single-worker Zstd level 3 compression.
func NewWireParquetWriter(rows []WireRow) (Writer, error) {
	snapshot := cloneWireRows(rows)
	orderedHash, err := HashWireRows(snapshot)
	if err != nil {
		return nil, err
	}
	return func(output io.Writer) (string, error) {
		if err := writeParquetRows(output, snapshot, WireSchemaVersion, "", "", orderedHash); err != nil {
			return "", err
		}
		return orderedHash, nil
	}, nil
}

// NewCanonicalParquetWriter snapshots validated rows and returns a finalizer
// writer with immutable compatibility metadata.
func NewCanonicalParquetWriter(rows []CanonicalRow) (Writer, error) {
	snapshot := cloneCanonicalRows(rows)
	if err := validateCanonicalVersions(snapshot); err != nil {
		return nil, err
	}
	orderedHash, err := HashCanonicalRows(snapshot)
	if err != nil {
		return nil, err
	}
	parser := snapshot[0].ParserVersion
	normalizer := snapshot[0].NormalizationVersion
	return func(output io.Writer) (string, error) {
		if err := writeParquetRows(output, snapshot, CanonicalSchemaVersion, parser, normalizer, orderedHash); err != nil {
			return "", err
		}
		return orderedHash, nil
	}, nil
}

func writeParquetRows[T any](output io.Writer, rows []T, schema, parser, normalizer, orderedHash string) (err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("segment_parquet_write_failed")
		}
	}()
	writer := parquet.NewGenericWriter[T](output, parquetWriterOptions(schema, parser, normalizer, orderedHash)...)
	written, writeErr := writer.Write(rows)
	if writeErr != nil || written != len(rows) {
		_ = writer.Close()
		return fmt.Errorf("segment_parquet_write_failed")
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("segment_parquet_close_failed")
	}
	return nil
}

func parquetWriterOptions(schema, parser, normalizer, orderedHash string) []parquet.WriterOption {
	options := []parquet.WriterOption{
		parquet.Compression(&zstd.Codec{Level: zstd.SpeedDefault, Concurrency: 1}),
		parquet.MaxRowsPerRowGroup(parquetMaxRowsPerGroup),
		parquet.CreatedBy("axiom", "v1", ""),
		parquet.KeyValueMetadata("axiom.schema_version", schema),
		parquet.KeyValueMetadata("axiom.ordered_content_hash", orderedHash),
		parquet.KeyValueMetadata("axiom.compression", "zstd-level-3"),
	}
	if parser != "" {
		options = append(options, parquet.KeyValueMetadata("axiom.parser_version", parser))
	}
	if normalizer != "" {
		options = append(options, parquet.KeyValueMetadata("axiom.normalization_version", normalizer))
	}
	return options
}

func validateCanonicalVersions(rows []CanonicalRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("segment_canonical_rows_empty")
	}
	parser := rows[0].ParserVersion
	normalizer := rows[0].NormalizationVersion
	for _, row := range rows[1:] {
		if row.ParserVersion != parser || row.NormalizationVersion != normalizer {
			return fmt.Errorf("segment_canonical_versions_mixed")
		}
	}
	return nil
}

func cloneWireRows(rows []WireRow) []WireRow {
	result := append([]WireRow(nil), rows...)
	for index := range result {
		result[index].Payload = append([]byte(nil), result[index].Payload...)
		result[index].ExchangeTimeUnixNano = cloneInt64(result[index].ExchangeTimeUnixNano)
	}
	return result
}

func cloneCanonicalRows(rows []CanonicalRow) []CanonicalRow {
	result := append([]CanonicalRow(nil), rows...)
	for index := range result {
		result[index].CanonicalEvent = append([]byte(nil), result[index].CanonicalEvent...)
		result[index].ExchangeTimeUnixNano = cloneInt64(result[index].ExchangeTimeUnixNano)
	}
	return result
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
