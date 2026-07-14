package segments

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Record is one decoder-validated canonical event in stored order.
type Record struct {
	RecordedLogicalTime uint64
	IngestOrdinal       uint64
	Canonical           []byte
}

// Gap is explicit missing dataset coverage.
type Gap struct {
	FirstOrdinal uint64
	LastOrdinal  uint64
	Reason       string
}

// Dataset declares ordered immutable segment coverage and explicit gaps.
type Dataset struct {
	ID              string
	OrderingVersion string
	OrderingScope   string
	Segments        []Manifest
	Gaps            []Gap
}

// Decoder reads one already verified Parquet segment using a compatible schema.
type Decoder func(string, Spec) ([]Record, error)

// Compatibility is the closed set of retained reader contracts.
type Compatibility struct {
	SchemaVersions        []string
	ParserVersions        []string
	NormalizationVersions []string
}

// Reader verifies manifests and yields deterministic decoder output.
type Reader struct {
	root        string
	schemas     map[string]struct{}
	parsers     map[string]struct{}
	normalizers map[string]struct{}
	decode      Decoder
}

// NewReader fixes a confined root, closed compatibility set, and decoder.
func NewReader(root string, compatibility Compatibility, decode Decoder) (*Reader, error) {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) || clean == string(filepath.Separator) || decode == nil {
		return nil, fmt.Errorf("dataset_reader_invalid")
	}
	schemas, err := compatibilitySet(compatibility.SchemaVersions)
	if err != nil {
		return nil, err
	}
	parsers, err := compatibilitySet(compatibility.ParserVersions)
	if err != nil {
		return nil, err
	}
	normalizers, err := compatibilitySet(compatibility.NormalizationVersions)
	if err != nil {
		return nil, err
	}
	return &Reader{root: clean, schemas: schemas, parsers: parsers, normalizers: normalizers, decode: decode}, nil
}

// Read validates order, coverage, checksums, compatibility, and record ordinals.
func (reader *Reader) Read(dataset Dataset) ([]Record, error) {
	if dataset.ID == "" || dataset.OrderingVersion != "dataset-order.v1" || dataset.OrderingScope == "" || len(dataset.Segments) == 0 {
		return nil, fmt.Errorf("dataset_manifest_invalid")
	}
	if err := validateGaps(dataset.Gaps); err != nil {
		return nil, err
	}
	result := make([]Record, 0)
	var prior uint64
	var priorLogical uint64
	seenOrdinals := make(map[uint64]struct{})
	for _, manifest := range dataset.Segments {
		if err := reader.validateManifest(manifest); err != nil {
			return nil, err
		}
		path := filepath.Join(reader.root, manifest.Path)
		records, err := reader.decode(path, manifest.Spec)
		if err != nil || uint64(len(records)) != manifest.Spec.RecordCount {
			return nil, fmt.Errorf("dataset_decode_invalid")
		}
		for _, record := range records {
			_, duplicate := seenOrdinals[record.IngestOrdinal]
			if record.RecordedLogicalTime == 0 || record.IngestOrdinal == 0 || duplicate || len(record.Canonical) == 0 ||
				record.RecordedLogicalTime < priorLogical ||
				(record.RecordedLogicalTime == priorLogical && record.IngestOrdinal <= prior) ||
				ordinalInGap(record.IngestOrdinal, dataset.Gaps) {
				return nil, fmt.Errorf("dataset_record_order_invalid")
			}
			seenOrdinals[record.IngestOrdinal] = struct{}{}
			priorLogical = record.RecordedLogicalTime
			prior = record.IngestOrdinal
			result = append(result, Record{
				RecordedLogicalTime: record.RecordedLogicalTime,
				IngestOrdinal:       record.IngestOrdinal, Canonical: append([]byte(nil), record.Canonical...),
			})
		}
		if records[0].IngestOrdinal != manifest.Spec.FirstOrdinal || records[len(records)-1].IngestOrdinal != manifest.Spec.LastOrdinal {
			return nil, fmt.Errorf("dataset_segment_coverage_invalid")
		}
	}
	return result, nil
}

func (reader *Reader) validateManifest(manifest Manifest) error {
	if validateSpec(manifest.Spec) != nil || manifest.Format != "parquet" || manifest.Compression != "zstd" ||
		!validHash(manifest.Checksum) || manifest.OrderedContentHash != manifest.Spec.OrderedContentHash ||
		manifest.Size <= 0 || filepath.Base(manifest.Path) != manifest.Path {
		return fmt.Errorf("dataset_segment_manifest_invalid")
	}
	if _, supported := reader.schemas[manifest.Spec.SchemaVersion]; !supported {
		return fmt.Errorf("dataset_schema_incompatible")
	}
	if _, supported := reader.parsers[manifest.Spec.ParserVersion]; !supported {
		return fmt.Errorf("dataset_parser_incompatible")
	}
	if _, supported := reader.normalizers[manifest.Spec.NormalizationVersion]; !supported {
		return fmt.Errorf("dataset_normalization_incompatible")
	}
	path := filepath.Join(reader.root, manifest.Path)
	checksum, size, err := fileDigest(path)
	if err != nil || checksum != manifest.Checksum || size != manifest.Size {
		return fmt.Errorf("dataset_segment_checksum_invalid")
	}
	return nil
}

func compatibilitySet(versions []string) (map[string]struct{}, error) {
	if len(versions) == 0 {
		return nil, fmt.Errorf("dataset_compatibility_invalid")
	}
	result := make(map[string]struct{}, len(versions))
	for _, version := range versions {
		if version == "" {
			return nil, fmt.Errorf("dataset_compatibility_invalid")
		}
		result[version] = struct{}{}
	}
	return result, nil
}

func ordinalInGap(ordinal uint64, gaps []Gap) bool {
	index := sort.Search(len(gaps), func(index int) bool { return gaps[index].LastOrdinal >= ordinal })
	return index < len(gaps) && gaps[index].FirstOrdinal <= ordinal
}

func validateGaps(gaps []Gap) error {
	var prior uint64
	for _, gap := range gaps {
		if gap.FirstOrdinal == 0 || gap.LastOrdinal < gap.FirstOrdinal || gap.Reason == "" || gap.FirstOrdinal <= prior {
			return fmt.Errorf("dataset_gap_invalid")
		}
		prior = gap.LastOrdinal
	}
	return nil
}

func fileDigest(path string) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("dataset_segment_file_invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(digest.Sum(nil)), size, nil
}
