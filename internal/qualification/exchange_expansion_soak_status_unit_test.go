package qualification

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"axiom/internal/exchanges/bybit"
	"axiom/internal/recorder"
)

func TestExchangeExpansionSoakStatusWriteIsAtomicAndComplete(t *testing.T) {
	root := t.TempDir()
	observed := time.Unix(1_800_000_000, 0).UTC()
	status := exchangeExpansionSoakStatus{
		SchemaVersion:        "axiom.exchange_expansion-soak-status.v3",
		SourceCommit:         testSourceCommit,
		StartedAt:            observed.Add(-time.Hour),
		ObservedAt:           observed,
		Elapsed:              time.Hour,
		RequiredDuration:     exchangeExpansionFormalSoakDuration,
		ProvisionalQualified: true,
		ProvisionalSLOs:      map[string]exchangeExpansionProvisionalSLO{},
		Collectors:           map[string]bybit.CollectorStats{},
		Books:                map[string]bookSample{},
	}
	if err := writeExchangeExpansionSoakStatus(root, status); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(root, "exchange_expansion-soak-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded exchangeExpansionSoakStatus
	if err = json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SourceCommit != testSourceCommit || !decoded.ProvisionalQualified ||
		decoded.Elapsed != time.Hour {
		t.Fatalf("unexpected status: %#v", decoded)
	}
	temporary, err := filepath.Glob(filepath.Join(root, ".exchange_expansion-soak-status.json.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(temporary) != 0 {
		t.Fatalf("temporary files remain: %v", temporary)
	}
}

func TestExchangeExpansionQualificationSourceCommitFailsClosed(t *testing.T) {
	t.Setenv("AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT", "")
	if _, err := exchangeExpansionQualificationSourceCommit(); err == nil {
		t.Fatal("missing source commit accepted")
	}
	t.Setenv("AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT", "not-a-commit")
	if _, err := exchangeExpansionQualificationSourceCommit(); err == nil {
		t.Fatal("invalid source commit accepted")
	}
	t.Setenv("AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT", testSourceCommit)
	if commit, err := exchangeExpansionQualificationSourceCommit(); err != nil || commit != testSourceCommit {
		t.Fatalf("commit=%q err=%v", commit, err)
	}
}

func testExchangeExpansionQualificationJournal(t *testing.T, root string,
	evidence exchangeExpansionSoakEvidence) *qualificationJournal {
	t.Helper()
	journal, err := newNamedQualificationJournal(root, "exchange_expansion-soak-events.jsonl",
		exchangeExpansionQualificationJournalSchema, "EXCHANGE_EXPANSION_EVENT", testSourceCommit, evidence.StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	return journal
}

func TestMonitorExchangeExpansionSoakCapacitySignalFlushesBeforeScheduledInterval(t *testing.T) {
	signal := make(chan struct{}, 1)
	signal <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamRecorder := &fakeSoakRecorder{manifest: recorder.DatasetManifest{Revision: 11}, signal: signal,
		usage: recorder.PendingUsage{UsedBytes: 128, LimitBytes: 512, FlushThresholdBytes: 128,
			HighWaterBytes: 128}, onFlush: cancel}
	root := t.TempDir()
	evidence := newExchangeExpansionSoakEvidence(time.Now().UTC(), 5*time.Minute, true, testSourceCommit, root)
	journal := testExchangeExpansionQualificationJournal(t, root, evidence)
	components := exchangeExpansionSoakComponents{recorder: streamRecorder,
		collectors: map[string]*bybit.InstrumentCollector{}}
	var latest recorder.DatasetManifest
	result := monitorExchangeExpansionSoak(ctx, root, components, nil, time.Hour, time.Hour,
		&latest, &evidence, journal)
	if result != "" || streamRecorder.flushes != 1 || latest.Revision != 11 {
		t.Fatalf("result=%q flushes=%d latest=%#v", result, streamRecorder.flushes, latest)
	}
	payload, err := os.ReadFile(filepath.Join(root, "exchange_expansion-soak-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status exchangeExpansionSoakStatus
	if err = json.Unmarshal(payload, &status); err != nil || status.Recorder.FlushThresholdBytes != 128 ||
		status.ManifestRevision != 11 {
		t.Fatalf("capacity status=%#v error=%v", status, err)
	}
}

func TestMonitorExchangeExpansionSoakFailsImmediatelyWithExactCollectorRecorderCause(t *testing.T) {
	streamRecorder := &fakeSoakRecorder{}
	root := t.TempDir()
	evidence := newExchangeExpansionSoakEvidence(time.Now().UTC(), 5*time.Minute, true, testSourceCommit, root)
	journal := testExchangeExpansionQualificationJournal(t, root, evidence)
	components := exchangeExpansionSoakComponents{recorder: streamRecorder,
		collectors: map[string]*bybit.InstrumentCollector{}}
	results := make(chan exchangeExpansionCollectorResult, 1)
	results <- exchangeExpansionCollectorResult{instrument: "ETHUSDT", err: &recorder.Error{
		Code: "recorder_capacity_exceeded"}}
	var latest recorder.DatasetManifest
	result := monitorExchangeExpansionSoak(context.Background(), root, components, results, time.Hour, time.Hour,
		&latest, &evidence, journal)
	if result != "collector_failed" || evidence.CollectorRunning["ETHUSDT"] ||
		len(evidence.FailureDetails) != 1 || evidence.FailureDetails[0].Recorder == nil ||
		evidence.FailureDetails[0].Recorder.Code != "recorder_capacity_exceeded" {
		t.Fatalf("result=%q running=%v details=%#v", result, evidence.CollectorRunning,
			evidence.FailureDetails)
	}
	payload, err := os.ReadFile(filepath.Join(root, "exchange_expansion-soak-status.json"))
	if err != nil {
		t.Fatal(err)
	}
	var status exchangeExpansionSoakStatus
	if err = json.Unmarshal(payload, &status); err != nil || status.ProvisionalQualified ||
		status.CollectorRunning["ETHUSDT"] {
		t.Fatalf("terminal status=%#v error=%v", status, err)
	}
}
