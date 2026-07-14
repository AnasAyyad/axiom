package segments

import (
	"fmt"
	"io"
	"math"
	"os"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

const parquetReadBatchSize = 1024

// DecodeCanonicalParquet verifies the physical codec, compatibility metadata,
// row digests, and ordered-content hash before returning replay records.
func DecodeCanonicalParquet(path string, spec Spec) ([]Record, error) {
	rows, err := ReadCanonicalParquetRows(path, spec)
	if err != nil {
		return nil, err
	}
	return canonicalRecords(rows), nil
}

// ReadCanonicalParquetRows verifies and returns linked canonical rows for
// dataset-manifest validation.
func ReadCanonicalParquetRows(path string, spec Spec) ([]CanonicalRow, error) {
	if spec.SchemaVersion != CanonicalSchemaVersion || spec.RecordCount > math.MaxInt64 {
		return nil, fmt.Errorf("segment_parquet_spec_invalid")
	}
	file, parsed, err := openParquet(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if err = verifyParquet(parsed, parquet.SchemaOf(new(CanonicalRow)), canonicalMetadata(spec), int64(spec.RecordCount)); err != nil {
		return nil, err
	}
	rows, err := readParquetRows[CanonicalRow](parsed)
	if err != nil || uint64(len(rows)) != spec.RecordCount {
		return nil, fmt.Errorf("segment_parquet_read_failed")
	}
	if err = verifyCanonicalRows(rows, spec); err != nil {
		return nil, err
	}
	return cloneCanonicalRows(rows), nil
}

// ReadWireParquet verifies and reads an immutable wire segment. Its metadata
// carries the ordered-content hash used to detect row mutation or reordering.
func ReadWireParquet(path string) ([]WireRow, error) {
	file, parsed, err := openParquet(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	orderedHash, ok := parsed.Lookup("axiom.ordered_content_hash")
	if !ok || !validHash(orderedHash) {
		return nil, fmt.Errorf("segment_parquet_metadata_invalid")
	}
	metadata := baseMetadata(WireSchemaVersion, orderedHash)
	if err = verifyParquet(parsed, parquet.SchemaOf(new(WireRow)), metadata, parsed.NumRows()); err != nil {
		return nil, err
	}
	rows, err := readParquetRows[WireRow](parsed)
	if err != nil || len(rows) == 0 {
		return nil, fmt.Errorf("segment_parquet_read_failed")
	}
	actual, err := HashWireRows(rows)
	if err != nil || actual != orderedHash {
		return nil, fmt.Errorf("segment_ordered_content_mismatch")
	}
	return cloneWireRows(rows), nil
}

func openParquet(path string) (*os.File, *parquet.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 8 {
		return nil, nil, fmt.Errorf("segment_parquet_file_invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("segment_parquet_file_invalid")
	}
	parsed, err := parquet.OpenFile(file, info.Size())
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("segment_parquet_open_failed")
	}
	return file, parsed, nil
}

func verifyParquet(file *parquet.File, schema *parquet.Schema, metadata map[string]string, rowCount int64) error {
	if rowCount <= 0 || file.NumRows() != rowCount || !parquet.EqualNodes(file.Schema(), schema) {
		return fmt.Errorf("segment_parquet_schema_invalid")
	}
	for key, expected := range metadata {
		actual, ok := file.Lookup(key)
		if !ok || actual != expected {
			return fmt.Errorf("segment_parquet_metadata_invalid")
		}
	}
	if !allColumnsUseZstd(file) {
		return fmt.Errorf("segment_parquet_compression_invalid")
	}
	return nil
}

func allColumnsUseZstd(file *parquet.File) bool {
	metadata := file.Metadata()
	if len(metadata.RowGroups) == 0 {
		return false
	}
	for _, group := range metadata.RowGroups {
		if len(group.Columns) == 0 {
			return false
		}
		for _, column := range group.Columns {
			if column.MetaData.Codec != format.Zstd {
				return false
			}
		}
	}
	return true
}

func readParquetRows[T any](file *parquet.File) (rows []T, err error) {
	defer func() {
		if recover() != nil {
			rows = nil
			err = fmt.Errorf("segment_parquet_read_failed")
		}
	}()
	reader := parquet.NewGenericReader[T](file)
	defer reader.Close()
	batch := make([]T, parquetReadBatchSize)
	for {
		count, readErr := reader.Read(batch)
		rows = append(rows, batch[:count]...)
		if readErr == io.EOF {
			return rows, nil
		}
		if readErr != nil {
			return nil, fmt.Errorf("segment_parquet_read_failed")
		}
		if count == 0 {
			return nil, fmt.Errorf("segment_parquet_read_failed")
		}
	}
}

func verifyCanonicalRows(rows []CanonicalRow, spec Spec) error {
	for _, row := range rows {
		if err := ValidateCanonicalRow(row); err != nil || row.ParserVersion != spec.ParserVersion ||
			row.NormalizationVersion != spec.NormalizationVersion {
			return fmt.Errorf("segment_canonical_row_invalid")
		}
	}
	actual, err := HashCanonicalRows(rows)
	if err != nil || actual != spec.OrderedContentHash {
		return fmt.Errorf("segment_ordered_content_mismatch")
	}
	if rows[0].IngestOrdinal != spec.FirstOrdinal || rows[len(rows)-1].IngestOrdinal != spec.LastOrdinal {
		return fmt.Errorf("segment_coverage_mismatch")
	}
	return nil
}

func canonicalRecords(rows []CanonicalRow) []Record {
	records := make([]Record, len(rows))
	for index, row := range rows {
		records[index] = Record{RecordedLogicalTime: row.RecordedLogicalTime, IngestOrdinal: row.IngestOrdinal,
			Canonical: append([]byte(nil), row.CanonicalEvent...)}
	}
	return records
}

func canonicalMetadata(spec Spec) map[string]string {
	metadata := baseMetadata(spec.SchemaVersion, spec.OrderedContentHash)
	metadata["axiom.parser_version"] = spec.ParserVersion
	metadata["axiom.normalization_version"] = spec.NormalizationVersion
	return metadata
}

func baseMetadata(schema, orderedHash string) map[string]string {
	return map[string]string{
		"axiom.schema_version":       schema,
		"axiom.ordered_content_hash": orderedHash,
		"axiom.compression":          "zstd-level-3",
	}
}
