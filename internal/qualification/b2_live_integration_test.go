package qualification

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

func TestB2ProductionPublicRecordOnlyAndCoherentQualification(t *testing.T) {
	if os.Getenv("AXIOM_B2_LIVE_PUBLIC") != "1" {
		t.Skip("AXIOM_B2_LIVE_PUBLIC=1 is required")
	}
	qualification := newLiveQualification(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	binanceResult, bybitResult := sampleLivePair(t, ctx, qualification.instrument,
		qualification.binanceClient, qualification.bybitClient, qualification.binanceSink, qualification.bybitSink)
	views := publishLiveViews(t, qualification, binanceResult, bybitResult)
	path, tierA, records := persistLiveTierA(t, qualification)
	t.Logf("B2_LIVE_DATASET_EVIDENCE root=%s manifest=%s tier_a_hash=%s records=%d",
		qualification.root, filepath.Base(path), tierA.Hash, records)
	triggerOffset := uint64(qualification.monotonic())
	triggerOrdinal := max(binanceResult.token.IngestOrdinal, bybitResult.token.IngestOrdinal) + 1
	joined, err := views.CoherentAsOf([]runtimecore.MarketKey{
		{Exchange: "bybit", Instrument: qualification.instrument},
		{Exchange: "binance", Instrument: qualification.instrument},
	}, runtimecore.AsOfTrigger{MonotonicNanos: triggerOffset, IngestOrdinal: triggerOrdinal,
		UTC: time.Now().UTC()}, runtimecore.InitialB2CoherentPolicy())
	if err != nil {
		t.Fatalf("live coherent view rejected: %v", err)
	}
	t.Logf("B2_LIVE_QUALIFICATION root=%s manifest=%s coherent_view=%s records=%d",
		qualification.root, filepath.Base(path), joined.Identity(), records)
}

type liveQualification struct {
	root                           string
	profile                        recorder.CollectorProfile
	instrument                     domain.Instrument
	monotonic                      exchangecontracts.MonotonicSource
	binanceRecorder, bybitRecorder *recorder.Recorder
	binanceClient                  *binance.PublicClient
	bybitClient                    *bybit.PublicClient
	binanceSink, bybitSink         exchangecontracts.PublicRecorder
}

func newLiveQualification(t *testing.T) liveQualification {
	t.Helper()
	evidenceRoot := os.Getenv("AXIOM_B2_LIVE_EVIDENCE_ROOT")
	if evidenceRoot == "" {
		evidenceRoot = t.TempDir()
	}
	region := os.Getenv("AXIOM_B2_COLLECTOR_REGION")
	if region == "" {
		region = "local"
	}
	session := fmt.Sprintf("b2-live-%d", time.Now().UTC().UnixNano())
	qualification := liveQualification{root: filepath.Join(evidenceRoot, session),
		profile: recorder.CollectorProfile{Instance: "b2-live-collector", Region: region,
			MinimumReaderVersion: "dataset-reader.v2"},
		monotonic: exchangecontracts.NewProcessMonotonicSource()}
	ordinals := &runtimecore.IngestOrdinals{}
	qualification.binanceRecorder = liveB2Recorder(t, filepath.Join(qualification.root, "binance"),
		"binance-b2-live", session+"-binance", "binance", ordinals, qualification.profile)
	qualification.bybitRecorder = liveB2Recorder(t, filepath.Join(qualification.root, "bybit"),
		"bybit-b2-live", session+"-bybit", "bybit", ordinals, qualification.profile)
	var err error
	qualification.binanceSink, err = recorder.NewBinanceStreamSink(qualification.binanceRecorder)
	if err != nil {
		t.Fatal(err)
	}
	qualification.bybitSink, err = recorder.NewPublicStreamSink(qualification.bybitRecorder,
		"bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		t.Fatal(err)
	}
	clock := &domain.SystemClock{}
	qualification.binanceClient, err = binance.NewPublicClientWithMonotonic("market-data-only-v1", clock, qualification.monotonic)
	if err != nil {
		t.Fatal(err)
	}
	qualification.bybitClient, err = bybit.NewPublicClientWithMonotonic("bybit-public-v1", clock, qualification.monotonic)
	if err != nil {
		t.Fatal(err)
	}
	qualification.instrument, err = domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	return qualification
}

func publishLiveViews(
	t *testing.T,
	qualification liveQualification,
	evidence ...liveMarketEvidence,
) *runtimecore.MarketViews {
	t.Helper()
	views := runtimecore.NewMarketViews()
	for _, item := range evidence {
		input := liveCoherentInput(t, qualification.instrument, item, qualification.profile)
		if err := views.ActivateGeneration(input.Key, input.ConnectionGeneration); err != nil {
			t.Fatal(err)
		}
		if _, err := views.Publish(input); err != nil {
			t.Fatal(err)
		}
	}
	return views
}

func persistLiveTierA(t *testing.T, qualification liveQualification) (string, recorder.TierAManifest, uint64) {
	t.Helper()
	binanceManifest, err := qualification.binanceRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	bybitManifest, err := qualification.bybitRecorder.Flush()
	if err != nil {
		t.Fatal(err)
	}
	tierA, err := recorder.BuildTierAManifest("b2-live-tier-a", time.Now().UTC(),
		map[string]string{"binance": filepath.Join(qualification.root, "binance"),
			"bybit": filepath.Join(qualification.root, "bybit")},
		[]recorder.DatasetManifest{binanceManifest, bybitManifest})
	if err != nil {
		t.Fatal(err)
	}
	path, err := recorder.WriteTierAManifest(filepath.Join(qualification.root, "qualification"), tierA)
	if err != nil {
		t.Fatal(err)
	}
	return path, tierA, binanceManifest.CanonicalCount + bybitManifest.CanonicalCount
}

type liveMarketEvidence struct {
	exchange string
	health   exchangecontracts.ClockHealth
	samples  []exchangecontracts.ClockHealth
	snapshot exchangecontracts.BookSnapshot
	token    exchangecontracts.StreamRecordToken
	err      error
}

func sampleLivePair(
	t *testing.T,
	ctx context.Context,
	instrument domain.Instrument,
	binanceClient *binance.PublicClient,
	bybitClient *bybit.PublicClient,
	binanceSink, bybitSink exchangecontracts.PublicRecorder,
) (liveMarketEvidence, liveMarketEvidence) {
	t.Helper()
	var binanceResult, bybitResult liveMarketEvidence
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		binanceResult.exchange = "binance"
		binanceResult.health, binanceResult.samples, binanceResult.err = sampleBoundedClock(ctx, 10, 10, func() (exchangecontracts.ClockHealth, error) {
			health, _, sampleErr := binanceClient.SampleServerTimeRecorded(ctx,
				instrument, "b2-live-binance", 1, binanceSink)
			return health, sampleErr
		})
	}()
	go func() {
		defer group.Done()
		bybitResult.exchange = "bybit"
		bybitResult.health, bybitResult.samples, bybitResult.err = sampleBoundedClock(ctx, 5, 4, func() (exchangecontracts.ClockHealth, error) {
			health, _, sampleErr := bybitClient.SampleServerTimeRecorded(ctx,
				instrument, "b2-live-bybit", 1, bybitSink)
			return health, sampleErr
		})
	}()
	group.Wait()
	requireLiveClocks(t, binanceResult, bybitResult)
	sampleLiveSnapshots(ctx, instrument, binanceClient, bybitClient, binanceSink, bybitSink,
		&binanceResult, &bybitResult)
	requireLiveEvidence(t, binanceResult, bybitResult)
	return binanceResult, bybitResult
}

func requireLiveClocks(t *testing.T, results ...liveMarketEvidence) {
	t.Helper()
	for _, result := range results {
		if result.err != nil || !result.health.Eligible {
			t.Logf("%s clock bounds: %s", result.exchange, summarizeClockSamples(result.samples))
			t.Fatalf("%s live clock rejected: uncertainty=%s eligible=%t error=%v",
				result.exchange, result.health.Uncertainty, result.health.Eligible, result.err)
		}
	}
}

func sampleLiveSnapshots(
	ctx context.Context,
	instrument domain.Instrument,
	binanceClient *binance.PublicClient,
	bybitClient *bybit.PublicClient,
	binanceSink, bybitSink exchangecontracts.PublicRecorder,
	binanceResult, bybitResult *liveMarketEvidence,
) {
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		binanceResult.snapshot, binanceResult.token, binanceResult.err = binanceClient.SnapshotRecorded(ctx,
			exchangecontracts.SnapshotRequest{Instrument: instrument, Depth: 100}, "b2-live-binance", 1, binanceSink)
	}()
	go func() {
		defer group.Done()
		bybitResult.snapshot, bybitResult.token, bybitResult.err = bybitClient.SnapshotRecorded(ctx,
			exchangecontracts.SnapshotRequest{Instrument: instrument, Depth: 50}, "b2-live-bybit", 1, bybitSink)
	}()
	group.Wait()
}

func requireLiveEvidence(t *testing.T, results ...liveMarketEvidence) {
	t.Helper()
	for _, result := range results {
		t.Logf("%s clock samples=%d best_uncertainty=%s final_uncertainty=%s",
			result.exchange, len(result.samples), result.health.Uncertainty,
			lastClockUncertainty(result.samples))
		if result.err != nil || !result.health.Eligible ||
			result.token.IngestOrdinal == 0 || result.token.MonotonicOffsetNanos == 0 ||
			result.token.ConnectionGeneration != 1 || result.token.ReceivedAt.Validate() != nil {
			t.Logf("%s clock bounds: %s", result.exchange, summarizeClockSamples(result.samples))
			t.Fatalf("%s live evidence rejected: uncertainty=%s eligible=%t ordinal=%d generation=%d error=%v",
				result.exchange, result.health.Uncertainty, result.health.Eligible,
				result.token.IngestOrdinal, result.token.ConnectionGeneration, result.err)
		}
	}
}

func sampleBoundedClock(
	ctx context.Context,
	batches, batchSize int,
	sample func() (exchangecontracts.ClockHealth, error),
) (exchangecontracts.ClockHealth, []exchangecontracts.ClockHealth, error) {
	type result struct {
		health exchangecontracts.ClockHealth
		err    error
	}
	var best exchangecontracts.ClockHealth
	var samples []exchangecontracts.ClockHealth
	var latestErr error
	for range batches {
		results := make(chan result, batchSize)
		for range batchSize {
			go func() {
				health, err := sample()
				results <- result{health: health, err: err}
			}()
		}
		for range batchSize {
			select {
			case <-ctx.Done():
				return best, samples, ctx.Err()
			case observed := <-results:
				if observed.err != nil {
					latestErr = observed.err
					continue
				}
				latestErr = nil
				samples = append(samples, observed.health)
				if best.ObservedAt.IsZero() || observed.health.Uncertainty < best.Uncertainty {
					best = observed.health
				}
			}
		}
		if best.Eligible && best.Uncertainty <= 100*time.Millisecond {
			return best, samples, nil
		}
	}
	return best, samples, latestErr
}

func lastClockUncertainty(samples []exchangecontracts.ClockHealth) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	return samples[len(samples)-1].Uncertainty
}

func summarizeClockSamples(samples []exchangecontracts.ClockHealth) string {
	parts := make([]string, len(samples))
	for index, sample := range samples {
		parts[index] = fmt.Sprintf("%d:%s+-%s", index+1, sample.Offset, sample.Uncertainty)
	}
	return strings.Join(parts, ",")
}

func liveCoherentInput(
	t *testing.T,
	instrument domain.Instrument,
	evidence liveMarketEvidence,
	profile recorder.CollectorProfile,
) runtimecore.MarketViewInput {
	t.Helper()
	book, err := marketdata.NewBook(evidence.exchange, instrument, 50, 100, nil)
	if err != nil || book.BeginGeneration("b2-live-"+evidence.exchange, 1) != nil {
		t.Fatal(err)
	}
	base := evidence.token.ReceivedAt.UTC
	sequence := evidence.token.IngestOrdinal * 3
	observation := marketdata.Observation{
		ReceivedAt:   domain.EventTime{UTC: base, Sequence: sequence - 2},
		ProcessedAt:  domain.EventTime{UTC: base.Add(time.Nanosecond), Sequence: sequence - 1},
		PublishedAt:  domain.EventTime{UTC: base.Add(2 * time.Nanosecond), Sequence: sequence},
		ConnectionID: "b2-live-" + evidence.exchange, ConnectionGeneration: 1,
		SourceSequence: evidence.snapshot.LastSequence, IngestOrdinal: evidence.token.IngestOrdinal,
		ReceivedOffsetNanos:  evidence.token.MonotonicOffsetNanos,
		ProcessedOffsetNanos: evidence.token.MonotonicOffsetNanos + 1,
		PublishedOffsetNanos: evidence.token.MonotonicOffsetNanos + 2,
	}
	if err = book.ReplaceSnapshot(evidence.snapshot, observation); err != nil {
		t.Fatal(err)
	}
	input, err := marketdata.CoherentInput(book.View(), evidence.health, profile.Instance, profile.Region)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func liveB2Recorder(
	t *testing.T,
	root, datasetID, sessionID, exchange string,
	ordinals *runtimecore.IngestOrdinals,
	profile recorder.CollectorProfile,
) *recorder.Recorder {
	t.Helper()
	value, err := recorder.NewB2(root, datasetID, sessionID, exchange, ordinals,
		func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
