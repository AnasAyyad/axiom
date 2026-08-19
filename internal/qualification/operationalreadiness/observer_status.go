package operationalReadiness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const ObserverStatusSchema = "axiom.operational_readiness.observer-status.v1"

// ObserverStatus is an atomic, bounded diagnostic companion to sample.json.
// It lets the runner preserve the exact redacted source category when a new
// sample cannot be acquired.
type ObserverStatus struct {
	SchemaVersion       string         `json:"schema_version"`
	UpdatedAt           time.Time      `json:"updated_at"`
	LastAttemptAt       time.Time      `json:"last_attempt_at"`
	LastSuccessAt       time.Time      `json:"last_success_at,omitempty"`
	PublishedRevision   uint64         `json:"published_revision"`
	Attempt             uint64         `json:"attempt"`
	ConsecutiveFailures uint64         `json:"consecutive_failures"`
	FailureCount        uint64         `json:"failure_count"`
	RecoveryCount       uint64         `json:"recovery_count"`
	LastOutageMillis    uint64         `json:"last_outage_ms,omitempty"`
	LastFailure         *SourceFailure `json:"last_failure,omitempty"`
	LifecycleHeadHash   string         `json:"lifecycle_head_hash,omitempty"`
}

func WriteObserverStatus(path string, status ObserverStatus) error {
	if path == "" || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) ||
		status.SchemaVersion != ObserverStatusSchema || status.UpdatedAt.IsZero() || status.LastAttemptAt.IsZero() {
		return fmt.Errorf("operational_readiness_observer_status_invalid")
	}
	payload, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("operational_readiness_observer_status_encode_failed")
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".operational-readiness-observer-status-*")
	if err != nil {
		return fmt.Errorf("operational_readiness_observer_status_write_failed")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0o440); err == nil {
		_, err = file.Write(payload)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil || os.Rename(temporary, path) != nil || syncDirectory(directory) != nil {
		return fmt.Errorf("operational_readiness_observer_status_write_failed")
	}
	return nil
}

func readObserverStatus(path string) (ObserverStatus, error) {
	var status ObserverStatus
	if path == "" || readStrictJSON(path, &status) != nil || status.SchemaVersion != ObserverStatusSchema ||
		status.UpdatedAt.IsZero() || status.LastAttemptAt.IsZero() {
		return ObserverStatus{}, fmt.Errorf("operational_readiness_observer_status_unavailable")
	}
	status.UpdatedAt = status.UpdatedAt.UTC()
	status.LastAttemptAt = status.LastAttemptAt.UTC()
	status.LastSuccessAt = status.LastSuccessAt.UTC()
	return status, nil
}
