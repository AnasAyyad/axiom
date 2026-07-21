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

func testQualificationJournal(t *testing.T, root string, evidence soakEvidence) *qualificationJournal {
	t.Helper()
	journal, err := newQualificationJournal(root, testSourceCommit, evidence.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
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
	status := soakStatus{SchemaVersion: "axiom.a7-soak-status.v2", SourceCommit: testSourceCommit,
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
	root := t.TempDir()
	journal := testQualificationJournal(t, root, evidence)
	var latest recorder.DatasetManifest
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := monitorSoakFailClosed(ctx, root, nil, streamRecorder, nil,
		time.Millisecond, time.Hour, &latest, &evidence,
		func(string, soakStatus) error { return nil }, journal)
	if result != periodicFlushFailure {
		t.Fatalf("monitor failure=%q", result)
	}
}

func TestMonitorSoakFailsClosedOnPeriodicStatusWriteFailure(t *testing.T) {
	streamRecorder := &fakeSoakRecorder{manifest: recorder.DatasetManifest{Revision: 7}}
	evidence := testSoakEvidence()
	root := t.TempDir()
	journal := testQualificationJournal(t, root, evidence)
	var latest recorder.DatasetManifest
	writes := 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := monitorSoakFailClosed(ctx, root, nil, streamRecorder, nil,
		time.Millisecond, time.Hour, &latest, &evidence,
		func(string, soakStatus) error {
			writes++
			if writes > 1 {
				return errors.New("status failed")
			}
			return nil
		}, journal)
	if result != statusWriteFailure || writes != 2 || latest.Revision != 7 {
		t.Fatalf("monitor failure=%q writes=%d revision=%d", result, writes, latest.Revision)
	}
}

func TestMonitorSoakCancellationIsClean(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	evidence := testSoakEvidence()
	root := t.TempDir()
	journal := testQualificationJournal(t, root, evidence)
	var latest recorder.DatasetManifest
	result := monitorSoakFailClosed(ctx, root, nil, &fakeSoakRecorder{}, nil,
		time.Hour, time.Hour, &latest, &evidence,
		func(string, soakStatus) error { return nil }, journal)
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
	if decoded.SchemaVersion != "axiom.a7-soak.v3" {
		t.Fatalf("schema=%q", decoded.SchemaVersion)
	}
}

func TestMonitorSoakFailsClosedWhenEventJournalAppendFails(t *testing.T) {
	evidence := testSoakEvidence()
	root := t.TempDir()
	journal := testQualificationJournal(t, root, evidence)
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	var latest recorder.DatasetManifest
	result := monitorSoakFailClosed(context.Background(), root, nil, &fakeSoakRecorder{}, nil,
		time.Hour, time.Hour, &latest, &evidence,
		func(string, soakStatus) error { return nil }, journal)
	if result != "event_journal_failed" || !containsFailure(evidence.Failures, result) ||
		len(evidence.FailureDetails) != 1 || evidence.FailureDetails[0].Phase != "initial_status" {
		t.Fatalf("result=%q failures=%v details=%#v", result, evidence.Failures, evidence.FailureDetails)
	}
}

func TestMonitorSoakFailsClosedWhenStatusWriterIsMissing(t *testing.T) {
	evidence := testSoakEvidence()
	root := t.TempDir()
	journal := testQualificationJournal(t, root, evidence)
	var latest recorder.DatasetManifest
	result := monitorSoakFailClosed(context.Background(), root, nil, &fakeSoakRecorder{}, nil,
		time.Hour, time.Hour, &latest, &evidence, nil, journal)
	if result != statusWriteFailure || len(evidence.FailureDetails) != 1 ||
		evidence.FailureDetails[0].Cause != "writer_missing" {
		t.Fatalf("result=%q details=%#v", result, evidence.FailureDetails)
	}
}

func TestHostResourceSamplesReportCollectionAvailability(t *testing.T) {
	memory := readMemory(time.Now().UTC())
	if !memory.ProcStatusAvailable || memory.RSS == 0 || !memory.OpenFDsAvailable || memory.OpenFDs == 0 {
		t.Fatalf("memory sample=%#v", memory)
	}
	storage := readStorage(time.Now().UTC(), t.TempDir())
	if !storage.StatfsAvailable || storage.AvailableBytes == 0 || storage.TotalBytes == 0 {
		t.Fatalf("storage sample=%#v", storage)
	}
}

func TestFailureCodesAreUniqueAndSorted(t *testing.T) {
	got := uniqueSortedFailures([]string{"z", "a", "z", "a", "m"})
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("failures=%v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("failures=%v", got)
		}
	}
}

func TestCollectSoakErrorsRetainsInstrumentAndBoundedRecorderCause(t *testing.T) {
	evidence := testSoakEvidence()
	results := make(chan collectorResult, 2)
	results <- collectorResult{instrument: "BTCUSDT", err: &recorder.Error{Code: "wire_finalize_failed",
		Stage: "wire_finalize", Cause: "disk_full", Class: "filesystem", Errno: 28}}
	results <- collectorResult{instrument: "ETHUSDT"}
	collectSoakErrors(results, &evidence)
	if len(evidence.Failures) != 1 || evidence.Failures[0] != "collector_failed" ||
		len(evidence.FailureDetails) != 1 {
		t.Fatalf("failures=%v details=%#v", evidence.Failures, evidence.FailureDetails)
	}
	detail := evidence.FailureDetails[0]
	if detail.Instrument != "BTCUSDT" || detail.Recorder == nil ||
		detail.Recorder.Code != "wire_finalize_failed" || detail.Cause != "wire_finalize_failed" {
		t.Fatalf("detail=%#v", detail)
	}
}
