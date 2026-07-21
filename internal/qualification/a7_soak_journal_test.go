package qualification

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"axiom/internal/recorder"
)

const qualificationJournalSchema = "axiom.a7-soak-events.v1"

type qualificationFailure struct {
	ObservedAt time.Time       `json:"observed_at"`
	Instrument string          `json:"instrument,omitempty"`
	Code       string          `json:"code"`
	Phase      string          `json:"phase"`
	Cause      string          `json:"cause,omitempty"`
	Class      string          `json:"class,omitempty"`
	Errno      int             `json:"errno,omitempty"`
	Recorder   *recorder.Error `json:"recorder,omitempty"`
}

type qualificationEvent struct {
	SchemaVersion    string          `json:"schema_version"`
	Sequence         uint64          `json:"sequence"`
	SourceCommit     string          `json:"source_commit"`
	RecordedAt       time.Time       `json:"recorded_at"`
	Elapsed          time.Duration   `json:"elapsed_nanos"`
	Phase            string          `json:"phase"`
	Outcome          string          `json:"outcome"`
	Code             string          `json:"code,omitempty"`
	ManifestRevision uint64          `json:"manifest_revision,omitempty"`
	PendingRaw       uint64          `json:"pending_raw,omitempty"`
	PendingCanonical uint64          `json:"pending_canonical,omitempty"`
	Duration         time.Duration   `json:"duration_nanos,omitempty"`
	Recorder         *recorder.Error `json:"recorder,omitempty"`
	PreviousHash     string          `json:"previous_hash,omitempty"`
	Hash             string          `json:"hash"`
}

type qualificationJournal struct {
	mutex        sync.Mutex
	file         *os.File
	path         string
	sourceCommit string
	started      time.Time
	sequence     uint64
	hash         string
}

func newQualificationJournal(root, sourceCommit string, started time.Time) (*qualificationJournal, error) {
	path := filepath.Join(root, "a7-soak-events.jsonl")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o640)
	if err != nil {
		return nil, err
	}
	return &qualificationJournal{file: file, path: path, sourceCommit: sourceCommit, started: started}, nil
}

func (journal *qualificationJournal) Append(event qualificationEvent) error {
	if journal == nil || journal.file == nil {
		return errors.New("qualification journal unavailable")
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	event.SchemaVersion = qualificationJournalSchema
	event.Sequence = journal.sequence + 1
	event.SourceCommit = journal.sourceCommit
	if event.RecordedAt.IsZero() {
		event.RecordedAt = time.Now().UTC()
	} else {
		event.RecordedAt = event.RecordedAt.UTC()
	}
	event.Elapsed = event.RecordedAt.Sub(journal.started)
	if event.Elapsed < 0 {
		event.Elapsed = 0
	}
	event.PreviousHash = journal.hash
	event.Hash = ""
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(payload)
	event.Hash = hex.EncodeToString(digest[:])
	payload, err = json.Marshal(event)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err = journal.file.Write(payload); err != nil {
		writeEmergencyQualificationEvent(event, "journal_write_failed")
		return err
	}
	if err = journal.file.Sync(); err != nil {
		writeEmergencyQualificationEvent(event, "journal_sync_failed")
		return err
	}
	journal.sequence, journal.hash = event.Sequence, event.Hash
	_, _ = fmt.Fprintf(os.Stderr, "A7_EVENT %s", payload)
	return nil
}

func (journal *qualificationJournal) Snapshot() (uint64, string) {
	if journal == nil {
		return 0, ""
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	return journal.sequence, journal.hash
}

func (journal *qualificationJournal) Close() error {
	if journal == nil || journal.file == nil {
		return nil
	}
	journal.mutex.Lock()
	defer journal.mutex.Unlock()
	err := journal.file.Sync()
	if closeErr := journal.file.Close(); err == nil {
		err = closeErr
	}
	journal.file = nil
	return err
}

func writeEmergencyQualificationEvent(event qualificationEvent, code string) {
	type emergency struct {
		SchemaVersion string             `json:"schema_version"`
		RecordedAt    time.Time          `json:"recorded_at"`
		Code          string             `json:"code"`
		Event         qualificationEvent `json:"event"`
	}
	payload, _ := json.Marshal(emergency{SchemaVersion: "axiom.a7-emergency.v1",
		RecordedAt: time.Now().UTC(), Code: code, Event: event})
	_, _ = fmt.Fprintf(os.Stderr, "A7_EMERGENCY %s\n", payload)
}

func verifyQualificationJournal(path, sourceCommit string, expectedSequence uint64, expectedHash string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	sequence, priorHash := uint64(0), ""
	for scanner.Scan() {
		var event qualificationEvent
		if err = json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		sequence++
		if event.SchemaVersion != qualificationJournalSchema || event.SourceCommit != sourceCommit ||
			event.Sequence != sequence || event.PreviousHash != priorHash {
			return errors.New("qualification journal metadata or chain mismatch")
		}
		storedHash := event.Hash
		event.Hash = ""
		payload, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		digest := sha256.Sum256(payload)
		if storedHash != hex.EncodeToString(digest[:]) {
			return errors.New("qualification journal hash mismatch")
		}
		priorHash = storedHash
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if sequence != expectedSequence || priorHash != expectedHash {
		return errors.New("qualification journal terminal mismatch")
	}
	return nil
}
func boundedQualificationFailure(code, phase, cause string, err error) qualificationFailure {
	failure := qualificationFailure{ObservedAt: time.Now().UTC(), Code: code, Phase: phase, Cause: cause}
	if detail, ok := recorder.FailureDetail(err); ok {
		failure.Recorder = &detail
		failure.Cause = detail.Code
		return failure
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		failure.Class, failure.Errno = "filesystem", int(errno)
		switch {
		case errors.Is(err, syscall.ENOSPC):
			failure.Cause = "disk_full"
		case errors.Is(err, syscall.EDQUOT):
			failure.Cause = "quota_exceeded"
		case errors.Is(err, syscall.EIO):
			failure.Cause = "io_failure"
		case errors.Is(err, syscall.EMFILE), errors.Is(err, syscall.ENFILE):
			failure.Cause = "file_descriptor_exhausted"
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			failure.Cause = "permission_denied"
		case errors.Is(err, syscall.EROFS):
			failure.Cause = "read_only_filesystem"
		default:
			failure.Cause = "filesystem_failure"
		}
	}
	return failure
}

func appendQualificationEvent(
	journal *qualificationJournal,
	evidence *soakEvidence,
	event qualificationEvent,
) bool {
	if err := journal.Append(event); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_failed", event.Phase, "journal_append", err))
		return false
	}
	return true
}
