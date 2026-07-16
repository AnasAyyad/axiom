package backtest

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

func TestDatasetReaderVerifiesChainAndStreamsOnePair(t *testing.T) {
	root, selected := datasetFixture(t, 2)
	reader, err := OpenDataset(root, selected, compatibility(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	descriptor := reader.Descriptor()
	if descriptor.Revision != 2 || descriptor.RecordCount != 2 || descriptor.Confidence != ConfidenceA {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	for ordinal := uint64(1); ordinal <= 2; ordinal++ {
		event, ok, nextErr := reader.Next()
		if nextErr != nil || !ok || event.Record.IngestOrdinal != ordinal || len(reader.rows) != 1 {
			t.Fatalf("event %d = %#v, %v, rows=%d", ordinal, event, nextErr, len(reader.rows))
		}
	}
	if _, ok, err := reader.Next(); err != nil || ok {
		t.Fatalf("end = %v, %v", ok, err)
	}
	if err = reader.SeekOrdinal(2); err != nil {
		t.Fatal(err)
	}
	seeked, ok, err := reader.Next()
	if err != nil || !ok || seeked.Record.IngestOrdinal != 2 {
		t.Fatalf("seeked = %#v, %v", seeked, err)
	}
}

func TestDatasetReaderDowngradesLowDensityAndRejectsVersions(t *testing.T) {
	root, selected := datasetFixture(t, 2)
	reader, err := OpenDataset(root, selected, compatibility(2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if reader.Descriptor().Confidence != ConfidenceC || reader.Descriptor().LowDensitySegments != 2 {
		t.Fatalf("descriptor = %#v", reader.Descriptor())
	}
	incompatible := compatibility(1, 0)
	incompatible.ParserVersion = "unsupported-parser"
	if code(OpenDataset(root, selected, incompatible)) != "dataset_version_incompatible" {
		t.Fatal("incompatible parser was accepted")
	}
}

func TestRunManifestHashIncludesBuildDatasetAndNamespace(t *testing.T) {
	root, selected := datasetFixture(t, 1)
	reader, err := OpenDataset(root, selected, compatibility(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := domain.NewRunID("fixture")
	hash := strings.Repeat("b", 64)
	manifest := RunManifest{RunID: runID, Mode: "replay", CodeCommit: strings.Repeat("a", 64),
		Build: CurrentBuildIdentity([]string{"trimpath"}, hash, hash), Dataset: reader.Descriptor(),
		ConfigurationHash: hash, Seed: "seed-1", SchedulerVersion: "scheduler-v1",
		SerializationVersion: "canonical-json-v1", StartingBalanceHash: hash,
		Models: ModelNamespace{ID: "models-v1", MarketContext: "production-public", LiquidityDomain: "combined-1",
			FeeDomain: "fee-v1", LatencyDomain: "latency-v1", FillDomain: "fill-v1"}}
	first, err := manifest.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	manifest.Models.LiquidityDomain = "independent-1"
	second, err := manifest.CanonicalHash()
	if err != nil || first == second {
		t.Fatalf("hashes = %s/%s, %v", first, second, err)
	}
	if manifest.Models.Comparable(ModelNamespace{}) {
		t.Fatal("invalid namespace was comparable")
	}
}

func datasetFixture(t *testing.T, revisions int) (string, string) {
	t.Helper()
	root := t.TempDir()
	recording, err := recorder.New(root, "fixture-dataset", "fixture-session", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	for revision := 1; revision <= revisions; revision++ {
		recordFixturePair(t, recording, uint64(revision))
		if _, err = recording.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	name := fmt.Sprintf("fixture-session-%06d.dataset.json", revisions)
	return root, filepath.Join(root, name)
}

func recordFixturePair(t *testing.T, recording *recorder.Recorder, sequence uint64) {
	t.Helper()
	now := time.Unix(1_700_000_000+int64(sequence), 0).UTC()
	instrument, _ := domain.NewSpotInstrument("BTC", "USDT")
	payload := []byte(fmt.Sprintf(`{"sequence":%d}`, sequence))
	link, err := recording.RecordRaw(recorder.RawInput{Exchange: "binance", EventType: recorder.EventDepth,
		Instrument: instrument, SessionID: "fixture-session", ConnectionID: "fixture-connection",
		ConnectionGeneration: 1, MonotonicOffsetNanos: sequence, RecordedLogicalTime: sequence,
		SourceSequence: fmt.Sprintf("%d", sequence), ExchangeTime: &now, ReceivedAt: now, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if err = recording.RecordCanonical(recorder.CanonicalInput{Link: link,
		EventID: fmt.Sprintf("fixture-event-%d", sequence), ParserVersion: "parser-v1",
		NormalizationVersion: "normalizer-v1", Canonical: payload}); err != nil {
		t.Fatal(err)
	}
}

func compatibility(minimum, maximum uint64) DatasetCompatibility {
	return DatasetCompatibility{SourceCommit: strings.Repeat("a", 64), ParserVersion: "parser-v1",
		NormalizationVersion: "normalizer-v1", MinimumRecordsPerPair: minimum, MaximumLowDensityPairs: maximum}
}

func code(_ *DatasetReader, err error) string {
	failure, ok := err.(*Error)
	if !ok {
		return ""
	}
	return failure.Code
}
