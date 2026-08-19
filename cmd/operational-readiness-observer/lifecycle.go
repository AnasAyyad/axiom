package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const observerLifecycleSchema = "axiom.operational_readiness.observer-lifecycle.v1"

type observerLifecycleEvent struct {
	SchemaVersion       string    `json:"schema_version"`
	Sequence            uint64    `json:"sequence"`
	OccurredAt          time.Time `json:"occurred_at"`
	Event               string    `json:"event"`
	Attempt             uint64    `json:"attempt,omitempty"`
	SourceRevision      uint64    `json:"source_revision,omitempty"`
	Source              string    `json:"source,omitempty"`
	Stage               string    `json:"stage,omitempty"`
	Role                string    `json:"role,omitempty"`
	Reason              string    `json:"reason,omitempty"`
	Retryable           bool      `json:"retryable,omitempty"`
	ConsecutiveFailures uint64    `json:"consecutive_failures,omitempty"`
	DurationMillis      uint64    `json:"duration_ms,omitempty"`
	LastSuccessAt       time.Time `json:"last_success_at,omitempty"`
	PriorEventHash      string    `json:"prior_event_hash,omitempty"`
	EventHash           string    `json:"event_hash"`
}

type observerLifecycleWriter struct {
	file     *os.File
	sequence uint64
	headHash string
}

func openObserverLifecycle(path string) (*observerLifecycleWriter, error) {
	if path == "" || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return nil, fmt.Errorf("observer_lifecycle_path_invalid")
	}
	writer := &observerLifecycleWriter{}
	if err := writer.load(path); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("observer_lifecycle_open_failed")
	}
	writer.file = file
	return writer, nil
}

func (writer *observerLifecycleWriter) load(path string) error {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("observer_lifecycle_read_failed")
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		var event observerLifecycleEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.SchemaVersion != observerLifecycleSchema ||
			event.Sequence != writer.sequence+1 || event.PriorEventHash != writer.headHash {
			return fmt.Errorf("observer_lifecycle_chain_invalid")
		}
		digest, digestErr := observerLifecycleDigest(event)
		if digestErr != nil || event.EventHash != digest {
			return fmt.Errorf("observer_lifecycle_chain_invalid")
		}
		writer.sequence, writer.headHash = event.Sequence, event.EventHash
	}
	if scanner.Err() != nil {
		return fmt.Errorf("observer_lifecycle_read_failed")
	}
	return nil
}

func (writer *observerLifecycleWriter) emit(event observerLifecycleEvent) error {
	if writer == nil || writer.file == nil || event.Event == "" || event.OccurredAt.IsZero() {
		return fmt.Errorf("observer_lifecycle_event_invalid")
	}
	event.SchemaVersion = observerLifecycleSchema
	event.Sequence = writer.sequence + 1
	event.OccurredAt = event.OccurredAt.UTC()
	event.PriorEventHash = writer.headHash
	digest, err := observerLifecycleDigest(event)
	if err != nil {
		return fmt.Errorf("observer_lifecycle_encode_failed")
	}
	event.EventHash = digest
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("observer_lifecycle_encode_failed")
	}
	payload = append(payload, '\n')
	if _, err = writer.file.Write(payload); err != nil || writer.file.Sync() != nil {
		return fmt.Errorf("observer_lifecycle_write_failed")
	}
	writer.sequence, writer.headHash = event.Sequence, event.EventHash
	_, _ = os.Stdout.Write(payload)
	return nil
}

func (writer *observerLifecycleWriter) close() error {
	if writer == nil || writer.file == nil {
		return nil
	}
	if err := writer.file.Sync(); err != nil {
		return fmt.Errorf("observer_lifecycle_write_failed")
	}
	return writer.file.Close()
}

func observerLifecycleDigest(event observerLifecycleEvent) (string, error) {
	event.EventHash = ""
	payload, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
