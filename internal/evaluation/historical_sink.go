package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/storage/segments"
)

const (
	historicalParserBinance = "binance-historical-candle.v1"
	historicalParserBybit   = "bybit-historical-candle.v1"
	historicalNormalizer    = "axiom-historical-candle-page.v1"
)

// HistoricalStoredSegment is one DB-registered immutable page artifact.
type HistoricalStoredSegment struct {
	PageStart time.Time
	Kind      string
	Manifest  segments.Manifest
}

// HistoricalSegmentRepository commits segment manifests only after the file
// finalizer has renamed and fsynced the exact Parquet artifact.
type HistoricalSegmentRepository interface {
	CommitHistoricalSegment(context.Context, string, time.Time, string, segments.Manifest) error
	HistoricalPageSegments(context.Context, string, time.Time) ([]HistoricalStoredSegment, error)
	HistoricalImportSegments(context.Context, string) ([]HistoricalStoredSegment, error)
}

// HistoricalFileSink stores exact raw response pages and canonical candle
// arrays as linked immutable Parquet segment pairs in the configured recorder
// root. Deterministic names make crash recovery idempotent.
type HistoricalFileSink struct {
	root       string
	repository HistoricalSegmentRepository
}

// NewHistoricalFileSink constructs a crash-safe immutable historical page
// sink rooted at the dedicated evaluation history directory.
func NewHistoricalFileSink(root string, repository HistoricalSegmentRepository) (*HistoricalFileSink, error) {
	clean := filepath.Clean(root)
	if !filepath.IsAbs(clean) || clean == string(filepath.Separator) || repository == nil {
		return nil, fmt.Errorf("evaluation_historical_sink_dependencies_missing")
	}
	return &HistoricalFileSink{root: clean, repository: repository}, nil
}

// LoadHistoricalPage recovers and verifies an already committed page pair.
func (sink *HistoricalFileSink) LoadHistoricalPage(ctx context.Context,
	spec HistoricalImportSpec) (HistoricalPageArtifact, bool, error) {
	if err := sink.recover(ctx, spec); err != nil {
		return HistoricalPageArtifact{}, false, err
	}
	records, err := sink.repository.HistoricalPageSegments(ctx, spec.ID, spec.Checkpoint)
	if err != nil || len(records) == 0 {
		return HistoricalPageArtifact{}, false, err
	}
	if len(records) != 2 {
		return HistoricalPageArtifact{}, false, nil
	}
	return sink.verifyPage(spec, records)
}

// PersistHistoricalPage writes and verifies one raw/canonical Parquet page
// pair before returning a durable checkpoint.
func (sink *HistoricalFileSink) PersistHistoricalPage(ctx context.Context, spec HistoricalImportSpec,
	page exchangecontracts.HistoricalCandlePage) (HistoricalPageArtifact, error) {
	if artifact, found, err := sink.LoadHistoricalPage(ctx, spec); err != nil || found {
		return artifact, err
	}
	records, err := sink.repository.HistoricalPageSegments(ctx, spec.ID, spec.Checkpoint)
	if err != nil {
		return HistoricalPageArtifact{}, err
	}
	existing := make(map[string]segments.Manifest, len(records))
	for _, record := range records {
		if record.Kind != "wire" && record.Kind != "canonical" {
			return HistoricalPageArtifact{}, fmt.Errorf("evaluation_historical_segment_kind_invalid")
		}
		existing[record.Kind] = record.Manifest
	}
	wireRow, canonicalRow, err := historicalPageRows(spec, page)
	if err != nil {
		return HistoricalPageArtifact{}, err
	}
	finalizer, err := segments.NewFinalizer(sink.root, nil)
	if err != nil {
		return HistoricalPageArtifact{}, err
	}
	if _, ok := existing["wire"]; !ok {
		manifest, finalizeErr := sink.finalizeWire(ctx, finalizer, spec, wireRow)
		if finalizeErr != nil {
			return HistoricalPageArtifact{}, finalizeErr
		}
		existing["wire"] = manifest
	}
	if _, ok := existing["canonical"]; !ok {
		manifest, finalizeErr := sink.finalizeCanonical(ctx, finalizer, spec, canonicalRow)
		if finalizeErr != nil {
			return HistoricalPageArtifact{}, finalizeErr
		}
		existing["canonical"] = manifest
	}
	artifact, found, verifyErr := sink.verifyPage(spec, []HistoricalStoredSegment{
		{PageStart: spec.Checkpoint, Kind: "wire", Manifest: existing["wire"]},
		{PageStart: spec.Checkpoint, Kind: "canonical", Manifest: existing["canonical"]},
	})
	if verifyErr != nil {
		return HistoricalPageArtifact{}, verifyErr
	}
	if !found {
		return HistoricalPageArtifact{}, fmt.Errorf("evaluation_historical_page_not_durable")
	}
	return artifact, nil
}

func (sink *HistoricalFileSink) recover(ctx context.Context, spec HistoricalImportSpec) error {
	finalizer, err := segments.NewFinalizer(sink.root, nil)
	if err != nil {
		return err
	}
	prefix := historicalArtifactPrefix(spec.ID)
	_, err = finalizer.RecoverPrefix(prefix, func(manifest segments.Manifest) error {
		pageStart, kind, parseErr := parseHistoricalSegmentName(prefix, manifest.Spec.Name)
		if parseErr != nil {
			return parseErr
		}
		return sink.repository.CommitHistoricalSegment(ctx, spec.ID, pageStart, kind, manifest)
	})
	return err
}

func (sink *HistoricalFileSink) finalizeWire(ctx context.Context, finalizer *segments.Finalizer,
	spec HistoricalImportSpec, row segments.WireRow) (segments.Manifest, error) {
	orderedHash, err := segments.HashWireRows([]segments.WireRow{row})
	if err != nil {
		return segments.Manifest{}, err
	}
	writer, err := segments.NewWireParquetWriter([]segments.WireRow{row})
	if err != nil {
		return segments.Manifest{}, err
	}
	manifestSpec := historicalSegmentSpec(spec, "wire", row.IngestOrdinal, row.ReceivedAtUnixNano,
		segments.WireSchemaVersion, "wire", "wire", orderedHash)
	return finalizer.Finalize(manifestSpec, writer, func(manifest segments.Manifest) error {
		return sink.repository.CommitHistoricalSegment(ctx, spec.ID, spec.Checkpoint, "wire", manifest)
	})
}

func (sink *HistoricalFileSink) finalizeCanonical(ctx context.Context, finalizer *segments.Finalizer,
	spec HistoricalImportSpec, row segments.CanonicalRow) (segments.Manifest, error) {
	orderedHash, err := segments.HashCanonicalRows([]segments.CanonicalRow{row})
	if err != nil {
		return segments.Manifest{}, err
	}
	writer, err := segments.NewCanonicalParquetWriter([]segments.CanonicalRow{row})
	if err != nil {
		return segments.Manifest{}, err
	}
	manifestSpec := historicalSegmentSpec(spec, "canonical", row.IngestOrdinal, row.ReceivedAtUnixNano,
		segments.CanonicalSchemaVersion, row.ParserVersion, row.NormalizationVersion, orderedHash)
	return finalizer.Finalize(manifestSpec, writer, func(manifest segments.Manifest) error {
		return sink.repository.CommitHistoricalSegment(ctx, spec.ID, spec.Checkpoint, "canonical", manifest)
	})
}

func (sink *HistoricalFileSink) verifyPage(spec HistoricalImportSpec,
	records []HistoricalStoredSegment) (HistoricalPageArtifact, bool, error) {
	wireManifest, canonicalManifest, err := historicalPageManifests(records)
	if err != nil {
		return HistoricalPageArtifact{}, false, err
	}
	if wireManifest.Path == "" || canonicalManifest.Path == "" {
		return HistoricalPageArtifact{}, false, nil
	}
	if err := verifyHistoricalManifestFile(sink.root, wireManifest); err != nil {
		return HistoricalPageArtifact{}, false, err
	}
	if err := verifyHistoricalManifestFile(sink.root, canonicalManifest); err != nil {
		return HistoricalPageArtifact{}, false, err
	}
	wireRows, err := segments.ReadWireParquet(filepath.Join(sink.root, wireManifest.Path))
	if err != nil || len(wireRows) != 1 {
		return HistoricalPageArtifact{}, false, fmt.Errorf("evaluation_historical_wire_invalid")
	}
	canonicalRows, err := segments.ReadCanonicalParquetRows(filepath.Join(sink.root, canonicalManifest.Path),
		canonicalManifest.Spec)
	if err != nil || len(canonicalRows) != 1 ||
		wireRows[0].IngestOrdinal != canonicalRows[0].IngestOrdinal ||
		wireRows[0].PayloadSHA256 != canonicalRows[0].WirePayloadSHA256 {
		return HistoricalPageArtifact{}, false, fmt.Errorf("evaluation_historical_link_invalid")
	}
	var candles []exchangecontracts.Candle
	if json.Unmarshal(canonicalRows[0].CanonicalEvent, &candles) != nil || len(candles) == 0 {
		return HistoricalPageArtifact{}, false, fmt.Errorf("evaluation_historical_canonical_invalid")
	}
	duration, err := HistoricalIntervalDuration(spec.Interval)
	if err != nil || !candles[0].OpenTime.Equal(spec.Checkpoint) {
		return HistoricalPageArtifact{}, false, fmt.Errorf("evaluation_historical_checkpoint_invalid")
	}
	next := candles[len(candles)-1].OpenTime.Add(duration)
	page := exchangecontracts.HistoricalCandlePage{Exchange: exchangecontracts.ExchangeID(spec.Exchange),
		Instrument: spec.Instrument, Interval: spec.Interval, Start: spec.Checkpoint, End: next.Add(-time.Millisecond),
		ReceivedAt: candles[0].ReceivedAt, RawPayload: wireRows[0].Payload,
		RawPayloadHash: hex.EncodeToString(wireRows[0].PayloadSHA256[:]), Candles: candles}
	if err = validateHistoricalPage(spec, page, duration, page.End); err != nil {
		return HistoricalPageArtifact{}, false, err
	}
	return HistoricalPageArtifact{ID: historicalPageArtifactID(spec.ID, spec.Checkpoint),
		ByteCount: wireManifest.Size + canonicalManifest.Size, RawPayloadHash: wireRows[0].PayloadSHA256,
		NormalizedPageHash: canonicalRows[0].CanonicalSHA256, NextCheckpoint: next,
		RowCount: uint64(len(candles))}, true, nil
}

func historicalPageManifests(records []HistoricalStoredSegment) (segments.Manifest, segments.Manifest, error) {
	var wireManifest, canonicalManifest segments.Manifest
	for _, record := range records {
		switch record.Kind {
		case "wire":
			wireManifest = record.Manifest
		case "canonical":
			canonicalManifest = record.Manifest
		default:
			return segments.Manifest{}, segments.Manifest{}, fmt.Errorf("evaluation_historical_segment_kind_invalid")
		}
	}
	return wireManifest, canonicalManifest, nil
}

func historicalPageRows(spec HistoricalImportSpec, page exchangecontracts.HistoricalCandlePage) (
	segments.WireRow, segments.CanonicalRow, error) {
	duration, err := HistoricalIntervalDuration(spec.Interval)
	if err != nil {
		return segments.WireRow{}, segments.CanonicalRow{}, err
	}
	pageIndex := uint64(spec.Checkpoint.Sub(spec.WindowStart)/duration)/uint64(historicalPageLimit) + 1
	canonical, err := json.Marshal(page.Candles)
	if err != nil || len(canonical) == 0 {
		return segments.WireRow{}, segments.CanonicalRow{}, fmt.Errorf("evaluation_historical_canonical_invalid")
	}
	last := page.Candles[len(page.Candles)-1]
	logical := uint64(last.CloseTime.UnixNano())
	exchangeTime := last.CloseTime.UnixNano()
	rawHash := sha256.Sum256(page.RawPayload)
	row := segments.WireRow{IngestOrdinal: pageIndex, Exchange: spec.Exchange, EventType: "candle",
		BaseAsset: string(spec.Instrument.Base), QuoteAsset: string(spec.Instrument.Quote),
		SourceSessionID: HistoricalImportSessionID(spec.ID), ConnectionID: "official-rest-history",
		ConnectionGeneration: 1, MonotonicOffsetNanos: logical, RecordedLogicalTime: logical,
		SourceSequence: strconv.FormatInt(spec.Checkpoint.UnixMilli(), 10), ExchangeTimeUnixNano: &exchangeTime,
		ReceivedAtUnixNano: page.ReceivedAt.UTC.UnixNano(), Payload: append([]byte(nil), page.RawPayload...),
		PayloadSHA256: rawHash}
	parser := historicalParserBinance
	if spec.Exchange == "bybit" {
		parser = historicalParserBybit
	}
	canonicalHash := sha256.Sum256(canonical)
	canonicalRow := segments.CanonicalRow{IngestOrdinal: pageIndex,
		EventID: historicalPageArtifactID(spec.ID, spec.Checkpoint), Exchange: spec.Exchange, EventType: "candle",
		BaseAsset: row.BaseAsset, QuoteAsset: row.QuoteAsset, SourceSessionID: row.SourceSessionID,
		ConnectionID: row.ConnectionID, ConnectionGeneration: 1, MonotonicOffsetNanos: logical,
		RecordedLogicalTime: logical, SourceSequence: row.SourceSequence, ExchangeTimeUnixNano: &exchangeTime,
		ReceivedAtUnixNano: row.ReceivedAtUnixNano, ParserVersion: parser, NormalizationVersion: historicalNormalizer,
		WirePayloadSHA256: rawHash, CanonicalEvent: canonical, CanonicalSHA256: canonicalHash}
	if segments.ValidateWireRow(row) != nil || segments.ValidateCanonicalRow(canonicalRow) != nil {
		return segments.WireRow{}, segments.CanonicalRow{}, fmt.Errorf("evaluation_historical_rows_invalid")
	}
	return row, canonicalRow, nil
}

func historicalSegmentSpec(spec HistoricalImportSpec, kind string, ordinal uint64, receivedAt int64,
	schema, parser, normalizer, orderedHash string) segments.Spec {
	return segments.Spec{Name: historicalPageArtifactID(spec.ID, spec.Checkpoint) + "-" + kind,
		SchemaVersion: schema, ParserVersion: parser, NormalizationVersion: normalizer,
		OrderedContentHash: orderedHash, FirstOrdinal: ordinal, LastOrdinal: ordinal, RecordCount: 1,
		StartedAt: time.Unix(0, receivedAt).UTC(), EndedAt: time.Unix(0, receivedAt).UTC()}
}

func historicalArtifactPrefix(importID string) string {
	digest := sha256.Sum256([]byte(importID))
	return "evalhist-" + hex.EncodeToString(digest[:8]) + "-"
}

func historicalPageArtifactID(importID string, start time.Time) string {
	return historicalArtifactPrefix(importID) + strconv.FormatInt(start.Unix(), 10)
}

// HistoricalImportSessionID is the deterministic filesystem-safe recorder
// session identity for one import stream.
func HistoricalImportSessionID(importID string) string {
	return strings.TrimSuffix(historicalArtifactPrefix(importID), "-")
}

// HistoricalImportDatasetID is the deterministic recorder dataset identity
// registered after every page is complete and verified.
func HistoricalImportDatasetID(importID string) string {
	digest := sha256.Sum256([]byte("dataset:" + importID))
	return "evalhistdataset-" + hex.EncodeToString(digest[:8])
}

func parseHistoricalSegmentName(prefix, name string) (time.Time, string, error) {
	value := strings.TrimPrefix(name, prefix)
	if value == name {
		return time.Time{}, "", fmt.Errorf("evaluation_historical_segment_name_invalid")
	}
	separator := strings.LastIndexByte(value, '-')
	if separator <= 0 {
		return time.Time{}, "", fmt.Errorf("evaluation_historical_segment_name_invalid")
	}
	kind := value[separator+1:]
	seconds, err := strconv.ParseInt(value[:separator], 10, 64)
	if err != nil || (kind != "wire" && kind != "canonical") {
		return time.Time{}, "", fmt.Errorf("evaluation_historical_segment_name_invalid")
	}
	return time.Unix(seconds, 0).UTC(), kind, nil
}

func verifyHistoricalManifestFile(root string, manifest segments.Manifest) error {
	if manifest.Path == "" || filepath.Base(manifest.Path) != manifest.Path ||
		manifest.Path != manifest.Spec.Name+".parquet" || manifest.Size <= 0 {
		return fmt.Errorf("evaluation_historical_manifest_invalid")
	}
	file, err := os.Open(filepath.Join(root, manifest.Path))
	if err != nil {
		return fmt.Errorf("evaluation_historical_file_unavailable")
	}
	defer file.Close()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil || size != manifest.Size || hex.EncodeToString(digest.Sum(nil)) != manifest.Checksum {
		return fmt.Errorf("evaluation_historical_checksum_invalid")
	}
	return nil
}

var _ HistoricalPageSink = (*HistoricalFileSink)(nil)
