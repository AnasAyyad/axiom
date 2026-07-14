package segments

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatasetReaderValidatesAndPreservesManifestOrder(t *testing.T) {
	root := t.TempDir()
	manifest := finalizedManifest(t, root)
	reader, err := NewReader(root, readerCompatibility("raw.v1"), func(_ string, _ Spec) ([]Record, error) {
		return []Record{{RecordedLogicalTime: 1, IngestOrdinal: 1, Canonical: []byte("a")}, {RecordedLogicalTime: 2, IngestOrdinal: 2, Canonical: []byte("b")}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := reader.Read(datasetFixture(manifest))
	if err != nil || len(records) != 2 || records[0].IngestOrdinal != 1 || records[1].IngestOrdinal != 2 {
		t.Fatalf("records = %#v, %v", records, err)
	}
	records[0].Canonical[0] = 'z'
	again, _ := reader.Read(datasetFixture(manifest))
	if string(again[0].Canonical) != "a" {
		t.Fatal("reader result retained caller mutation")
	}
}

func TestDatasetReaderRejectsCorruptionCompatibilityAndReordering(t *testing.T) {
	root := t.TempDir()
	manifest := finalizedManifest(t, root)
	decode := func(_ string, _ Spec) ([]Record, error) {
		return []Record{{RecordedLogicalTime: 2, IngestOrdinal: 2, Canonical: []byte("b")}, {RecordedLogicalTime: 1, IngestOrdinal: 1, Canonical: []byte("a")}}, nil
	}
	reader, _ := NewReader(root, readerCompatibility("raw.v1"), decode)
	if _, err := reader.Read(datasetFixture(manifest)); err == nil {
		t.Fatal("decoder reordering accepted")
	}
	incompatible, _ := NewReader(root, readerCompatibility("raw.v2"), decode)
	if _, err := incompatible.Read(datasetFixture(manifest)); err == nil {
		t.Fatal("incompatible schema accepted")
	}
	compatibleDecode := func(_ string, _ Spec) ([]Record, error) {
		return []Record{{RecordedLogicalTime: 1, IngestOrdinal: 1, Canonical: []byte("a")}, {RecordedLogicalTime: 2, IngestOrdinal: 2, Canonical: []byte("b")}}, nil
	}
	compatible, _ := NewReader(root, readerCompatibility("raw.v1"), compatibleDecode)
	parserMismatch := manifest
	parserMismatch.Spec.ParserVersion = "parser.v2"
	if _, err := compatible.Read(datasetFixture(parserMismatch)); err == nil {
		t.Fatal("incompatible parser accepted")
	}
	normalizationMismatch := manifest
	normalizationMismatch.Spec.NormalizationVersion = "normalized.v2"
	if _, err := compatible.Read(datasetFixture(normalizationMismatch)); err == nil {
		t.Fatal("incompatible normalization accepted")
	}
	hashMismatch := manifest
	hashMismatch.OrderedContentHash = strings.Repeat("b", 64)
	if _, err := compatible.Read(datasetFixture(hashMismatch)); err == nil {
		t.Fatal("manifest ordered-content mismatch accepted")
	}
	file, _ := os.OpenFile(filepath.Join(root, manifest.Path), os.O_WRONLY|os.O_APPEND, 0)
	_, _ = file.Write([]byte("corrupt"))
	_ = file.Close()
	if _, err := reader.Read(datasetFixture(manifest)); err == nil {
		t.Fatal("corrupt segment accepted")
	}
}

func TestDatasetReaderRequiresExplicitValidGapOrdering(t *testing.T) {
	root := t.TempDir()
	manifest := finalizedManifest(t, root)
	reader, _ := NewReader(root, readerCompatibility("raw.v1"), func(_ string, _ Spec) ([]Record, error) {
		return []Record{{RecordedLogicalTime: 1, IngestOrdinal: 1, Canonical: []byte("a")}, {RecordedLogicalTime: 2, IngestOrdinal: 2, Canonical: []byte("b")}}, nil
	})
	dataset := datasetFixture(manifest)
	dataset.Gaps = []Gap{{FirstOrdinal: 5, LastOrdinal: 4, Reason: "loss"}}
	if _, err := reader.Read(dataset); err == nil {
		t.Fatal("invalid explicit gap accepted")
	}
	dataset.Gaps = []Gap{{FirstOrdinal: 1, LastOrdinal: 1, Reason: "claimed_loss"}}
	if _, err := reader.Read(dataset); err == nil {
		t.Fatal("gap overlapping a recorded ordinal accepted")
	}
}

func TestDatasetReaderRejectsSymlinkedSegment(t *testing.T) {
	root := t.TempDir()
	manifest := finalizedManifest(t, root)
	path := filepath.Join(root, manifest.Path)
	realPath := path + ".real"
	if err := os.Rename(path, realPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, path); err != nil {
		t.Fatal(err)
	}
	reader, _ := NewReader(root, readerCompatibility("raw.v1"), func(_ string, _ Spec) ([]Record, error) {
		return []Record{{RecordedLogicalTime: 1, IngestOrdinal: 1, Canonical: []byte("a")}}, nil
	})
	if _, err := reader.Read(datasetFixture(manifest)); err == nil {
		t.Fatal("symlinked dataset segment accepted")
	}
}

func TestDatasetReaderUsesLogicalTimeThenUniqueIngestOrdinal(t *testing.T) {
	root := t.TempDir()
	manifest := finalizedManifest(t, root)
	reader, _ := NewReader(root, readerCompatibility("raw.v1"), func(_ string, _ Spec) ([]Record, error) {
		return []Record{
			{RecordedLogicalTime: 7, IngestOrdinal: 1, Canonical: []byte("a")},
			{RecordedLogicalTime: 7, IngestOrdinal: 2, Canonical: []byte("b")},
		}, nil
	})
	if _, err := reader.Read(datasetFixture(manifest)); err != nil {
		t.Fatalf("logical-time tie with increasing ordinal rejected: %v", err)
	}
	duplicate, _ := NewReader(root, readerCompatibility("raw.v1"), func(_ string, _ Spec) ([]Record, error) {
		return []Record{
			{RecordedLogicalTime: 7, IngestOrdinal: 1, Canonical: []byte("a")},
			{RecordedLogicalTime: 8, IngestOrdinal: 1, Canonical: []byte("b")},
		}, nil
	})
	manifest.Spec.LastOrdinal = 1
	if _, err := duplicate.Read(datasetFixture(manifest)); err == nil {
		t.Fatal("duplicate ingest ordinal accepted across logical times")
	}
	dataset := datasetFixture(manifest)
	dataset.OrderingVersion = "dataset-order.v0"
	if _, err := duplicate.Read(dataset); err == nil {
		t.Fatal("unsupported dataset ordering version accepted")
	}
}

func datasetFixture(manifest Manifest) Dataset {
	return Dataset{
		ID: "dataset-a", OrderingVersion: "dataset-order.v1", OrderingScope: "recorder-session-a",
		Segments: []Manifest{manifest},
	}
}

func readerCompatibility(schema string) Compatibility {
	return Compatibility{
		SchemaVersions: []string{schema}, ParserVersions: []string{"parser.v1"},
		NormalizationVersions: []string{"normalized.v1"},
	}
}

func finalizedManifest(t *testing.T, root string) Manifest {
	t.Helper()
	finalizer, _ := NewFinalizer(root, nil)
	manifest, err := finalizer.Finalize(segmentSpec(), parquetFixture, func(Manifest) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
