package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"axiom/internal/recorder"
	"axiom/internal/storage/segments"
)

const maximumAuditClockLead = 5 * time.Second

// RecordedDatasetExpectation is the DB identity that a filesystem manifest
// must match before any of its rows can be eligible.
type RecordedDatasetExpectation struct {
	DatasetID         string
	RecorderDatasetID string
	ManifestHash      string
	ManifestRevision  uint64
	ManifestPath      string
	ExpectedExchange  string
}

// DataAuditFinding is bounded evidence; it contains no payload or path.
type DataAuditFinding struct {
	DatasetID      string
	ExchangeID     string
	InstrumentID   string
	Eligibility    string
	ReasonCode     string
	ManifestHash   [sha256.Size]byte
	SegmentCount   uint64
	ByteCount      uint64
	GapCount       uint64
	DuplicateCount uint64
}

// AuditRecordedDataset verifies manifest identity and chain, Parquet hashes,
// raw/canonical linkage, ordinals, source monotonicity, instruments, clocks,
// gaps, and reader compatibility without changing any file.
func AuditRecordedDataset(root string, expected RecordedDatasetExpectation) DataAuditFinding {
	finding := DataAuditFinding{DatasetID: expected.DatasetID, Eligibility: "ineligible",
		ReasonCode: "MANIFEST_UNAVAILABLE"}
	if expected.DatasetID == "" || expected.RecorderDatasetID == "" || expected.ManifestHash == "" ||
		expected.ManifestRevision == 0 || filepath.Base(expected.ManifestPath) != expected.ManifestPath {
		return finding
	}
	digest, err := hex.DecodeString(expected.ManifestHash)
	if err != nil || len(digest) != sha256.Size {
		finding.ReasonCode = "MANIFEST_HASH_INVALID"
		return finding
	}
	copy(finding.ManifestHash[:], digest)
	manifest, err := recorder.ReadManifest(filepath.Join(root, expected.ManifestPath))
	if err != nil || manifest.DatasetID != expected.RecorderDatasetID || manifest.Hash != expected.ManifestHash ||
		manifest.Revision != expected.ManifestRevision {
		finding.ReasonCode = "MANIFEST_IDENTITY_MISMATCH"
		return finding
	}
	finding.ExchangeID = manifest.Exchange
	finding.SegmentCount = uint64(len(manifest.Segments))
	for _, reference := range manifest.Segments {
		if reference.Manifest.Size > 0 {
			finding.ByteCount += uint64(reference.Manifest.Size)
		}
	}
	finding.GapCount = uint64(len(manifest.Gaps))
	if expected.ExpectedExchange != "" && manifest.Exchange != expected.ExpectedExchange {
		finding.ReasonCode = "EXCHANGE_IDENTITY_MISMATCH"
		return finding
	}
	if recorder.VerifyManifestChain(root, manifest) != nil {
		finding.ReasonCode = "MANIFEST_CHAIN_INVALID"
		return finding
	}
	if _, err = recorder.VerifyDataset(root, manifest); err != nil {
		finding.ReasonCode = "PARQUET_OR_LINKAGE_INVALID"
		return finding
	}
	if err = auditRecordedRows(root, manifest, &finding); err != nil {
		finding.ReasonCode = err.Error()
		return finding
	}
	if finding.GapCount != 0 || !manifest.Complete {
		finding.ReasonCode = "DECLARED_GAPS_INELIGIBLE"
		return finding
	}
	finding.Eligibility, finding.ReasonCode = "eligible", "ELIGIBLE_VERIFIED"
	return finding
}

func auditRecordedRows(root string, manifest recorder.DatasetManifest, finding *DataAuditFinding) error {
	var priorOrdinal uint64
	lastSequence := make(map[string]string)
	instruments := make(map[string]struct{})
	for index := 0; index < len(manifest.Segments); index += 2 {
		wireReference := manifest.Segments[index]
		if wireReference.Kind != "wire" || index+1 >= len(manifest.Segments) ||
			manifest.Segments[index+1].Kind != "canonical" {
			return fmt.Errorf("SEGMENT_PAIR_INVALID")
		}
		wireRows, err := segments.ReadWireParquet(filepath.Join(root, wireReference.Manifest.Path))
		if err != nil {
			return fmt.Errorf("WIRE_SEGMENT_INVALID")
		}
		for _, row := range wireRows {
			if row.IngestOrdinal <= priorOrdinal {
				return fmt.Errorf("ORDINAL_REGRESSION")
			}
			priorOrdinal = row.IngestOrdinal
			if row.Exchange != manifest.Exchange || !auditInstrumentAllowed(row.BaseAsset, row.QuoteAsset) {
				return fmt.Errorf("UNIVERSE_INVALID")
			}
			instrument := row.BaseAsset + "/" + row.QuoteAsset
			instruments[instrument] = struct{}{}
			received := time.Unix(0, row.ReceivedAtUnixNano).UTC()
			if row.ExchangeTimeUnixNano != nil && time.Unix(0, *row.ExchangeTimeUnixNano).UTC().After(received.Add(maximumAuditClockLead)) {
				return fmt.Errorf("CLOCK_UNSAFE")
			}
			if row.SourceSequence != "" {
				key := row.Exchange + ":" + instrument + ":" + row.EventType + ":" + row.ConnectionID + ":" +
					strconv.FormatUint(row.ConnectionGeneration, 10)
				if lastSequence[key] == row.SourceSequence {
					finding.DuplicateCount++
				}
				lastSequence[key] = row.SourceSequence
			}
		}
	}
	if finding.DuplicateCount != 0 {
		return fmt.Errorf("DUPLICATE_SOURCE_SEQUENCE")
	}
	if len(instruments) == 1 {
		for instrument := range instruments {
			finding.InstrumentID = instrument
		}
	} else if len(instruments) > 1 {
		finding.InstrumentID = "multiple"
	}
	return nil
}

func auditInstrumentAllowed(base, quote string) bool {
	symbol := base + quote
	return symbol == "BTCUSDT" || symbol == "ETHUSDT" || symbol == "ETHBTC"
}
