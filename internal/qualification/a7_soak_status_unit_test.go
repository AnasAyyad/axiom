package qualification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/recorder"
)

const testSourceCommit = "0123456789abcdef0123456789abcdef01234567"

type fakeSoakRecorder struct {
	manifest  recorder.DatasetManifest
	flushErr  error
	raw       uint64
	canonical uint64
}

func (fake *fakeSoakRecorder) Flush() (recorder.DatasetManifest, error) {
	return fake.manifest, fake.flushErr
}

func (fake *fakeSoakRecorder) PendingCounts() (uint64, uint64) {
	return fake.raw, fake.canonical
}

func testSoakEvidence() soakEvidence {
	return newSoakEvidence(time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		5*time.Minute, true, testSourceCommit)
}

func TestQualificationSourceCommitIsExactAndSanitized(t *testing.T) {
	t.Setenv("AXIOM_A7_SOURCE_COMMIT", testSourceCommit)
	commit, err := qualificationSourceCommit()
	if err != nil || commit != testSourceCommit {
		t.Fatalf("commit=%q err=%v", commit, err)
	}
	t.Setenv("AXIOM_A7_SOURCE_COMMIT", "not-a-commit")
	if commit, err = qualificationSourceCommit(); err == nil || commit != "" {
		t.Fatalf("invalid commit=%q err=%v", commit, err)
	}
}

func TestWriteSoakStatusAtomicallyReplacesOneFile(t *testing.T) {
	root := t.TempDir()
	status := soakStatus{SchemaVersion: "axiom.a7-soak-status.v1", SourceCommit: testSourceCommit,
		ManifestRevision: 1}
	if err := writeSoakStatus(root, status); err != nil {
		t.Fatal(err)
	}
	status.ManifestRevision = 2
	if err := writeSoakStatus(root, status); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "a7-soak-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded soakStatus
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ManifestRevision != 2 {
		t.Fatalf("revision=%d", decoded.ManifestRevision)
	}
	temporary, err := filepath.Glob(filepath.Join(root, ".a7-soak-status.json.*.tmp"))
	if err != nil || len(temporary) != 0 {
		t.Fatalf("temporary files=%v err=%v", temporary, err)
	}
}

func TestMonitorSoakFailsClosedOnPeriodicFlushFailure(t *testing.T) {
	failure := errors.New("flush failed")
	streamRecorder := &fakeSoakRecorder{flushErr: failure}
	evidence := testSoakEvidence()
	var latest recorder.DatasetManifest
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := monitorSoakFailClosed(ctx, t.TempDir(), nil, streamRecorder, nil,
		time.Millisecond, time.Hour, &latest, &evidence,
		func(string, soakStatus) error { return nil })
	if result != periodicFlushFailure {
		t.Fatalf("monitor failure=%q", result)
	}
}

func TestMonitorSoakFailsClosedOnPeriodicStatusWriteFailure(t *testing.T) {
	streamRecorder := &fakeSoakRecorder{manifest: recorder.DatasetManifest{Revision: 7}}
	evidence := testSoakEvidence()
	var latest recorder.DatasetManifest
	writes := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := monitorSoakFailClosed(ctx, t.TempDir(), nil, streamRecorder, nil,
		time.Millisecond, time.Hour, &latest, &evidence,
		func(string, soakStatus) error {
			writes++
			if writes > 1 {
				return errors.New("status failed")
			}
			return nil
		})
	if result != statusWriteFailure || writes != 2 || latest.Revision != 7 {
		t.Fatalf("monitor failure=%q writes=%d revision=%d", result, writes, latest.Revision)
	}
}

func TestMonitorSoakCancellationIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	evidence := testSoakEvidence()
	var latest recorder.DatasetManifest
	result := monitorSoakFailClosed(ctx, t.TempDir(), nil, &fakeSoakRecorder{}, nil,
		time.Hour, time.Hour, &latest, &evidence,
		func(string, soakStatus) error { return nil })
	if result != "" {
		t.Fatalf("cancellation failure=%q", result)
	}
}

func TestFinalFlushFailsClosedOnRecorderFailureAndIncompleteRows(t *testing.T) {
	t.Run("flush failure", func(t *testing.T) {
		failure := errors.New("flush failed")
		_, err := finalFlush(&fakeSoakRecorder{raw: 1, canonical: 1, flushErr: failure},
			recorder.DatasetManifest{})
		if !errors.Is(err, failure) {
			t.Fatalf("flush err=%v", err)
		}
	})
	t.Run("incomplete rows", func(t *testing.T) {
		_, err := finalFlush(&fakeSoakRecorder{raw: 1}, recorder.DatasetManifest{})
		if err == nil {
			t.Fatal("incomplete pending rows accepted")
		}
	})
}

func TestWriteSoakEvidenceUsesAtomicTerminalReport(t *testing.T) {
	root := t.TempDir()
	evidence := testSoakEvidence()
	evidence.SchemaVersion = "axiom.a7-soak.v2"
	if err := writeSoakEvidence(root, evidence); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "a7-soak-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded soakEvidence
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != evidence.SchemaVersion || decoded.SourceCommit != testSourceCommit {
		t.Fatalf("decoded evidence=%#v", decoded)
	}
}
