package recorder

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/storage/segments"
)

func TestQuarantineUncommittedArtifactsPreservesCommittedRevision(t *testing.T) {
	recording, err := testRecorder(t)
	if err != nil {
		t.Fatal(err)
	}
	recordPair(t, recording, 1, `{"wire":1}`, `{"canonical":1}`)
	committed, err := recording.Flush()
	if err != nil {
		t.Fatal(err)
	}
	orphanName := "session-public-data-000002-wire-orphan"
	orphanSpec := recoverySegmentSpec(orphanName)
	finalizer, _ := segments.NewFinalizer(recording.root, func(stage segments.Stage) error {
		if stage == segments.StageDirectorySynced {
			return errors.New("injected crash")
		}
		return nil
	})
	if _, err = finalizer.Finalize(orphanSpec, recoveryParquetFixture, func(segments.Manifest) error {
		t.Fatal("orphan unexpectedly committed")
		return nil
	}); err == nil {
		t.Fatal("injected crash did not stop finalization")
	}
	partial := filepath.Join(recording.root, orphanName+".parquet.partial")
	if err = os.WriteFile(partial, []byte("PAR1later-unproved-partialPAR1"), 0o640); err != nil {
		t.Fatal(err)
	}
	callbackCalls := 0
	result, err := QuarantineUncommittedArtifacts(recording.root, recording.sessionID, committed,
		func(names []string, proven []segments.Manifest) error {
			callbackCalls++
			if len(names) != 1 || names[0] != orphanName || len(proven) != 1 || proven[0].Spec != orphanSpec {
				t.Fatalf("names=%v proven=%#v", names, proven)
			}
			return nil
		})
	if err != nil || callbackCalls != 1 || len(result.Moved) != 3 {
		t.Fatalf("result=%#v calls=%d error=%v", result, callbackCalls, err)
	}
	assertRecoveryFilesPreserved(t, recording.root, orphanName, committed)
	if repeated, repeatErr := QuarantineUncommittedArtifacts(recording.root, recording.sessionID, committed,
		func([]string, []segments.Manifest) error {
			t.Fatal("idempotent recovery invoked persistence callback")
			return nil
		}); repeatErr != nil || len(repeated.Moved) != 0 {
		t.Fatalf("repeat=%#v error=%v", repeated, repeatErr)
	}
}

func assertRecoveryFilesPreserved(t *testing.T, root, orphanName string, committed DatasetManifest) {
	t.Helper()
	for _, reference := range committed.Segments {
		if _, err := os.Stat(filepath.Join(root, reference.Manifest.Path)); err != nil {
			t.Fatalf("committed segment moved: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, orphanName+".parquet")); !os.IsNotExist(err) {
		t.Fatal("orphan final remained live")
	}
}

func TestSegmentIdentityChangesWithContentAtSameRevision(t *testing.T) {
	first := segmentSpec("session-public-data", "wire", 2, 10, 20, 11, 1, 2,
		segments.WireSchemaVersion, "wire", "wire", strings.Repeat("a", 64))
	second := segmentSpec("session-public-data", "wire", 2, 10, 20, 11, 1, 2,
		segments.WireSchemaVersion, "wire", "wire", strings.Repeat("b", 64))
	if first.Name == second.Name || !strings.Contains(first.Name, strings.Repeat("a", 12)) ||
		!strings.Contains(second.Name, strings.Repeat("b", 12)) {
		t.Fatalf("first=%s second=%s", first.Name, second.Name)
	}
}

func TestQuarantineUncommittedArtifactsRejectsSessionSymlink(t *testing.T) {
	root := t.TempDir()
	name := "session-public-data-000002-wire-unsafe.parquet"
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	_, err := QuarantineUncommittedArtifacts(root, "session-public-data", DatasetManifest{},
		func([]string, []segments.Manifest) error { return nil })
	if err == nil {
		t.Fatal("session-owned symlink was ignored")
	}
}

func recoverySegmentSpec(name string) segments.Spec {
	started := time.Unix(1_700_000_000, 0).UTC()
	return segments.Spec{Name: name, SchemaVersion: segments.WireSchemaVersion, ParserVersion: "wire",
		NormalizationVersion: "wire", OrderedContentHash: strings.Repeat("c", 64), FirstOrdinal: 2,
		LastOrdinal: 3, RecordCount: 2, StartedAt: started, EndedAt: started.Add(time.Minute)}
}

func recoveryParquetFixture(writer io.Writer) (string, error) {
	_, err := writer.Write([]byte("PAR1recovery-fixture-zstd-metadataPAR1"))
	return strings.Repeat("c", 64), err
}
