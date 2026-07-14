package segments

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFinalizationCommitsOnlyAfterRenameAndDirectorySync(t *testing.T) {
	root := t.TempDir()
	var stages []Stage
	finalizer, err := NewFinalizer(root, func(stage Stage) error {
		stages = append(stages, stage)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	manifest, err := finalizer.Finalize(segmentSpec(), parquetFixture, func(value Manifest) error {
		committed = true
		if _, statErr := os.Stat(filepath.Join(root, value.Path)); statErr != nil {
			t.Fatal("manifest committed before final file existed")
		}
		return nil
	})
	if err != nil || !committed || manifest.Format != "parquet" || manifest.Compression != "zstd" {
		t.Fatalf("finalize = %#v, committed=%t, %v", manifest, committed, err)
	}
	want := []Stage{
		StageCreated, StageWritten, StageSynced, StageProofSynced,
		StageRenamed, StageDirectorySynced, StageManifestReady,
	}
	if len(stages) != len(want) {
		t.Fatalf("stages = %v", stages)
	}
	for index := range want {
		if stages[index] != want[index] {
			t.Fatalf("stages = %v", stages)
		}
	}
	if _, err = os.Stat(filepath.Join(root, segmentSpec().Name+".proof")); !os.IsNotExist(err) {
		t.Fatal("completed proof was not removed")
	}
}

func TestKillPointsNeverSilentlyAdvertiseIncompleteSegment(t *testing.T) {
	for _, kill := range []Stage{
		StageCreated, StageWritten, StageSynced, StageProofSynced,
		StageRenamed, StageDirectorySynced, StageManifestReady,
	} {
		t.Run(string(kill), func(t *testing.T) {
			root := t.TempDir()
			finalizer, _ := NewFinalizer(root, func(stage Stage) error {
				if stage == kill {
					return errors.New("injected termination")
				}
				return nil
			})
			commits := 0
			_, err := finalizer.Finalize(segmentSpec(), parquetFixture, func(Manifest) error {
				commits++
				return nil
			})
			if err == nil {
				t.Fatal("kill point did not terminate finalization")
			}
			if kill != StageManifestReady && commits != 0 {
				t.Fatalf("manifest committed before boundary: %d", commits)
			}
			if kill == StageManifestReady && commits != 1 {
				t.Fatalf("durable commit lost at final boundary: %d", commits)
			}
		})
	}
}

func TestRecoveryFinalizesOnlyHashProvedPartial(t *testing.T) {
	root := t.TempDir()
	finalizer, _ := NewFinalizer(root, func(stage Stage) error {
		if stage == StageProofSynced {
			return errors.New("crash")
		}
		return nil
	})
	_, _ = finalizer.Finalize(segmentSpec(), parquetFixture, func(Manifest) error { return nil })
	recovery, _ := NewFinalizer(root, nil)
	if moved, quarantineErr := recovery.QuarantineInvalidProofs(); quarantineErr != nil || len(moved) != 0 {
		t.Fatalf("valid proof was quarantined: %v, %v", moved, quarantineErr)
	}
	if moved, quarantineErr := recovery.QuarantineUnprovedPartials(); quarantineErr != nil || len(moved) != 0 {
		t.Fatalf("proved partial was quarantined: %v, %v", moved, quarantineErr)
	}
	commits := 0
	recovered, err := recovery.Recover(func(Manifest) error { commits++; return nil })
	if err != nil || commits != 1 || len(recovered) != 1 {
		t.Fatalf("recovery = %#v, commits=%d, %v", recovered, commits, err)
	}
	if _, err = os.Stat(filepath.Join(root, segmentSpec().Name+".parquet")); err != nil {
		t.Fatal("proved partial was not finalized")
	}
}

func TestRecoveryRejectsCorruptProofAndQuarantinesUnprovedPartial(t *testing.T) {
	root := t.TempDir()
	finalizer, _ := NewFinalizer(root, func(stage Stage) error {
		if stage == StageProofSynced {
			return errors.New("crash")
		}
		return nil
	})
	_, _ = finalizer.Finalize(segmentSpec(), parquetFixture, func(Manifest) error { return nil })
	partial := filepath.Join(root, segmentSpec().Name+".parquet.partial")
	file, _ := os.OpenFile(partial, os.O_WRONLY|os.O_APPEND, 0)
	_, _ = file.Write([]byte("corrupt"))
	_ = file.Close()
	recovery, _ := NewFinalizer(root, nil)
	if _, err := recovery.Recover(func(Manifest) error { return nil }); err == nil {
		t.Fatal("corrupt proved file recovered")
	}
	moved, err := recovery.QuarantineInvalidProofs()
	if err != nil || len(moved) != 2 {
		t.Fatalf("quarantine = %v, %v", moved, err)
	}
}

func TestFinalizerRejectsTraversalAndNonParquetPayload(t *testing.T) {
	if _, err := NewFinalizer("relative", nil); err == nil {
		t.Fatal("relative root accepted")
	}
	finalizer, _ := NewFinalizer(t.TempDir(), nil)
	spec := segmentSpec()
	spec.Name = "../escape"
	if _, err := finalizer.Finalize(spec, parquetFixture, func(Manifest) error { return nil }); err == nil {
		t.Fatal("traversal name accepted")
	}
	spec = segmentSpec()
	if _, err := finalizer.Finalize(spec, func(writer io.Writer) (string, error) {
		_, writeErr := writer.Write([]byte("not parquet"))
		return spec.OrderedContentHash, writeErr
	}, func(Manifest) error { return nil }); err == nil {
		t.Fatal("non-Parquet payload accepted")
	}
}

func TestFinalizerRejectsWriterContentHashMismatchBeforeProof(t *testing.T) {
	root := t.TempDir()
	finalizer, _ := NewFinalizer(root, nil)
	committed := false
	_, err := finalizer.Finalize(segmentSpec(), func(writer io.Writer) (string, error) {
		_, writeErr := writer.Write([]byte("PAR1fixture-with-zstd-column-metadataPAR1"))
		return strings.Repeat("b", 64), writeErr
	}, func(Manifest) error { committed = true; return nil })
	if err == nil || committed {
		t.Fatalf("content mismatch accepted: committed=%t, err=%v", committed, err)
	}
	if proofs, _ := filepath.Glob(filepath.Join(root, "*.proof")); len(proofs) != 0 {
		t.Fatal("content mismatch produced a recovery proof")
	}
}

func segmentSpec() Spec {
	return Spec{
		Name: "binance-btcusdt-20260714t09", SchemaVersion: "raw.v1", ParserVersion: "parser.v1",
		NormalizationVersion: "normalized.v1", OrderedContentHash: strings.Repeat("a", 64),
		FirstOrdinal: 1, LastOrdinal: 2, RecordCount: 2,
		StartedAt: time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC),
		EndedAt:   time.Date(2026, 7, 14, 9, 1, 0, 0, time.UTC),
	}
}

func parquetFixture(writer io.Writer) (string, error) {
	_, err := writer.Write([]byte("PAR1fixture-with-zstd-column-metadataPAR1"))
	return strings.Repeat("a", 64), err
}
