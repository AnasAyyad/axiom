package recorder

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"

	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

func TestCoherentMarketDataDualRecorderContentionPreservesSharedOrdinalContinuity(t *testing.T) {
	base := t.TempDir()
	ordinals := &runtimecore.IngestOrdinals{}
	sourceCommit := strings.Repeat("a", 40)
	profile := CollectorProfile{Instance: "collector-1", Region: "test-region",
		MinimumReaderVersion: "dataset-reader.v2", SourceCommit: sourceCommit}
	binanceRecorder := newCoherentMarketDataQualificationRecorder(t, filepath.Join(base, "binance"), "binance", ordinals, profile)
	bybitRecorder := newCoherentMarketDataQualificationRecorder(t, filepath.Join(base, "bybit"), "bybit", ordinals, profile)
	instrument := recorderInstrument(t)

	const recordsPerExchange = 64
	recordCoherentMarketDataQualificationConcurrently(t, instrument, recordsPerExchange, binanceRecorder, bybitRecorder)
	manifests := flushCoherentMarketDataQualificationPair(t, binanceRecorder, bybitRecorder)
	if manifests["binance"].RawRecordCount != recordsPerExchange || manifests["bybit"].RawRecordCount != recordsPerExchange {
		t.Fatalf("record counts=%d/%d", manifests["binance"].RawRecordCount, manifests["bybit"].RawRecordCount)
	}
	if manifests["binance"].SourceCommit != sourceCommit || manifests["bybit"].SourceCommit != sourceCommit {
		t.Fatalf("source commits=%q/%q", manifests["binance"].SourceCommit, manifests["bybit"].SourceCommit)
	}
	tierA, err := BuildTierAManifest("coherent_market_data-contention-tier-a", time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		map[string]string{"binance": binanceRecorder.root, "bybit": bybitRecorder.root},
		[]DatasetManifest{manifests["bybit"], manifests["binance"]})
	if err != nil || !tierA.Complete || tierA.HiddenGapCount != 0 || tierA.QualityTier != "A" {
		t.Fatalf("tier A=%#v err=%v", tierA, err)
	}
}

func recordCoherentMarketDataQualificationConcurrently(t *testing.T, instrument domain.Instrument, count int, binanceRecorder, bybitRecorder *Recorder) {
	t.Helper()
	errorsByRecord := make(chan error, count*2)
	var group sync.WaitGroup
	for index := 1; index <= count; index++ {
		sequence := uint64(index)
		for _, current := range []struct {
			exchange string
			recorder *Recorder
		}{{"binance", binanceRecorder}, {"bybit", bybitRecorder}} {
			group.Add(1)
			go func(exchange string, value *Recorder) {
				defer group.Done()
				errorsByRecord <- recordCoherentMarketDataQualificationEvent(value, instrument, exchange, sequence)
			}(current.exchange, current.recorder)
		}
	}
	group.Wait()
	close(errorsByRecord)
	for err := range errorsByRecord {
		if err != nil {
			t.Fatal(err)
		}
	}
}

type coherentMarketDataQualificationFlushResult struct {
	exchange string
	manifest DatasetManifest
	err      error
}

func flushCoherentMarketDataQualificationPair(t *testing.T, binanceRecorder, bybitRecorder *Recorder) map[string]DatasetManifest {
	t.Helper()
	flushes := make(chan coherentMarketDataQualificationFlushResult, 2)
	go func() {
		manifest, err := binanceRecorder.Flush()
		flushes <- coherentMarketDataQualificationFlushResult{"binance", manifest, err}
	}()
	go func() {
		manifest, err := bybitRecorder.Flush()
		flushes <- coherentMarketDataQualificationFlushResult{"bybit", manifest, err}
	}()
	manifests := make(map[string]DatasetManifest, 2)
	for range 2 {
		result := <-flushes
		if result.err != nil {
			t.Fatal(result.err)
		}
		manifests[result.exchange] = result.manifest
	}
	return manifests
}

func TestCoherentMarketDataTierAFinalizationRejectsCompatibilityMismatch(t *testing.T) {
	base := t.TempDir()
	ordinals := &runtimecore.IngestOrdinals{}
	binanceRecorder := newCoherentMarketDataQualificationRecorder(t, filepath.Join(base, "binance"), "binance", ordinals,
		CollectorProfile{Instance: "binance-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"})
	bybitRecorder := newCoherentMarketDataQualificationRecorder(t, filepath.Join(base, "bybit"), "bybit", ordinals,
		CollectorProfile{Instance: "bybit-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v3"})
	instrument := recorderInstrument(t)
	if err := recordCoherentMarketDataQualificationEvent(binanceRecorder, instrument, "binance", 1); err != nil {
		t.Fatal(err)
	}
	if err := recordCoherentMarketDataQualificationEvent(bybitRecorder, instrument, "bybit", 1); err != nil {
		t.Fatal(err)
	}
	binanceManifest, err := binanceRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	bybitManifest, err := bybitRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	_, err = BuildTierAManifest("coherent_market_data-mismatch-tier-a", time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
		map[string]string{"binance": binanceRecorder.root, "bybit": bybitRecorder.root},
		[]DatasetManifest{binanceManifest, bybitManifest})
	if recorderCode(err) != "tier_a_compatibility_mismatch" {
		t.Fatalf("compatibility mismatch=%v", err)
	}
}

func TestCoherentMarketDataRecorderResumesVerifiedManifestAndOrdinals(t *testing.T) {
	root := t.TempDir()
	profile := CollectorProfile{Instance: "collector-resume", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"}
	ordinals := &runtimecore.IngestOrdinals{}
	committer := func(segments.Manifest) error { return nil }
	recorder, err := NewCoherentMarketData(root, "resume-dataset", "resume-session", "binance",
		ordinals, committer, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	instrument := recorderInstrument(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	link, err := recorder.RecordRaw(RawInput{Exchange: "binance", EventType: EventDepth,
		Instrument: instrument, SessionID: "resume-session", ConnectionID: "connection-1",
		ConnectionGeneration: 1, MonotonicOffsetNanos: 1, RecordedLogicalTime: 1,
		SourceSequence: "1", ExchangeTime: &now, ReceivedAt: now, Payload: []byte(`{"depth":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err = recorder.RecordCanonical(CanonicalInput{Link: link, EventID: "event-1", ParserVersion: "parser-v1",
		NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"depth":1}`)}); err != nil {
		t.Fatal(err)
	}
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	last, _ := ManifestLastOrdinal(manifest)
	restoredOrdinals, _ := runtimecore.NewIngestOrdinalsAfter(last)
	resumed, restored, err := ResumeCoherentMarketData(root, "resume-dataset", "resume-session", "binance",
		restoredOrdinals, committer, nil, profile)
	if err != nil || restored.Hash != manifest.Hash {
		t.Fatalf("restored=%#v err=%v", restored, err)
	}
	link, err = resumed.RecordRaw(RawInput{Exchange: "binance", EventType: EventDepth,
		Instrument: instrument, SessionID: "resume-session", ConnectionID: "connection-2",
		ConnectionGeneration: 2, MonotonicOffsetNanos: 2, RecordedLogicalTime: 2,
		SourceSequence: "2", ExchangeTime: &now, ReceivedAt: now.Add(time.Second), Payload: []byte(`{"depth":2}`)})
	if err != nil || link.IngestOrdinal != 2 {
		t.Fatalf("link=%#v err=%v", link, err)
	}
	if err = resumed.RecordCanonical(CanonicalInput{Link: link, EventID: "event-2", ParserVersion: "parser-v1",
		NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"depth":2}`)}); err != nil {
		t.Fatal(err)
	}
	manifest, err = resumed.Flush()
	if err != nil || manifest.Revision != 2 || manifest.RawRecordCount != 2 || VerifyManifestChain(root, manifest) != nil {
		t.Fatalf("manifest=%#v err=%v", manifest, err)
	}
}

func newCoherentMarketDataQualificationRecorder(t *testing.T, root, exchange string, ordinals *runtimecore.IngestOrdinals, profile CollectorProfile) *Recorder {
	t.Helper()
	value, err := NewCoherentMarketData(root, exchange+"-qualification", exchange+"-session", exchange, ordinals,
		func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func recordCoherentMarketDataQualificationEvent(value *Recorder, instrument domain.Instrument, exchange string, sequence uint64) error {
	now := time.Unix(1_800_000_000, 0).UTC()
	link, err := value.RecordRaw(RawInput{Exchange: exchange, EventType: EventDepth, Instrument: instrument,
		SessionID: exchange + "-session", ConnectionID: "connection-1", ConnectionGeneration: 1,
		MonotonicOffsetNanos: 1, RecordedLogicalTime: 1, SourceSequence: eventID(sequence),
		ExchangeTime: &now, ReceivedAt: now, Payload: []byte(`{"kind":"depth"}`)})
	if err != nil {
		return err
	}
	return value.RecordCanonical(CanonicalInput{Link: link, EventID: eventID(link.IngestOrdinal),
		ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1", Canonical: []byte(`{"sequence":1}`)})
}
