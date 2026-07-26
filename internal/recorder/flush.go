package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"axiom/internal/storage/segments"
)

// Flush finalizes one linked wire/canonical pair and an immutable cumulative
// dataset-manifest revision.
func (recorder *Recorder) Flush() (DatasetManifest, error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	raw, canonical := recorder.completePrefix()
	if len(raw) == 0 {
		if len(recorder.raw) == 0 {
			return cloneManifest(recorder.latest), nil
		}
		return DatasetManifest{}, recorderError("segment_incomplete")
	}
	return recorder.flushCompletedLocked(raw, canonical)
}

// FlushReady finalizes the complete prefix available at this instant. A raw
// event whose canonical pair is still being built remains pending without
// turning a routine flush tick into a recorder failure.
func (recorder *Recorder) FlushReady() (DatasetManifest, bool, error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	raw, canonical := recorder.completePrefix()
	if len(raw) == 0 {
		return cloneManifest(recorder.latest), false, nil
	}
	manifest, err := recorder.flushCompletedLocked(raw, canonical)
	return manifest, true, err
}

func (recorder *Recorder) flushCompletedLocked(raw []segments.WireRow,
	canonical []segments.CanonicalRow) (DatasetManifest, error) {
	if err := verifyPendingLinks(raw, canonical); err != nil {
		return DatasetManifest{}, err
	}
	revision := recorder.revision + 1
	wireManifest, err := recorder.finalizeWire(revision, raw)
	if err != nil {
		return DatasetManifest{}, err
	}
	canonicalManifest, err := recorder.finalizeCanonical(revision, canonical)
	if err != nil {
		return DatasetManifest{}, err
	}
	references := []SegmentReference{{Kind: "wire", Manifest: wireManifest},
		{Kind: "canonical", Manifest: canonicalManifest}}
	segmentReferences := append(cloneReferences(recorder.segments), references...)
	rawCount := recorder.rawCount + uint64(len(raw))
	canonicalCount := recorder.canonicalCount + uint64(len(canonical))
	generationCoverage := recorder.nextGenerationCoverage(raw)
	manifest := recorder.newManifest(revision, segmentReferences, rawCount, canonicalCount, generationCoverage)
	if err = writeManifest(recorder.root, manifest); err != nil {
		return DatasetManifest{}, err
	}
	recorder.segments, recorder.rawCount, recorder.canonicalCount = segmentReferences, rawCount, canonicalCount
	recorder.revision, recorder.previous, recorder.latest = revision, manifest.Hash, cloneManifest(manifest)
	recorder.generationCoverage = generationCoverage
	recorder.discardFlushed(len(raw))
	return cloneManifest(manifest), nil
}

func (recorder *Recorder) completePrefix() ([]segments.WireRow, []segments.CanonicalRow) {
	ready := 0
	for ready < len(recorder.raw) {
		record := recorder.links[recorder.raw[ready].IngestOrdinal]
		if record == nil || !record.canonical {
			break
		}
		ready++
	}
	if ready == 0 {
		return nil, nil
	}
	raw := append([]segments.WireRow(nil), recorder.raw[:ready]...)
	canonicalByOrdinal := make(map[uint64]segments.CanonicalRow, ready)
	for _, row := range recorder.canonical {
		canonicalByOrdinal[row.IngestOrdinal] = row
	}
	canonical := make([]segments.CanonicalRow, 0, ready)
	for _, row := range raw {
		value, ok := canonicalByOrdinal[row.IngestOrdinal]
		if !ok {
			return nil, nil
		}
		canonical = append(canonical, value)
	}
	return raw, canonical
}

func (recorder *Recorder) discardFlushed(count int) {
	flushed := make(map[uint64]struct{}, count)
	for _, row := range recorder.raw[:count] {
		flushed[row.IngestOrdinal] = struct{}{}
		delete(recorder.links, row.IngestOrdinal)
	}
	recorder.raw = append([]segments.WireRow(nil), recorder.raw[count:]...)
	remainingCanonical := recorder.canonical[:0]
	for _, row := range recorder.canonical {
		if _, ok := flushed[row.IngestOrdinal]; !ok {
			remainingCanonical = append(remainingCanonical, row)
		}
	}
	recorder.canonical = append([]segments.CanonicalRow(nil), remainingCanonical...)
	recorder.pendingBytes, recorder.reservedBytes = 0, 0
	for _, row := range recorder.raw {
		recorder.pendingBytes += uint64(len(row.Payload) + recordMemoryOverhead)
		record := recorder.links[row.IngestOrdinal]
		if record != nil && !record.canonical {
			recorder.reservedBytes += uint64(maximumEventBytes + recordMemoryOverhead)
		}
	}
	for _, row := range recorder.canonical {
		recorder.pendingBytes += uint64(len(row.CanonicalEvent) + recordMemoryOverhead)
	}
	recorder.updateCapacitySignalLocked()
}

func (recorder *Recorder) finalizeWire(revision uint64, rows []segments.WireRow) (segments.Manifest, error) {
	hash, err := segments.HashWireRows(rows)
	if err != nil {
		return segments.Manifest{}, recorderError("wire_hash_failed")
	}
	writer, err := segments.NewWireParquetWriter(rows)
	if err != nil {
		return segments.Manifest{}, recorderError("wire_writer_failed")
	}
	spec := segmentSpec(recorder.sessionID, "wire", revision, rows[0].IngestOrdinal,
		rows[len(rows)-1].IngestOrdinal, uint64(len(rows)), rows[0].ReceivedAtUnixNano,
		rows[len(rows)-1].ReceivedAtUnixNano, segments.WireSchemaVersion, "wire", "wire", hash)
	manifest, err := recorder.finalizer.Finalize(spec, writer, recorder.commit)
	if err != nil {
		return segments.Manifest{}, recorderFinalizeError("wire_finalize_failed", "wire_finalize", err)
	}
	return manifest, nil
}

func (recorder *Recorder) finalizeCanonical(
	revision uint64,
	rows []segments.CanonicalRow,
) (segments.Manifest, error) {
	hash, err := segments.HashCanonicalRows(rows)
	if err != nil {
		return segments.Manifest{}, recorderError("canonical_hash_failed")
	}
	writer, err := segments.NewCanonicalParquetWriter(rows)
	if err != nil {
		return segments.Manifest{}, recorderError("canonical_writer_failed")
	}
	spec := segmentSpec(recorder.sessionID, "canonical", revision, rows[0].IngestOrdinal,
		rows[len(rows)-1].IngestOrdinal, uint64(len(rows)), rows[0].ReceivedAtUnixNano,
		rows[len(rows)-1].ReceivedAtUnixNano, segments.CanonicalSchemaVersion,
		rows[0].ParserVersion, rows[0].NormalizationVersion, hash)
	manifest, err := recorder.finalizer.Finalize(spec, writer, recorder.commit)
	if err != nil {
		return segments.Manifest{}, recorderFinalizeError("canonical_finalize_failed", "canonical_finalize", err)
	}
	return manifest, nil
}

func segmentSpec(
	session, kind string,
	revision, first, last, count uint64,
	started, ended int64,
	schema, parser, normalizer, hash string,
) segments.Spec {
	name := fmt.Sprintf("%s-%06d-%s", session, revision, kind)
	return segments.Spec{Name: name, SchemaVersion: schema, ParserVersion: parser,
		NormalizationVersion: normalizer, OrderedContentHash: hash, FirstOrdinal: first,
		LastOrdinal: last, RecordCount: count, StartedAt: time.Unix(0, started).UTC(), EndedAt: time.Unix(0, ended).UTC()}
}

func verifyPendingLinks(raw []segments.WireRow, canonical []segments.CanonicalRow) error {
	for index := range raw {
		if raw[index].IngestOrdinal != canonical[index].IngestOrdinal ||
			raw[index].PayloadSHA256 != canonical[index].WirePayloadSHA256 {
			return recorderError("segment_link_mismatch")
		}
	}
	return nil
}

func boundedCause(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	for _, character := range value {
		if (character < 'a' || character > 'z') && character != '_' && (character < '0' || character > '9') {
			return "storage_failure"
		}
	}
	if len(value) == 0 || len(value) > 96 {
		return "storage_failure"
	}
	return value
}

func (recorder *Recorder) newManifest(
	revision uint64,
	references []SegmentReference,
	rawCount, canonicalCount uint64,
	generationCoverage map[uint64]GenerationCoverage,
) DatasetManifest {
	manifest := DatasetManifest{SchemaVersion: datasetSchemaVersion, DatasetID: recorder.datasetID,
		SessionID: recorder.sessionID, Exchange: recorder.exchange, Revision: revision,
		PreviousHash: recorder.previous, CreatedAt: recorder.now(), Segments: cloneReferences(references),
		Gaps: append([]Gap(nil), recorder.gaps...), RawRecordCount: rawCount,
		CanonicalCount: canonicalCount, Complete: len(recorder.gaps) == 0}
	if recorder.profile != nil {
		manifest.SchemaVersion = datasetSchemaVersionV2
		manifest.QualityTier = "candidate"
		coverage := recorder.exchangeCoverage(references, rawCount, canonicalCount, generationCoverage)
		manifest.ExchangeCoverage = []ExchangeCoverage{coverage}
		manifest.Compatibility = &CompatibilityRequirements{MinimumReaderVersion: recorder.profile.MinimumReaderVersion,
			SchemaVersions:        append([]string(nil), coverage.SchemaVersions...),
			ParserVersions:        append([]string(nil), coverage.ParserVersions...),
			NormalizationVersions: append([]string(nil), coverage.NormalizationVersions...)}
	}
	manifest.Hash = manifestHash(manifest)
	return manifest
}

func manifestHash(manifest DatasetManifest) string {
	manifest.Hash = ""
	encoded, _ := json.Marshal(manifest)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func writeManifest(root string, manifest DatasetManifest) error {
	if err := validateManifest(manifest); err != nil {
		return err
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return recorderError("manifest_encode_failed")
	}
	name := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	partial, final := filepath.Join(root, name+".partial"), filepath.Join(root, name)
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return recorderIOError("manifest_create_failed", "manifest_create", err)
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return recorderIOError("manifest_write_failed", "manifest_write_sync_close", err)
	}
	if err = os.Rename(partial, final); err != nil {
		return recorderIOError("manifest_rename_failed", "manifest_rename", err)
	}
	directory, err := os.Open(root)
	if err != nil {
		return recorderIOError("manifest_sync_failed", "manifest_directory_open", err)
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err != nil {
		return recorderIOError("manifest_sync_failed", "manifest_directory_sync", err)
	}
	if closeErr != nil {
		return recorderIOError("manifest_sync_failed", "manifest_directory_close", closeErr)
	}
	return nil
}

func cloneManifest(manifest DatasetManifest) DatasetManifest {
	manifest.Segments = cloneReferences(manifest.Segments)
	manifest.Gaps = append([]Gap(nil), manifest.Gaps...)
	manifest.ExchangeCoverage = cloneExchangeCoverage(manifest.ExchangeCoverage)
	if manifest.Compatibility != nil {
		copy := cloneCompatibility(*manifest.Compatibility)
		manifest.Compatibility = &copy
	}
	return manifest
}

func cloneReferences(references []SegmentReference) []SegmentReference {
	return append([]SegmentReference(nil), references...)
}
