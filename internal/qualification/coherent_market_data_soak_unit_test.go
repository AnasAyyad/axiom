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

func TestCoherentMarketDataDeclaredMembershipCadenceAndReadiness(t *testing.T) {
	if coherentMarketDataCoherentSampleEvery != 5*time.Second || coherentMarketDataReadinessTimeout != 2*time.Minute || coherentMarketDataMaximumDegradation != 15*time.Second {
		t.Fatal("coherent market data declared timing changed")
	}
	trackers := newCoherentMarketDataPairTrackers()
	for _, pair := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		if trackers[pair] == nil || !validCoherentMarketDataPair(pair) {
			t.Fatalf("missing pair %s", pair)
		}
	}
	if len(trackers) != 3 {
		t.Fatalf("pairs = %d", len(trackers))
	}
	results := make(chan coherentMarketDataCollectorResult)
	monitor := coherentMarketDataMonitor{results: results}
	started := time.Now()
	_, failure := awaitCoherentMarketDataReadiness(context.Background(), monitor, 5*time.Millisecond, time.Hour)
	if failure != "readiness_timeout" || time.Since(started) > time.Second {
		t.Fatalf("failure=%s", failure)
	}
}

type coherentMarketDataRejectionReplayTest struct {
	name   string
	code   coherentMarketDataRejectionCode
	mutate func(*coherentMarketDataCoherentSample)
}

func coherentMarketDataRejectionReplayTests() []coherentMarketDataRejectionReplayTest {
	return []coherentMarketDataRejectionReplayTest{
		{"capture", coherentMarketDataRejectCaptureFailure, func(sample *coherentMarketDataCoherentSample) { sample.CaptureFailed = true }},
		{"missing", coherentMarketDataRejectMissing, func(sample *coherentMarketDataCoherentSample) { sample.Members[1].Reference = nil }},
		{"post trigger", coherentMarketDataRejectPostTrigger, func(sample *coherentMarketDataCoherentSample) {
			sample.Members[1].Reference.ReceiveMonotonicNanos = sample.Trigger.MonotonicNanos + 1
		}},
		{"generation", coherentMarketDataRejectGeneration, func(sample *coherentMarketDataCoherentSample) { sample.Members[1].ActiveGeneration++ }},
		{"gap", coherentMarketDataRejectGap, func(sample *coherentMarketDataCoherentSample) { sample.Members[1].UnresolvedGap = true }},
		{"stale", coherentMarketDataRejectStale, func(sample *coherentMarketDataCoherentSample) {
			sample.Trigger.MonotonicNanos = sample.Members[0].Reference.ReceiveMonotonicNanos + uint64(250*time.Millisecond) + 1
		}},
		{"uncertainty", coherentMarketDataRejectUncertainty, func(sample *coherentMarketDataCoherentSample) {
			sample.Members[0].Reference.ClockUncertainty = 100*time.Millisecond + 1
		}},
		{"interval", coherentMarketDataRejectInterval, func(sample *coherentMarketDataCoherentSample) {
			sample.Members[1].Reference.ClockOffset = 100 * time.Millisecond
		}},
		{"identity", coherentMarketDataRejectIdentity, func(sample *coherentMarketDataCoherentSample) { sample.Members[0].Key.Exchange = "bad/value" }},
		{"configuration", coherentMarketDataRejectConfiguration, func(sample *coherentMarketDataCoherentSample) { sample.Policy.Version = "different" }},
		{"duplicate", coherentMarketDataRejectDuplicateMembership, func(sample *coherentMarketDataCoherentSample) {
			sample.Members[1].Key = sample.Members[0].Key
			sample.Members[1].Reference.Key = sample.Members[0].Key
		}},
	}
}

func TestCoherentMarketDataCoherentRejectionReplayCodes(t *testing.T) {
	base := coherentMarketDataTestSample(t)
	tests := coherentMarketDataRejectionReplayTests()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sample := cloneCoherentMarketDataCoherentSample(base)
			test.mutate(&sample)
			sample.Outcome, sample.CoherentID, sample.RejectionCode = "rejected", "", test.code
			if _, code := evaluateCoherentMarketDataCoherentSample(sample); code != test.code {
				t.Fatalf("code=%s want=%s", code, test.code)
			}
			if err := replayCoherentMarketDataCoherentSample(sample); err != nil {
				t.Fatal(err)
			}
		})
	}
	counts := coherentMarketDataRejectionCounts{}
	for _, code := range coherentMarketDataRejectionCodes {
		counts.increment(code)
	}
	payload, _ := json.Marshal(counts)
	for _, code := range coherentMarketDataRejectionCodes {
		if !strings.Contains(string(payload), `"`+string(code)+`":1`) {
			t.Fatalf("missing fixed count %s: %s", code, payload)
		}
	}
}

func TestCoherentMarketDataRecoveryInclusiveBoundaryAndOfficialFreeze(t *testing.T) {
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
			tracker := &coherentMarketDataPairTracker{}
			rejected := coherentMarketDataTestSample(t)
			rejected.SampledAt, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = started, "rejected", "", coherentMarketDataRejectStale
			if !tracker.Observe(&rejected) {
				t.Fatal("initial rejection failed")
			}
			success := coherentMarketDataTestSample(t)
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
	journal, err := newNamedQualificationJournal(root, "events.jsonl", coherentMarketDataJournalSchema, "COHERENT_MARKET_DATA_TEST", testSourceCommit, started)
	if err != nil {
		t.Fatal(err)
	}
	evidence := newCoherentMarketDataSoakEvidence(root, testSourceCommit, "test-region", started, time.Minute, time.Minute, coherentMarketDataCoherentSampleEvery, false)
	tracker := &coherentMarketDataPairTracker{}
	rejected := coherentMarketDataTestSample(t)
	rejected.SampledAt, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = started, "rejected", "", coherentMarketDataRejectStale
	tracker.Observe(&rejected)
	monitor := coherentMarketDataMonitor{evidence: &evidence, journal: journal, pairs: map[string]*coherentMarketDataPairTracker{"BTCUSDT": tracker}}
	if failure := freezeCoherentMarketDataOfficialEnd(started.Add(time.Minute), monitor); failure != "coherent_degradation_unresolved" {
		t.Fatalf("failure=%s", failure)
	}
	frozen := evidence.Pairs["BTCUSDT"]
	tracker.snapshot.DegradedSince = time.Time{}
	if evidence.Pairs["BTCUSDT"] != frozen || frozen.DegradedSince.IsZero() {
		t.Fatal("official state was not frozen")
	}
	_ = journal.Close()
}

func TestCoherentMarketDataCoherentSegmentsReplayRolloverAndCorruption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coherent")
	writer, err := newCoherentMarketDataCoherentSegmentWriter(root, testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	first := coherentMarketDataTestSample(t)
	if err = writer.Append(first); err != nil {
		t.Fatal(err)
	}
	rejected := cloneCoherentMarketDataCoherentSample(first)
	rejected.SampledAt = first.SampledAt.Add(coherentMarketDataCoherentSegmentEvery)
	rejected.CaptureFailed, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = true, "rejected", "", coherentMarketDataRejectCaptureFailure
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
	manifest, err := verifyCoherentMarketDataCoherentSegments(root, testSourceCommit, sequence, hash)
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
	if _, err = verifyCoherentMarketDataCoherentSegments(root, testSourceCommit, sequence, hash); err == nil {
		t.Fatal("corruption accepted")
	}
}

func TestCoherentMarketDataCoherentSegmentsFailClosedOnAtomicAndPartialWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coherent")
	writer, err := newCoherentMarketDataCoherentSegmentWriter(root, testSourceCommit, func(string, any) error { return errors.New("injected") })
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Append(coherentMarketDataTestSample(t)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(); err == nil {
		t.Fatal("atomic failure accepted")
	}
	root = filepath.Join(t.TempDir(), "coherent")
	writer, err = newCoherentMarketDataCoherentSegmentWriter(root, testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Append(coherentMarketDataTestSample(t)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Flush(); err != nil {
		t.Fatal(err)
	}
	sequence, hash, _ := writer.Snapshot()
	if err = os.WriteFile(filepath.Join(root, ".coherent_market_data.partial"), []byte("partial"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err = verifyCoherentMarketDataCoherentSegments(root, testSourceCommit, sequence, hash); err == nil {
		t.Fatal("partial write accepted")
	}
}

func TestCoherentMarketDataResourceAndBoundedEvidenceFailures(t *testing.T) {
	memory := memorySample{ProcStatusAvailable: true, OpenFDsAvailable: true, HeapAlloc: coherentMarketDataDeclaredHeapLimit}
	storage := storageSample{StatfsAvailable: true, AvailableBytes: coherentMarketDataMinimumFreeBytes, AvailableInodes: 1}
	if failures := coherentMarketDataResourceFailures(memory, storage); len(failures) != 0 {
		t.Fatal(failures)
	}
	memory.HeapAlloc++
	storage.AvailableBytes--
	failures := coherentMarketDataResourceFailures(memory, storage)
	if !containsFailure(failures, "heap_limit_exceeded") || !containsFailure(failures, "storage_capacity_failed") {
		t.Fatal(failures)
	}
	failure := boundedQualificationFailure("status_write_failed", "status", "atomic_status_write", errors.New("arbitrary secret text"))
	payload, _ := json.Marshal(failure)
	if strings.Contains(string(payload), "arbitrary secret text") {
		t.Fatal("arbitrary error text persisted")
	}
}

func TestCoherentMarketDataStatusFailureAtomicReplacementAndCancellation(t *testing.T) {
	monitor := newCoherentMarketDataUnitMonitor(t)
	monitor.status = func(string, coherentMarketDataSoakStatus) error { return errors.New("unbounded injected detail") }
	observed := monitor.evidence.HarnessStartedAt.Add(time.Minute)
	if failure := writeCoherentMarketDataStatusStep(observed, monitor); failure != statusWriteFailure {
		t.Fatalf("failure=%s", failure)
	}
	if !containsFailure(monitor.evidence.Failures, statusWriteFailure) {
		t.Fatal("status failure was not retained")
	}
	payload, _ := json.Marshal(monitor.evidence.FailureDetails)
	if strings.Contains(string(payload), "unbounded injected detail") {
		t.Fatal("unbounded status error persisted")
	}
	monitor.status = writeCoherentMarketDataSoakStatus
	if failure := writeCoherentMarketDataStatusStep(observed.Add(time.Minute), monitor); failure != "" {
		t.Fatal(failure)
	}
	var status coherentMarketDataSoakStatus
	if err := readStrictJSON(filepath.Join(monitor.root, "coherent_market_data-soak-status.json"), &status); err != nil {
		t.Fatal(err)
	}
	if status.SchemaVersion != coherentMarketDataStatusSchema || status.SourceCommit != testSourceCommit || status.EventJournalSequence == 0 {
		t.Fatalf("status=%#v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failure := monitorCoherentMarketDataSoak(ctx, make(chan time.Time), time.Hour, time.Hour, monitor); failure != "" {
		t.Fatalf("cancellation failure=%s", failure)
	}
	_ = monitor.journal.Close()
}

func TestCoherentMarketDataReadinessFailsClosedOnCollectorExit(t *testing.T) {
	monitor := newCoherentMarketDataUnitMonitor(t)
	results := make(chan coherentMarketDataCollectorResult, 1)
	results <- coherentMarketDataCollectorResult{exchange: "binance", instrument: "BTCUSDT", err: errors.New("private detail")}
	monitor.results = results
	if _, failure := awaitCoherentMarketDataReadiness(context.Background(), monitor, time.Hour, time.Hour); failure != "collector_failed" {
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

func TestCoherentMarketDataReplayRejectsOutcomeIdentityDrift(t *testing.T) {
	success := coherentMarketDataTestSample(t)
	success.CoherentID = strings.Repeat("0", 64)
	if err := replayCoherentMarketDataCoherentSample(success); err == nil {
		t.Fatal("success identity drift accepted")
	}
	rejected := coherentMarketDataTestSample(t)
	rejected.CaptureFailed, rejected.Outcome, rejected.CoherentID, rejected.RejectionCode = true, "rejected", "", coherentMarketDataRejectMissing
	if err := replayCoherentMarketDataCoherentSample(rejected); err == nil {
		t.Fatal("rejection code drift accepted")
	}
}

func newCoherentMarketDataUnitMonitor(t *testing.T) coherentMarketDataMonitor {
	t.Helper()
	root := t.TempDir()
	started := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	evidence := newCoherentMarketDataSoakEvidence(root, testSourceCommit, "test-region", started, time.Minute, time.Minute, coherentMarketDataCoherentSampleEvery, false)
	journal, err := newNamedQualificationJournal(root, "events.jsonl", coherentMarketDataJournalSchema, "COHERENT_MARKET_DATA_TEST", testSourceCommit, started)
	if err != nil {
		t.Fatal(err)
	}
	segments, err := newCoherentMarketDataCoherentSegmentWriter(filepath.Join(root, "coherent"), testSourceCommit, writeAtomicJSON)
	if err != nil {
		t.Fatal(err)
	}
	ordinals := &runtimecore.IngestOrdinals{}
	profile := recorder.CollectorProfile{Instance: "test-collector", Region: "test-region", MinimumReaderVersion: coherentMarketDataMinimumReader}
	components := coherentMarketDataSoakComponents{
		binanceRecorder:   mustCoherentMarketDataRecorder(t, filepath.Join(root, "binance"), "test-binance", "test-binance", "binance", ordinals, profile),
		bybitRecorder:     mustCoherentMarketDataRecorder(t, filepath.Join(root, "bybit"), "test-bybit", "test-bybit", "bybit", ordinals, profile),
		binanceCollectors: map[string]*binance.InstrumentCollector{},
		bybitCollectors:   map[string]*bybit.InstrumentCollector{},
	}
	return coherentMarketDataMonitor{root: root, components: components, latest: &coherentMarketDataLatestManifests{}, evidence: &evidence,
		pairs: newCoherentMarketDataPairTrackers(), segments: segments, journal: journal, status: writeCoherentMarketDataSoakStatus,
		resources: func(observed time.Time, _ string) (memorySample, storageSample) {
			return memorySample{ObservedAt: observed, ProcStatusAvailable: true, OpenFDsAvailable: true},
				storageSample{ObservedAt: observed, StatfsAvailable: true, AvailableBytes: coherentMarketDataMinimumFreeBytes, AvailableInodes: 1}
		}}
}

func coherentMarketDataTestSample(t *testing.T) coherentMarketDataCoherentSample {
	t.Helper()
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	left := runtimecore.ViewReference{Key: runtimecore.MarketKey{Exchange: "binance", Instrument: instrument}, BookVersion: 7,
		ConnectionGeneration: 2, ReceiveMonotonicNanos: uint64(time.Second), ReceiveUTC: observed,
		IngestOrdinal: 10, ClockUncertainty: 10 * time.Millisecond, StateHash: strings.Repeat("a", 64),
		CollectorInstance: coherentMarketDataBinanceInstance, CollectorRegion: "test-region"}
	right := runtimecore.ViewReference{Key: runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument}, BookVersion: 9,
		ConnectionGeneration: 3, ReceiveMonotonicNanos: uint64(time.Second + 10*time.Millisecond), ReceiveUTC: observed.Add(10 * time.Millisecond),
		IngestOrdinal: 11, ClockUncertainty: 10 * time.Millisecond, StateHash: strings.Repeat("b", 64),
		CollectorInstance: coherentMarketDataBybitInstance, CollectorRegion: "test-region"}
	sample := coherentMarketDataCoherentSample{Phase: "official", Pair: "BTCUSDT", SampledAt: observed, Policy: runtimecore.InitialCoherentMarketDataCoherentPolicy(),
		Trigger: runtimecore.AsOfTrigger{MonotonicNanos: uint64(time.Second + 100*time.Millisecond), IngestOrdinal: 11, UTC: observed},
		Members: []coherentMarketDataCoherentMemberEvidence{{Key: left.Key, Reference: &left, ActiveGeneration: 2}, {Key: right.Key, Reference: &right, ActiveGeneration: 3}}}
	identity, code := evaluateCoherentMarketDataCoherentSample(sample)
	if code != "" {
		t.Fatalf("fixture rejected: %s", code)
	}
	sample.Outcome, sample.CoherentID = "success", identity
	return sample
}
