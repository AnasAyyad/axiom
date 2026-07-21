package qualification

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestQualificationJournalIsDurableHashChainedAndSanitized(t *testing.T) {
	root := t.TempDir()
	started := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	journal, err := newQualificationJournal(root, testSourceCommit, started)
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.Append(qualificationEvent{RecordedAt: started.Add(time.Second), Phase: "preflight", Outcome: "passed"}); err != nil {
		t.Fatal(err)
	}
	if err = journal.Append(qualificationEvent{RecordedAt: started.Add(2 * time.Second), Phase: "recorder_flush",
		Outcome: "passed", ManifestRevision: 1, PendingRaw: 2, PendingCanonical: 2}); err != nil {
		t.Fatal(err)
	}
	sequence, hash := journal.Snapshot()
	if sequence != 2 || len(hash) != sha256.Size*2 {
		t.Fatalf("journal snapshot=%d/%q", sequence, hash)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	assertQualificationJournal(t, filepath.Join(root, "a7-soak-events.jsonl"), hash)
}

func assertQualificationJournal(t *testing.T, path, expectedHash string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var prior string
	count := 0
	for scanner.Scan() {
		var event qualificationEvent
		if err = json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		stored := event.Hash
		event.Hash = ""
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		digest := sha256.Sum256(payload)
		if stored != hex.EncodeToString(digest[:]) || event.PreviousHash != prior ||
			event.SourceCommit != testSourceCommit || event.SchemaVersion != qualificationJournalSchema {
			t.Fatalf("invalid event=%#v", event)
		}
		prior = stored
		count++
	}
	if err = scanner.Err(); err != nil || count != 2 || prior != expectedHash {
		t.Fatalf("events=%d prior=%q err=%v", count, prior, err)
	}
}

func TestQualificationJournalRefusesOverwriteAndFailsClosedAfterClose(t *testing.T) {
	root := t.TempDir()
	journal, err := newQualificationJournal(root, testSourceCommit, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newQualificationJournal(root, testSourceCommit, time.Now().UTC()); err == nil {
		t.Fatal("journal overwrite accepted")
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err = journal.Append(qualificationEvent{Phase: "closed", Outcome: "failed"}); err == nil {
		t.Fatal("append after close accepted")
	}
}

func TestQualificationFailureRetainsSanitizedFilesystemCause(t *testing.T) {
	failure := boundedQualificationFailure("status_write_failed", "periodic_status",
		"atomic_status_write", syscall.ENOSPC)
	if failure.Code != "status_write_failed" || failure.Phase != "periodic_status" ||
		failure.Cause != "disk_full" || failure.Class != "filesystem" ||
		failure.Errno != int(syscall.ENOSPC) || failure.Recorder != nil {
		t.Fatalf("failure=%#v", failure)
	}
}

func TestVerifyQualificationJournalRejectsTampering(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a7-soak-events.jsonl")
	journal, err := newQualificationJournal(root, testSourceCommit, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.Append(qualificationEvent{Phase: "preflight", Outcome: "passed"}); err != nil {
		t.Fatal(err)
	}
	sequence, terminalHash := journal.Snapshot()
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	if err = verifyQualificationJournal(path, testSourceCommit, sequence, terminalHash); err != nil {
		t.Fatalf("valid journal rejected: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event qualificationEvent
	if err = json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	event.Outcome = "failed"
	payload, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(payload, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	if err = verifyQualificationJournal(path, testSourceCommit, sequence, terminalHash); err == nil {
		t.Fatal("tampered journal accepted")
	}
}
