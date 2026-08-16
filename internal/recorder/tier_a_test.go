package recorder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

func TestLegacyV1ManifestEncodingAndValidationRemainUnchanged(t *testing.T) {
	recorder, err := New(t.TempDir(), "legacy-dataset", "legacy-session", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	recordForExchange(t, recorder, "binance", "legacy-session", 1, 1)
	manifest, err := recorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(manifest)
	if err != nil || manifest.SchemaVersion != datasetSchemaVersion ||
		strings.Contains(string(encoded), "quality_tier") || strings.Contains(string(encoded), "exchange_coverage") ||
		strings.Contains(string(encoded), "compatibility_requirements") {
		t.Fatalf("legacy manifest drifted: %s, %v", encoded, err)
	}
	path := filepath.Join(recorder.root, "legacy-session-000001.dataset.json")
	stored, err := ReadManifest(path)
	if err != nil || stored.Hash != manifest.Hash || manifest.Hash != manifestHash(manifest) {
		t.Fatalf("legacy manifest rejected: %#v, %v", stored, err)
	}
}

func TestCoherentMarketDataTierAManifestProvesPerExchangeCoverageAndCombinedLinkage(t *testing.T) {
	base := t.TempDir()
	ordinals := &runtimecore.IngestOrdinals{}
	profile := CollectorProfile{Instance: "collector-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"}
	binanceRoot, bybitRoot := filepath.Join(base, "binance"), filepath.Join(base, "bybit")
	binanceRecorder, err := NewCoherentMarketData(binanceRoot, "binance-coherent_market_data", "binance-session", "binance", ordinals,
		func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	bybitRecorder, err := NewCoherentMarketData(bybitRoot, "bybit-coherent_market_data", "bybit-session", "bybit", ordinals,
		func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	recordForExchange(t, binanceRecorder, "binance", "binance-session", 1, 1)
	recordForExchange(t, bybitRecorder, "bybit", "bybit-session", 1, 1)
	recordForExchange(t, binanceRecorder, "binance", "binance-session", 2, 2)
	binanceManifest, err := binanceRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	bybitManifest, err := bybitRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	if binanceManifest.SchemaVersion != datasetSchemaVersionV2 || binanceManifest.QualityTier != "candidate" ||
		len(binanceManifest.ExchangeCoverage) != 1 ||
		len(binanceManifest.ExchangeCoverage[0].GenerationHistory) != 2 ||
		!binanceManifest.ExchangeCoverage[0].RawCanonicalLinkageComplete {
		t.Fatalf("Binance coherent market data coverage = %#v", binanceManifest.ExchangeCoverage)
	}
	createdAt := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	tierA, err := BuildTierAManifest("combined-tier-a", createdAt,
		map[string]string{"binance": binanceRoot, "bybit": bybitRoot},
		[]DatasetManifest{bybitManifest, binanceManifest})
	if err != nil || tierA.QualityTier != "A" || tierA.HiddenGapCount != 0 || !tierA.Complete ||
		len(tierA.Members) != 2 || tierA.Members[0].Exchange != "binance" || len(tierA.Hash) != 64 {
		t.Fatalf("Tier A manifest = %#v, %v", tierA, err)
	}
	assertStoredTierAManifest(t, base, tierA)
}

func TestCoherentMarketDataManifestAcceptsInterleavedConnectionGenerations(t *testing.T) {
	root := t.TempDir()
	profile := CollectorProfile{Instance: "collector-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"}
	stream, err := NewCoherentMarketData(root, "binance-parallel-streams", "binance-parallel-session", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	recordForExchange(t, stream, "binance", "binance-parallel-session", 1, 1)
	recordForExchange(t, stream, "binance", "binance-parallel-session", 2, 2)
	recordForExchange(t, stream, "binance", "binance-parallel-session", 1, 3)
	manifest, err := stream.Flush()
	if err != nil {
		t.Fatal(err)
	}
	coverage := manifest.ExchangeCoverage[0]
	if len(coverage.GenerationHistory) != 2 || coverage.GenerationHistory[0].RecordCount != 2 ||
		coverage.GenerationHistory[0].FirstOrdinal != 1 || coverage.GenerationHistory[0].LastOrdinal != 3 ||
		coverage.GenerationHistory[1].FirstOrdinal != 2 || coverage.GenerationHistory[1].LastOrdinal != 2 ||
		coverage.FirstOrdinal != 1 || coverage.LastOrdinal != 3 {
		t.Fatalf("parallel connection coverage = %#v", coverage)
	}
	if _, err = ValidateDataset(root, manifest); err != nil {
		t.Fatalf("parallel connection manifest rejected: %v", err)
	}
}

func TestCoherentMarketDataManifestUsesFullSegmentReceivedTimeBounds(t *testing.T) {
	root := t.TempDir()
	profile := CollectorProfile{Instance: "collector-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"}
	stream, err := NewCoherentMarketData(root, "binance-time-bounds", "binance-time-bounds-session", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Unix(1_700_000_000, 0).UTC()
	peak := base.Add(4 * time.Nanosecond)
	recordForExchangeAt(t, stream, "binance", "binance-time-bounds-session", 1, 1, base)
	recordForExchangeAt(t, stream, "binance", "binance-time-bounds-session", 2, 2, peak)
	recordForExchangeAt(t, stream, "binance", "binance-time-bounds-session", 1, 3, peak.Add(-time.Nanosecond))

	manifest, err := stream.Flush()
	if err != nil {
		t.Fatalf("out-of-order receive timestamps rejected: %v", err)
	}
	coverage := manifest.ExchangeCoverage[0]
	if !coverage.CoverageStart.Equal(base) || !coverage.CoverageEnd.Equal(peak) {
		t.Fatalf("received-time coverage = %s..%s", coverage.CoverageStart, coverage.CoverageEnd)
	}
	for _, reference := range manifest.Segments {
		if !reference.Manifest.Spec.StartedAt.Equal(base) || !reference.Manifest.Spec.EndedAt.Equal(peak) {
			t.Fatalf("%s segment bounds = %s..%s", reference.Kind,
				reference.Manifest.Spec.StartedAt, reference.Manifest.Spec.EndedAt)
		}
	}
	if _, err = ValidateDataset(root, manifest); err != nil {
		t.Fatalf("received-time manifest rejected: %v", err)
	}
}

func assertStoredTierAManifest(t *testing.T, base string, tierA TierAManifest) {
	t.Helper()
	path, err := WriteTierAManifest(filepath.Join(base, "qualification"), tierA)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := ReadTierAManifest(path)
	if err != nil || stored.Hash != tierA.Hash {
		t.Fatalf("stored Tier A = %#v, %v", stored, err)
	}

	encoded, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(encoded), `"hidden_gap_count":0`) ||
		!strings.Contains(string(encoded), `"raw_canonical_linkage_complete":true`) {
		t.Fatalf("explicit Tier A semantics missing: %s, %v", encoded, err)
	}
}

func TestCoherentMarketDataTierAManifestRejectsCombinedOrdinalHole(t *testing.T) {
	base := t.TempDir()
	profile := CollectorProfile{Instance: "collector-1", Region: "test-region", MinimumReaderVersion: "dataset-reader.v2"}
	firstOrdinals, secondOrdinals := &runtimecore.IngestOrdinals{}, &runtimecore.IngestOrdinals{}
	first, err := NewCoherentMarketData(filepath.Join(base, "binance"), "binance-hole", "binance-hole-session", "binance",
		firstOrdinals, func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCoherentMarketData(filepath.Join(base, "bybit"), "bybit-hole", "bybit-hole-session", "bybit",
		secondOrdinals, func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	// Independent counters produce duplicate ordinal 1, which cannot be a complete combined dataset.
	recordForExchange(t, first, "binance", "binance-hole-session", 1, 1)
	recordForExchange(t, second, "bybit", "bybit-hole-session", 1, 1)
	firstManifest, _ := first.Flush()
	secondManifest, _ := second.Flush()
	_, err = BuildTierAManifest("combined-hole", time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
		map[string]string{"binance": first.root, "bybit": second.root}, []DatasetManifest{firstManifest, secondManifest})
	if recorderCode(err) != "tier_a_hidden_gap" {
		t.Fatalf("combined ordinal defect = %v", err)
	}
}

func recordForExchange(
	t *testing.T,
	recorder *Recorder,
	exchange, session string,
	generation, sequence uint64,
) {
	t.Helper()
	now := time.Unix(1_700_000_000+int64(sequence), int64(generation)).UTC()
	recordForExchangeAt(t, recorder, exchange, session, generation, sequence, now)
}

func recordForExchangeAt(
	t *testing.T,
	recorder *Recorder,
	exchange, session string,
	generation, sequence uint64,
	now time.Time,
) {
	t.Helper()
	payload := []byte(`{"kind":"depth"}`)
	link, err := recorder.RecordRaw(RawInput{Exchange: exchange, EventType: EventDepth,
		Instrument: recorderInstrument(t), SessionID: session, ConnectionID: "connection-1",
		ConnectionGeneration: generation, MonotonicOffsetNanos: sequence,
		RecordedLogicalTime: sequence, SourceSequence: eventID(sequence), ExchangeTime: &now,
		ReceivedAt: now, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if err = recorder.RecordCanonical(CanonicalInput{Link: link, EventID: eventID(link.IngestOrdinal),
		ParserVersion: "parser-v1", NormalizationVersion: "normalizer-v1",
		Canonical: []byte(`{"sequence":1}`)}); err != nil {
		t.Fatal(err)
	}
}
