package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"axiom/internal/storage/segments"
)

// Flush finalizes one linked wire/canonical pair and an immutable cumulative
// dataset-manifest revision.
func (recorder *Recorder) Flush() (DatasetManifest, error) {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	if len(recorder.raw) == 0 || len(recorder.raw) != len(recorder.canonical) {
		return DatasetManifest{}, recorderError("segment_incomplete")
	}
	raw := append([]segments.WireRow(nil), recorder.raw...)
	canonical := append([]segments.CanonicalRow(nil), recorder.canonical...)
	sort.Slice(raw, func(left, right int) bool { return raw[left].IngestOrdinal < raw[right].IngestOrdinal })
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].IngestOrdinal < canonical[right].IngestOrdinal })
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
	recorder.segments = append(recorder.segments, references...)
	recorder.rawCount += uint64(len(raw))
	recorder.canonicalCount += uint64(len(canonical))
	manifest := recorder.newManifest(revision)
	if err = writeManifest(recorder.root, manifest); err != nil {
		return DatasetManifest{}, err
	}
	recorder.revision, recorder.previous = revision, manifest.Hash
	for _, row := range raw {
		delete(recorder.links, row.IngestOrdinal)
	}
	recorder.raw, recorder.canonical = nil, nil
	recorder.pendingBytes, recorder.reservedBytes = 0, 0
	return cloneManifest(manifest), nil
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
		return segments.Manifest{}, recorderError("wire_finalize_failed")
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
		return segments.Manifest{}, recorderError("canonical_finalize_failed")
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

func (recorder *Recorder) newManifest(revision uint64) DatasetManifest {
	manifest := DatasetManifest{SchemaVersion: datasetSchemaVersion, DatasetID: recorder.datasetID,
		SessionID: recorder.sessionID, Exchange: recorder.exchange, Revision: revision,
		PreviousHash: recorder.previous, CreatedAt: recorder.now(), Segments: cloneReferences(recorder.segments),
		Gaps: append([]Gap(nil), recorder.gaps...), RawRecordCount: recorder.rawCount,
		CanonicalCount: recorder.canonicalCount, Complete: len(recorder.gaps) == 0}
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
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return recorderError("manifest_encode_failed")
	}
	name := fmt.Sprintf("%s-%06d.dataset.json", manifest.SessionID, manifest.Revision)
	partial, final := filepath.Join(root, name+".partial"), filepath.Join(root, name)
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return recorderError("manifest_create_failed")
	}
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return recorderError("manifest_write_failed")
	}
	if err = os.Rename(partial, final); err != nil {
		return recorderError("manifest_rename_failed")
	}
	directory, err := os.Open(root)
	if err != nil {
		return recorderError("manifest_sync_failed")
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err != nil || closeErr != nil {
		return recorderError("manifest_sync_failed")
	}
	return nil
}

func cloneManifest(manifest DatasetManifest) DatasetManifest {
	manifest.Segments = cloneReferences(manifest.Segments)
	manifest.Gaps = append([]Gap(nil), manifest.Gaps...)
	return manifest
}

func cloneReferences(references []SegmentReference) []SegmentReference {
	return append([]SegmentReference(nil), references...)
}
