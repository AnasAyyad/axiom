package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	operationalReadiness "axiom/internal/qualification/operationalreadiness"
)

func TestObserverLifecycleIsAppendOnlyHashChainedAndResumable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observer-lifecycle.jsonl")
	writer, err := openObserverLifecycle(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = writer.emit(observerLifecycleEvent{OccurredAt: now, Event: "observer_started"}); err != nil {
		t.Fatal(err)
	}
	if err = writer.emit(observerLifecycleEvent{OccurredAt: now.Add(time.Second), Event: "observation_failed",
		Source: "runtime", Stage: "health", Role: "recorder", Reason: "not_ready", Retryable: true}); err != nil {
		t.Fatal(err)
	}
	head := writer.headHash
	if err = writer.close(); err != nil {
		t.Fatal(err)
	}
	writer, err = openObserverLifecycle(path)
	if err != nil || writer.sequence != 2 || writer.headHash != head {
		t.Fatalf("resume writer=%+v error=%v", writer, err)
	}
	if err = writer.emit(observerLifecycleEvent{OccurredAt: now.Add(2 * time.Second), Event: "source_recovered"}); err != nil {
		t.Fatal(err)
	}
	if err = writer.close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	var prior string
	var sequence uint64
	for scanner.Scan() {
		var event observerLifecycleEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Sequence != sequence+1 ||
			event.PriorEventHash != prior || event.EventHash == "" {
			t.Fatalf("invalid lifecycle event: %s", scanner.Text())
		}
		prior, sequence = event.EventHash, event.Sequence
	}
	if scanner.Err() != nil || sequence != 3 {
		t.Fatalf("sequence=%d error=%v", sequence, scanner.Err())
	}
}

func TestHealthcheckReadsFreshSampleWithoutDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	t.Setenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE", path)
	t.Setenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_STATUS_FILE", filepath.Join(t.TempDir(), "status.json"))
	t.Setenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_LIFECYCLE_FILE", filepath.Join(t.TempDir(), "lifecycle.jsonl"))
	t.Setenv("AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE", filepath.Join(t.TempDir(), "drill.json"))
	if err := operationalReadiness.WriteLiveSample(path, operationalReadiness.Sample{
		SourceRevision: 1,
		ObservedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--healthcheck"}); err != nil {
		t.Fatal(err)
	}
}

func TestHealthcheckRejectsStaleSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	t.Setenv("AXIOM_OPERATIONAL_READINESS_SAMPLE_FILE", path)
	t.Setenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_STATUS_FILE", filepath.Join(t.TempDir(), "status.json"))
	t.Setenv("AXIOM_OPERATIONAL_READINESS_OBSERVER_LIFECYCLE_FILE", filepath.Join(t.TempDir(), "lifecycle.jsonl"))
	t.Setenv("AXIOM_OPERATIONAL_READINESS_DRILL_OBSERVATION_FILE", filepath.Join(t.TempDir(), "drill.json"))
	if err := operationalReadiness.WriteLiveSample(path, operationalReadiness.Sample{
		SourceRevision: 1,
		ObservedAt:     time.Now().UTC().Add(-3 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), []string{"--healthcheck"}); err == nil {
		t.Fatal("stale observer sample passed the healthcheck")
	}
}
