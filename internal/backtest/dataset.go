package backtest

import (
	"path/filepath"

	"axiom/internal/recorder"
	"axiom/internal/storage/segments"
)

// DatasetCompatibility fixes admitted parser and normalizer versions.
type DatasetCompatibility struct {
	SourceCommit           string
	ParserVersion          string
	NormalizationVersion   string
	MinimumRecordsPerPair  uint64
	MaximumLowDensityPairs uint64
}

// DatasetEvent is one defensive canonical record.
type DatasetEvent struct {
	Record segments.Record
}

// DatasetReader verifies once, then keeps at most one segment pair in memory.
type DatasetReader struct {
	root       string
	manifest   recorder.DatasetManifest
	descriptor DatasetDescriptor
	pair       int
	rows       []segments.Record
	row        int
	previous   uint64
}

// OpenDataset admits one selected manifest revision after complete recorder
// verification and cumulative-chain validation.
func OpenDataset(root, manifestPath string, compatibility DatasetCompatibility) (*DatasetReader, error) {
	if root == "" || filepath.Dir(manifestPath) != root || !validCompatibility(compatibility) {
		return nil, backtestError("dataset_configuration_invalid")
	}
	manifest, err := recorder.ReadManifest(manifestPath)
	if err != nil || recorder.VerifyManifestChain(root, manifest) != nil {
		return nil, backtestError("dataset_manifest_invalid")
	}
	verification, err := recorder.VerifyDataset(root, manifest)
	if err != nil {
		return nil, backtestError("dataset_verification_failed")
	}
	descriptor, err := describeDataset(manifest, verification, compatibility)
	if err != nil {
		return nil, err
	}
	return &DatasetReader{root: root, manifest: manifest, descriptor: descriptor}, nil
}

// Descriptor returns the immutable selected dataset identity.
func (reader *DatasetReader) Descriptor() DatasetDescriptor { return reader.descriptor.Clone() }

// Gaps returns a defensive copy of all declared quality gaps.
func (reader *DatasetReader) Gaps() []recorder.Gap {
	return append([]recorder.Gap(nil), reader.manifest.Gaps...)
}

// SeekOrdinal uses manifest coverage indexes and positions the next read at the
// exact selected event without retaining earlier segments.
func (reader *DatasetReader) SeekOrdinal(ordinal uint64) error {
	if ordinal == 0 {
		return backtestError("dataset_seek_invalid")
	}
	for pair := 0; pair*2 < len(reader.manifest.Segments); pair++ {
		spec := reader.manifest.Segments[pair*2+1].Manifest.Spec
		if ordinal < spec.FirstOrdinal || ordinal > spec.LastOrdinal {
			continue
		}
		reader.pair, reader.rows, reader.row, reader.previous = pair, nil, 0, ordinal-1
		if err := reader.loadPair(); err != nil {
			return err
		}
		for index := range reader.rows {
			if reader.rows[index].IngestOrdinal == ordinal {
				reader.row = index
				return nil
			}
		}
		return backtestError("dataset_seek_invalid")
	}
	return backtestError("dataset_seek_invalid")
}

// Next yields canonical events in ordinal order while loading one pair at a time.
func (reader *DatasetReader) Next() (DatasetEvent, bool, error) {
	for reader.row >= len(reader.rows) {
		if reader.pair*2 >= len(reader.manifest.Segments) {
			return DatasetEvent{}, false, nil
		}
		if err := reader.loadPair(); err != nil {
			return DatasetEvent{}, false, err
		}
	}
	record := cloneRecord(reader.rows[reader.row])
	reader.row++
	if record.IngestOrdinal <= reader.previous {
		return DatasetEvent{}, false, backtestError("dataset_order_invalid")
	}
	reader.previous = record.IngestOrdinal
	return DatasetEvent{Record: record}, true, nil
}

func (reader *DatasetReader) loadPair() error {
	wireReference := reader.manifest.Segments[reader.pair*2]
	canonicalReference := reader.manifest.Segments[reader.pair*2+1]
	if wireReference.Kind != "wire" || canonicalReference.Kind != "canonical" {
		return backtestError("dataset_pair_invalid")
	}
	wire, err := segments.ReadWireParquet(filepath.Join(reader.root, wireReference.Manifest.Path))
	if err != nil {
		return backtestError("dataset_pair_invalid")
	}
	canonical, err := segments.ReadCanonicalParquetRows(
		filepath.Join(reader.root, canonicalReference.Manifest.Path), canonicalReference.Manifest.Spec)
	if err != nil || len(wire) != len(canonical) {
		return backtestError("dataset_pair_invalid")
	}
	rows := make([]segments.Record, len(canonical))
	for index := range canonical {
		if wire[index].IngestOrdinal != canonical[index].IngestOrdinal ||
			wire[index].PayloadSHA256 != canonical[index].WirePayloadSHA256 {
			return backtestError("dataset_pair_invalid")
		}
		rows[index] = segments.Record{RecordedLogicalTime: canonical[index].RecordedLogicalTime,
			IngestOrdinal: canonical[index].IngestOrdinal, Canonical: append([]byte(nil), canonical[index].CanonicalEvent...)}
	}
	reader.rows, reader.row, reader.pair = rows, 0, reader.pair+1
	return nil
}

func describeDataset(
	manifest recorder.DatasetManifest,
	verification recorder.DatasetVerification,
	compatibility DatasetCompatibility,
) (DatasetDescriptor, error) {
	canonical := manifest.Segments[1].Manifest.Spec
	if canonical.ParserVersion != compatibility.ParserVersion ||
		canonical.NormalizationVersion != compatibility.NormalizationVersion {
		return DatasetDescriptor{}, backtestError("dataset_version_incompatible")
	}
	hashes := make([]string, 0, len(manifest.Segments))
	var lowDensity uint64
	for index, reference := range manifest.Segments {
		if reference.Manifest.Spec.SchemaVersion != schemaForKind(reference.Kind) {
			return DatasetDescriptor{}, backtestError("dataset_schema_incompatible")
		}
		if reference.Kind == "canonical" && (reference.Manifest.Spec.ParserVersion != compatibility.ParserVersion ||
			reference.Manifest.Spec.NormalizationVersion != compatibility.NormalizationVersion) {
			return DatasetDescriptor{}, backtestError("dataset_version_incompatible")
		}
		if index%2 == 1 && reference.Manifest.Spec.RecordCount < compatibility.MinimumRecordsPerPair {
			lowDensity++
		}
		hashes = append(hashes, reference.Manifest.Checksum)
	}
	confidence := confidenceFor(manifest, lowDensity, compatibility.MaximumLowDensityPairs)
	return DatasetDescriptor{DatasetID: manifest.DatasetID, ManifestHash: manifest.Hash, Revision: manifest.Revision,
		SourceCommit: compatibility.SourceCommit, SchemaVersion: manifest.SchemaVersion,
		ParserVersion: canonical.ParserVersion, NormalizationVersion: canonical.NormalizationVersion,
		SegmentHashes: hashes, RecordCount: verification.RecordCount, GapCount: uint64(len(manifest.Gaps)),
		LowDensitySegments: lowDensity, Complete: manifest.Complete, Confidence: confidence}, nil
}

func confidenceFor(manifest recorder.DatasetManifest, lowDensity, maximum uint64) ConfidenceTier {
	if lowDensity > maximum {
		return ConfidenceC
	}
	if !manifest.Complete {
		return ConfidenceB
	}
	return ConfidenceA
}

func validCompatibility(value DatasetCompatibility) bool {
	return validCommit(value.SourceCommit) && value.ParserVersion != "" && value.NormalizationVersion != "" &&
		value.MinimumRecordsPerPair > 0
}

func schemaForKind(kind string) string {
	if kind == "wire" {
		return segments.WireSchemaVersion
	}
	if kind == "canonical" {
		return segments.CanonicalSchemaVersion
	}
	return ""
}

func cloneRecord(record segments.Record) segments.Record {
	record.Canonical = append([]byte(nil), record.Canonical...)
	return record
}
