package recorder

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"axiom/internal/storage/segments"
)

// DatasetVerification is a bounded-memory replay/linkage proof.
type DatasetVerification struct {
	RecordCount  uint64 `json:"record_count"`
	ReplaySHA256 string `json:"replay_sha256"`
	SegmentPairs uint64 `json:"segment_pairs"`
}

// ReadManifest strictly decodes one immutable dataset manifest.
func ReadManifest(path string) (DatasetManifest, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maximumEventBytes {
		return DatasetManifest{}, recorderError("manifest_unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return DatasetManifest{}, recorderError("manifest_unavailable")
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maximumEventBytes+1))
	decoder.DisallowUnknownFields()
	var manifest DatasetManifest
	if err = decoder.Decode(&manifest); err != nil {
		return DatasetManifest{}, recorderError("manifest_invalid")
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return DatasetManifest{}, recorderError("manifest_invalid")
	}
	return manifest, nil
}

// ValidateDataset verifies manifest identity, every immutable segment, all
// raw/canonical links, and canonical replay ordering.
func ValidateDataset(root string, manifest DatasetManifest) ([]segments.Record, error) {
	if err := validateManifest(manifest); err != nil {
		return nil, err
	}
	wire, canonicalRows, err := loadDatasetRows(root, manifest)
	if err != nil {
		return nil, err
	}
	if uint64(len(wire)) != manifest.RawRecordCount || uint64(len(canonicalRows)) != manifest.CanonicalCount ||
		manifest.RawRecordCount != manifest.CanonicalCount {
		return nil, recorderError("manifest_count_mismatch")
	}
	sort.Slice(canonicalRows, func(left, right int) bool {
		return canonicalRows[left].IngestOrdinal < canonicalRows[right].IngestOrdinal
	})
	return linkCanonicalRows(wire, canonicalRows)
}

func loadDatasetRows(
	root string,
	manifest DatasetManifest,
) (map[uint64][sha256.Size]byte, []segments.CanonicalRow, error) {
	wire := make(map[uint64][sha256.Size]byte)
	canonicalRows := make([]segments.CanonicalRow, 0, manifest.CanonicalCount)
	for _, reference := range manifest.Segments {
		if filepath.Base(reference.Manifest.Path) != reference.Manifest.Path {
			return nil, nil, recorderError("segment_path_invalid")
		}
		path := filepath.Join(root, reference.Manifest.Path)
		if err := verifyFileChecksum(path, reference.Manifest.Checksum); err != nil {
			return nil, nil, err
		}
		switch reference.Kind {
		case "wire":
			rows, err := segments.ReadWireParquet(path)
			if err != nil {
				return nil, nil, recorderError("wire_segment_invalid")
			}
			for _, row := range rows {
				if _, duplicate := wire[row.IngestOrdinal]; duplicate {
					return nil, nil, recorderError("wire_ordinal_duplicate")
				}
				wire[row.IngestOrdinal] = row.PayloadSHA256
			}
		case "canonical":
			rows, err := segments.ReadCanonicalParquetRows(path, reference.Manifest.Spec)
			if err != nil {
				return nil, nil, recorderError("canonical_segment_invalid")
			}
			canonicalRows = append(canonicalRows, rows...)
		default:
			return nil, nil, recorderError("segment_kind_invalid")
		}
	}
	return wire, canonicalRows, nil
}

func linkCanonicalRows(
	wire map[uint64][sha256.Size]byte,
	canonicalRows []segments.CanonicalRow,
) ([]segments.Record, error) {
	records := make([]segments.Record, len(canonicalRows))
	for index, row := range canonicalRows {
		hash, exists := wire[row.IngestOrdinal]
		if !exists || hash != row.WirePayloadSHA256 || (index > 0 &&
			canonicalRows[index-1].IngestOrdinal >= row.IngestOrdinal) {
			return nil, recorderError("canonical_link_invalid")
		}
		records[index] = segments.Record{RecordedLogicalTime: row.RecordedLogicalTime,
			IngestOrdinal: row.IngestOrdinal, Canonical: append([]byte(nil), row.CanonicalEvent...)}
	}
	return records, nil
}

// VerifyDataset validates and hashes canonical replay order one segment pair at
// a time, keeping memory bounded by the configured recorder flush interval.
func VerifyDataset(root string, manifest DatasetManifest) (DatasetVerification, error) {
	verification, _, err := verifyDataset(root, manifest, false)
	return verification, err
}

func verifyDatasetWithOrdinals(
	root string,
	manifest DatasetManifest,
) (DatasetVerification, []uint64, error) {
	return verifyDataset(root, manifest, true)
}

func verifyDataset(
	root string,
	manifest DatasetManifest,
	collectOrdinals bool,
) (DatasetVerification, []uint64, error) {
	if err := validateManifest(manifest); err != nil || len(manifest.Segments)%2 != 0 {
		return DatasetVerification{}, nil, recorderError("manifest_invalid")
	}
	hash := sha256.New()
	var count, previous uint64
	var ordinals []uint64
	if collectOrdinals {
		ordinals = make([]uint64, 0, manifest.CanonicalCount)
	}
	for index := 0; index < len(manifest.Segments); index += 2 {
		wireReference, canonicalReference := manifest.Segments[index], manifest.Segments[index+1]
		wireRows, canonicalRows, err := loadVerifiedSegmentPair(root, wireReference, canonicalReference)
		if err != nil {
			return DatasetVerification{}, nil, err
		}
		for rowIndex := range wireRows {
			wire, canonical := wireRows[rowIndex], canonicalRows[rowIndex]
			if wire.IngestOrdinal != canonical.IngestOrdinal || wire.PayloadSHA256 != canonical.WirePayloadSHA256 ||
				canonical.IngestOrdinal <= previous {
				return DatasetVerification{}, nil, recorderError("canonical_link_invalid")
			}
			var framing [24]byte
			binary.BigEndian.PutUint64(framing[0:8], canonical.IngestOrdinal)
			binary.BigEndian.PutUint64(framing[8:16], canonical.RecordedLogicalTime)
			binary.BigEndian.PutUint64(framing[16:24], uint64(len(canonical.CanonicalEvent)))
			_, _ = hash.Write(framing[:])
			_, _ = hash.Write(canonical.CanonicalEvent)
			previous, count = canonical.IngestOrdinal, count+1
			if collectOrdinals {
				ordinals = append(ordinals, canonical.IngestOrdinal)
			}
		}
	}
	if count != manifest.RawRecordCount || count != manifest.CanonicalCount {
		return DatasetVerification{}, nil, recorderError("manifest_count_mismatch")
	}
	return DatasetVerification{RecordCount: count, ReplaySHA256: hex.EncodeToString(hash.Sum(nil)),
		SegmentPairs: uint64(len(manifest.Segments) / 2)}, ordinals, nil
}

func loadVerifiedSegmentPair(
	root string,
	wireReference, canonicalReference SegmentReference,
) ([]segments.WireRow, []segments.CanonicalRow, error) {
	if wireReference.Kind != "wire" || canonicalReference.Kind != "canonical" ||
		filepath.Base(wireReference.Manifest.Path) != wireReference.Manifest.Path ||
		filepath.Base(canonicalReference.Manifest.Path) != canonicalReference.Manifest.Path {
		return nil, nil, recorderError("segment_path_invalid")
	}
	wirePath := filepath.Join(root, wireReference.Manifest.Path)
	canonicalPath := filepath.Join(root, canonicalReference.Manifest.Path)
	if err := verifyFileChecksum(wirePath, wireReference.Manifest.Checksum); err != nil {
		return nil, nil, err
	}
	if err := verifyFileChecksum(canonicalPath, canonicalReference.Manifest.Checksum); err != nil {
		return nil, nil, err
	}
	wireRows, err := segments.ReadWireParquet(wirePath)
	if err != nil {
		return nil, nil, recorderError("wire_segment_invalid")
	}
	canonicalRows, err := segments.ReadCanonicalParquetRows(canonicalPath, canonicalReference.Manifest.Spec)
	if err != nil || len(wireRows) != len(canonicalRows) {
		return nil, nil, recorderError("canonical_segment_invalid")
	}
	return wireRows, canonicalRows, nil
}

func validateManifest(manifest DatasetManifest) error {
	if (manifest.SchemaVersion != datasetSchemaVersion && manifest.SchemaVersion != datasetSchemaVersionV2) ||
		!identifierPattern.MatchString(manifest.DatasetID) ||
		!identifierPattern.MatchString(manifest.SessionID) || !identifierPattern.MatchString(manifest.Exchange) ||
		manifest.Revision == 0 || manifest.CreatedAt.IsZero() || manifest.CreatedAt.Location() != time.UTC ||
		len(manifest.Segments) == 0 || manifest.Hash == "" || manifest.Hash != manifestHash(manifest) ||
		(manifest.Revision == 1 && manifest.PreviousHash != "") ||
		(manifest.Revision > 1 && !validDigest(manifest.PreviousHash)) || manifest.Complete != (len(manifest.Gaps) == 0) {
		return recorderError("manifest_invalid")
	}
	for _, gap := range manifest.Gaps {
		if err := validateGap(gap, manifest.Exchange); err != nil {
			return err
		}
	}
	if manifest.SchemaVersion == datasetSchemaVersion {
		if manifest.QualityTier != "" || len(manifest.ExchangeCoverage) != 0 || manifest.Compatibility != nil {
			return recorderError("manifest_invalid")
		}
		return nil
	}
	if manifest.QualityTier != "candidate" || len(manifest.ExchangeCoverage) != 1 ||
		manifest.Compatibility == nil || !validCompatibility(*manifest.Compatibility) ||
		validateExchangeCoverage(manifest, manifest.ExchangeCoverage[0]) != nil {
		return recorderError("manifest_invalid")
	}
	return nil
}

func validateExchangeCoverage(manifest DatasetManifest, coverage ExchangeCoverage) error {
	if coverage.Exchange != manifest.Exchange || !identifierPattern.MatchString(coverage.CollectorInstance) ||
		!identifierPattern.MatchString(coverage.CollectorRegion) || coverage.CoverageStart.IsZero() ||
		coverage.CoverageEnd.Before(coverage.CoverageStart) || coverage.CoverageStart.Location() != time.UTC ||
		coverage.CoverageEnd.Location() != time.UTC || coverage.FirstOrdinal == 0 ||
		coverage.LastOrdinal < coverage.FirstOrdinal || len(coverage.GenerationHistory) == 0 ||
		coverage.RawRecordCount != manifest.RawRecordCount ||
		coverage.CanonicalRecordCount != manifest.CanonicalCount || !coverage.RawCanonicalLinkageComplete ||
		coverage.HiddenGapCount != 0 || coverage.Complete != manifest.Complete ||
		!sortedUniqueNonempty(coverage.SchemaVersions) || !sortedUniqueNonempty(coverage.ParserVersions) ||
		!sortedUniqueNonempty(coverage.NormalizationVersions) {
		return recorderError("manifest_coverage_invalid")
	}
	var generationCount, firstOrdinal, lastOrdinal uint64
	var coverageStart, coverageEnd time.Time
	for index, generation := range coverage.GenerationHistory {
		if generation.ConnectionGeneration == 0 || generation.FirstOrdinal == 0 ||
			generation.LastOrdinal < generation.FirstOrdinal || generation.RecordCount == 0 ||
			generation.CoverageStart.IsZero() || generation.CoverageEnd.Before(generation.CoverageStart) ||
			generation.CoverageStart.Location() != time.UTC || generation.CoverageEnd.Location() != time.UTC ||
			(index > 0 && (coverage.GenerationHistory[index-1].ConnectionGeneration >= generation.ConnectionGeneration ||
				coverage.GenerationHistory[index-1].LastOrdinal >= generation.FirstOrdinal ||
				coverage.GenerationHistory[index-1].CoverageEnd.After(generation.CoverageStart))) {
			return recorderError("manifest_coverage_invalid")
		}
		if index == 0 {
			firstOrdinal, coverageStart = generation.FirstOrdinal, generation.CoverageStart
		}
		lastOrdinal, coverageEnd = generation.LastOrdinal, generation.CoverageEnd
		generationCount += generation.RecordCount
	}
	if generationCount != manifest.RawRecordCount ||
		firstOrdinal != coverage.FirstOrdinal || lastOrdinal != coverage.LastOrdinal ||
		!coverageStart.Equal(coverage.CoverageStart) || !coverageEnd.Equal(coverage.CoverageEnd) ||
		manifest.Compatibility.MinimumReaderVersion == "" ||
		!equalStrings(coverage.SchemaVersions, manifest.Compatibility.SchemaVersions) ||
		!equalStrings(coverage.ParserVersions, manifest.Compatibility.ParserVersions) ||
		!equalStrings(coverage.NormalizationVersions, manifest.Compatibility.NormalizationVersions) {
		return recorderError("manifest_coverage_invalid")
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func verifyFileChecksum(path, expected string) error {
	if !validDigest(expected) {
		return recorderError("segment_checksum_invalid")
	}
	file, err := os.Open(path)
	if err != nil {
		return recorderError("segment_unavailable")
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil || hex.EncodeToString(hash.Sum(nil)) != expected {
		return recorderError("segment_checksum_invalid")
	}
	return nil
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
