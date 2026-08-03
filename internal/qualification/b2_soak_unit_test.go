package qualification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
)

func TestB2DeclaredMembershipCadenceAndReadiness(t *testing.T) {
	if b2CoherentSampleEvery != 5*time.Second || b2ReadinessTimeout != 2*time.Minute || b2MaximumDegradation != 15*time.Second {
		t.Fatal("B2 declared timing changed")
	}
	trackers := newB2PairTrackers()
	for _, pair := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		if trackers[pair] == nil || !validB2Pair(pair) {
			t.Fatalf("missing pair %s", pair)
		}
	}
	if len(trackers) != 3 {
		t.Fatalf("pairs = %d", len(trackers))
	}
	results := make(chan b2CollectorResult)
	monitor := b2Monitor{results: results}
	started := time.Now()
	_, failure := awaitB2Readiness(context.Background(), monitor, 5*time.Millisecond, time.Hour)
	if failure != "readiness_timeout" || time.Since(started) > time.Second {
		t.Fatalf("failure=%s", failure)
	}
}

type b2RejectionReplayTest struct {
	name   string
	code   b2RejectionCode
	mutate func(*b2CoherentSample)
}

func b2RejectionReplayTests() []b2RejectionReplayTest {
	return []b2RejectionReplayTest{
		{"capture", b2RejectCaptureFailure, func(sample *b2CoherentSample) { sample.CaptureFailed = true }},
		{"missing", b2RejectMissing, func(sample *b2CoherentSample) { sample.Members[1].Reference = nil }},
		{"post trigger", b2RejectPostTrigger, func(sample *b2CoherentSample) {
			sample.Members[1].Reference.ReceiveMonotonicNanos = sample.Trigger.MonotonicNanos + 1
		}},
		{"generation", b2RejectGeneration, func(sample *b2CoherentSample) { sample.Members[1].ActiveGeneration++ }},
		{"gap", b2RejectGap, func(sample *b2CoherentSample) { sample.Members[1].UnresolvedGap = true }},
		{"stale", b2RejectStale, func(sample *b2CoherentSample) {
			sample.Trigger.MonotonicNanos = sample.Members[0].Reference.ReceiveMonotonicNanos + uint64(250*time.Millisecond) + 1
		}},
		{"uncertainty", b2RejectUncertainty, func(sample *b2CoherentSample) {
			sample.Members[0].Reference.ClockUncertainty = 100*time.Millisecond + 1
		}},
		{"interval", b2RejectInterval, func(sample *b2CoherentSample) { sample.Members[1].Reference.ClockOffset = 100 * time.Millisecond }},
		{"identity", b2RejectIdentity, func(sample *b2CoherentSample) { sample.Members[0].Key.Exchange = "bad/value" }},
		{"configuration", b2RejectConfiguration, func(sample *b2CoherentSample) { sample.Policy.Version = "different" }},
		{"duplicate", b2RejectDuplicateMembership, func(sample *b2CoherentSample) {
			sample.Members[1].Key = sample.Members[0].Key
			sample.Members[1].Reference.Key = sample.Members[0].Key
		}},
	}
}

func TestB2CoherentRejectionReplayCodes(t *testing.T) {
	base := b2TestSample(t)
	tests := b2RejectionReplayTests()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sample := cloneB2CoherentSample(base)
			test.mutate(&sample)
			sample.Outcome, sample.CoherentID, sample.RejectionCode = "rejected", "", test.code
			if _, code := evaluateB2CoherentSample(sample); code != test.code {
				t.Fatalf("code=%s want=%s", code, test.code)
			}
			if err := replayB2CoherentSample(sample); err != nil {
				t.Fatal(err)
			}
		})
	}
	counts := b2RejectionCounts{}
	for _, code := range b2RejectionCodes {
		counts.increment(code)
	}
	payload, _ := json.Marshal(counts)
	for _, code := range b2RejectionCodes {
		if !strings.Contains(string(payload), `"`+string(code)+`":1`) {
			t.Fatalf("missing fixed count %s: %s", code, payload)
		}
	}
}

func TestB2RecoveryInclusiveBoundaryAndOfficialFreeze(t *testing.T) {
	started := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name     string
		duration time.Duration
		want     bool
	}{
		{"exactly fifteen seconds", 15 * time.Second, true},
		{"one nanosecond over", 15*time.Second + time.Nanosecond, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracker := &b2PairTracker{}
			rejected := b2TestSample(t)
			rejected.SampledAt, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = started, "rejected", "", b2RejectStale
			if !tracker.Observe(&rejected) {
				t.Fatal("initial rejection failed")
			}
			success := b2TestSample(t)
			success.SampledAt = started.Add(test.duration)
			if got := tracker.Observe(&success); got != test.want {
				t.Fatalf("within=%t want=%t", got, test.want)
			}
			if success.Degradation.RecoveryDuration != test.duration || success.Degradation.RecoveryWithinLimit != test.want {
				t.Fatal("recovery fact mismatch")
			}
		})
	}
	root := t.TempDir()
	journal, err := newNamedQualificationJournal(root, "events.jsonl", b2JournalSchema, "B2_TEST", testSourceCommit, started)
	if err != nil {
		t.Fatal(err)
	}
	evidence := newB2SoakEvidence(root, testSourceCommit, "test-region", started, time.Minute, time.Minute, b2CoherentSampleEvery, false)
	tracker := &b2PairTracker{}
	rejected := b2TestSample(t)
	rejected.SampledAt, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = started, "rejected", "", b2RejectStale
	tracker.Observe(&rejected)
	monitor := b2Monitor{evidence: &evidence, journal: journal, pairs: map[string]*b2PairTracker{"BTCUSDT": tracker}}
	if failure := freezeB2OfficialEnd(started.Add(time.Minute), monitor); failure != "coherent_degradation_unresolved" {
		t.Fatalf("failure=%s", failure)
	}
	frozen := evidence.Pairs["BTCUSDT"]
	tracker.snapshot.DegradedSince = time.Time{}
	if evidence.Pairs["BTCUSDT"] != frozen || frozen.DegradedSince.IsZero() {
		t.Fatal("official state was not frozen")
	}
	_ = journal.Close()
}

func TestB2CoherentSegmentsReplayRolloverAndCorruption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coherent")
	writer, err := newB2CoherentSegmentWriter(root, testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	first := b2TestSample(t)
	if err = writer.Append(first); err != nil {
		t.Fatal(err)
	}
	rejected := cloneB2CoherentSample(first)
	rejected.SampledAt = first.SampledAt.Add(b2CoherentSegmentEvery)
	rejected.CaptureFailed, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = true, "rejected", "", b2RejectCaptureFailure
	if err = writer.Append(rejected); err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(); err != nil {
		t.Fatal(err)
	}
	sequence, hash, samples := writer.Snapshot()
	if sequence != 2 || samples != 2 {
		t.Fatalf("sequence=%d samples=%d", sequence, samples)
	}
	manifest, err := verifyB2CoherentSegments(root, testSourceCommit, sequence, hash)
	if err != nil || len(manifest.Segments) != 2 {
		t.Fatalf("verify=%v", err)
	}
	path := filepath.Join(root, manifest.Segments[0].Filename)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload[len(payload)/2] ^= 1
	if err = os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyB2CoherentSegments(root, testSourceCommit, sequence, hash); err == nil {
		t.Fatal("corruption accepted")
	}
}

func TestB2CoherentSegmentsFailClosedOnAtomicAndPartialWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coherent")
	writer, err := newB2CoherentSegmentWriter(root, testSourceCommit, func(string, any) error { return errors.New("injected") })
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Append(b2TestSample(t)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(); err == nil {
		t.Fatal("atomic failure accepted")
	}
	root = filepath.Join(t.TempDir(), "coherent")
	writer, err = newB2CoherentSegmentWriter(root, testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Append(b2TestSample(t)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(); err != nil {
		t.Fatal(err)
	}
	sequence, hash, _ := writer.Snapshot()
	if err = os.WriteFile(filepath.Join(root, ".b2.partial"), []byte("partial"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyB2CoherentSegments(root, testSourceCommit, sequence, hash); err == nil {
		t.Fatal("partial write accepted")
	}
}

func TestB2ResourceAndBoundedEvidenceFailures(t *testing.T) {
	memory := memorySample{ProcStatusAvailable: true, OpenFDsAvailable: true, HeapAlloc: b2DeclaredHeapLimit}
	storage := storageSample{StatfsAvailable: true, AvailableBytes: b2MinimumFreeBytes, AvailableInodes: 1}
	if failures := b2ResourceFailures(memory, storage); len(failures) != 0 {
		t.Fatal(failures)
	}
	memory.HeapAlloc++
	storage.AvailableBytes--
	failures := b2ResourceFailures(memory, storage)
	if !containsFailure(failures, "heap_limit_exceeded") || !containsFailure(failures, "storage_capacity_failed") {
		t.Fatal(failures)
	}
	failure := boundedQualificationFailure("status_write_failed", "status", "atomic_status_write", errors.New("arbitrary secret text"))
	payload, _ := json.Marshal(failure)
	if strings.Contains(string(payload), "arbitrary secret text") {
		t.Fatal("arbitrary error text persisted")
	}
}

func TestB2StatusFailureAtomicReplacementAndCancellation(t *testing.T) {
	monitor := newB2UnitMonitor(t)
	monitor.status = func(string, b2SoakStatus) error { return errors.New("unbounded injected detail") }
	observed := monitor.evidence.HarnessStartedAt.Add(time.Minute)
	if failure := writeB2StatusStep(observed, monitor); failure != statusWriteFailure {
		t.Fatalf("failure=%s", failure)
	}
	if !containsFailure(monitor.evidence.Failures, statusWriteFailure) {
		t.Fatal("status failure was not retained")
	}
	payload, _ := json.Marshal(monitor.evidence.FailureDetails)
	if strings.Contains(string(payload), "unbounded injected detail") {
		t.Fatal("unbounded status error persisted")
	}
	monitor.status = writeB2SoakStatus
	if failure := writeB2StatusStep(observed.Add(time.Minute), monitor); failure != "" {
		t.Fatal(failure)
	}
	var status b2SoakStatus
	if err := readStrictJSON(filepath.Join(monitor.root, "b2-soak-status.json"), &status); err != nil {
		t.Fatal(err)
	}
	if status.SchemaVersion != b2StatusSchema || status.SourceCommit != testSourceCommit || status.EventJournalSequence == 0 {
		t.Fatalf("status=%#v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failure := monitorB2Soak(ctx, make(chan time.Time), time.Hour, time.Hour, monitor); failure != "" {
		t.Fatalf("cancellation failure=%s", failure)
	}
	_ = monitor.journal.Close()
}

func TestB2ReadinessFailsClosedOnCollectorExit(t *testing.T) {
	monitor := newB2UnitMonitor(t)
	results := make(chan b2CollectorResult, 1)
	results <- b2CollectorResult{exchange: "binance", instrument: "BTCUSDT", err: errors.New("private detail")}
	monitor.results = results
	if _, failure := awaitB2Readiness(context.Background(), monitor, time.Hour, time.Hour); failure != "collector_failed" {
		t.Fatalf("failure=%s", failure)
	}
	if !containsFailure(monitor.evidence.Failures, "collector_failed") || monitor.evidence.CollectorRunning["binance:BTCUSDT"] {
		t.Fatal("collector terminal state was not retained")
	}
	payload, _ := json.Marshal(monitor.evidence.FailureDetails)
	if strings.Contains(string(payload), "private detail") {
		t.Fatal("collector error text persisted")
	}
	_ = monitor.journal.Close()
}

func TestB2ReplayRejectsOutcomeIdentityDrift(t *testing.T) {
	success := b2TestSample(t)
	success.CoherentID = strings.Repeat("0", 64)
	if err := replayB2CoherentSample(success); err == nil {
		t.Fatal("success identity drift accepted")
	}
	rejected := b2TestSample(t)
	rejected.CaptureFailed, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = true, "rejected", "", b2RejectMissing
	if err := replayB2CoherentSample(rejected); err == nil {
		t.Fatal("rejection code drift accepted")
	}
}

func newB2UnitMonitor(t *testing.T) b2Monitor {
	t.Helper()
	root := t.TempDir()
	started := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	evidence := newB2SoakEvidence(root, testSourceCommit, "test-region", started, time.Minute, time.Minute, b2CoherentSampleEvery, false)
	journal, err := newNamedQualificationJournal(root, "events.jsonl", b2JournalSchema, "B2_TEST", testSourceCommit, started)
	if err != nil {
		t.Fatal(err)
	}
	segments, err := newB2CoherentSegmentWriter(filepath.Join(root, "coherent"), testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	ordinals := &runtimecore.IngestOrdinals{}
	profile := recorder.CollectorProfile{Instance: "test-collector", Region: "test-region", MinimumReaderVersion: b2MinimumReader}
	components := b2SoakComponents{
		binanceRecorder:   mustB2Recorder(t, filepath.Join(root, "binance"), "test-binance", "test-binance", "binance", ordinals, profile),
		bybitRecorder:     mustB2Recorder(t, filepath.Join(root, "bybit"), "test-bybit", "test-bybit", "bybit", ordinals, profile),
		binanceCollectors: map[string]*binance.InstrumentCollector{},
		bybitCollectors:   map[string]*bybit.InstrumentCollector{},
	}
	return b2Monitor{root: root, components: components, latest: &b2LatestManifests{}, evidence: &evidence,
		pairs: newB2PairTrackers(), segments: segments, journal: journal, status: writeB2SoakStatus,
		resources: func(observed time.Time, _ string) (memorySample, storageSample) {
			return memorySample{ObservedAt: observed, ProcStatusAvailable: true, OpenFDsAvailable: true},
				storageSample{ObservedAt: observed, StatfsAvailable: true, AvailableBytes: b2MinimumFreeBytes, AvailableInodes: 1}
		}}
}

func b2TestSample(t *testing.T) b2CoherentSample {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	left := runtimecore.ViewReference{Key: runtimecore.MarketKey{Exchange: "binance", Instrument: instrument}, BookVersion: 7,
		ConnectionGeneration: 2, ReceiveMonotonicNanos: uint64(time.Second), ReceiveUTC: observed,
		IngestOrdinal: 10, ClockUncertainty: 10 * time.Millisecond, StateHash: strings.Repeat("a", 64),
		CollectorInstance: b2BinanceInstance, CollectorRegion: "test-region"}
	right := runtimecore.ViewReference{Key: runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument}, BookVersion: 9,
		ConnectionGeneration: 3, ReceiveMonotonicNanos: uint64(time.Second + 10*time.Millisecond), ReceiveUTC: observed.Add(10 * time.Millisecond),
		IngestOrdinal: 11, ClockUncertainty: 10 * time.Millisecond, StateHash: strings.Repeat("b", 64),
		CollectorInstance: b2BybitInstance, CollectorRegion: "test-region"}
	sample := b2CoherentSample{Phase: "official", Pair: "BTCUSDT", SampledAt: observed, Policy: runtimecore.InitialB2CoherentPolicy(),
		Trigger: runtimecore.AsOfTrigger{MonotonicNanos: uint64(time.Second + 100*time.Millisecond), IngestOrdinal: 11, UTC: observed},
		Members: []b2CoherentMemberEvidence{{Key: left.Key, Reference: &left, ActiveGeneration: 2}, {Key: right.Key, Reference: &right, ActiveGeneration: 3}}}
	identity, code := evaluateB2CoherentSample(sample)
	if code != "" {
		t.Fatalf("fixture rejected: %s", code)
	}
	sample.Outcome, sample.CoherentID = "success", identity
	return sample
}
