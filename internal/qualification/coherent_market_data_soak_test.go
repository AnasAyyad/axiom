package qualification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

const (
	coherentMarketDataFormalSoakDuration  = 72 * time.Hour
	coherentMarketDataReadinessTimeout    = 2 * time.Minute
	coherentMarketDataReadinessPollEvery  = time.Second
	coherentMarketDataFormalFlushEvery    = 5 * time.Minute
	coherentMarketDataDeclaredHeapLimit   = 1024 * 1024 * 1024
	coherentMarketDataMinimumFreeBytes    = 10 * 1024 * 1024 * 1024
	coherentMarketDataBinanceInstance     = "coherent_market_data-binance-collector"
	coherentMarketDataBybitInstance       = "coherent_market_data-bybit-collector"
	coherentMarketDataMinimumReader       = "dataset-reader.v2"
	coherentMarketDataQualificationSchema = "axiom.coherent_market_data-soak.v1"
	coherentMarketDataStatusSchema        = "axiom.coherent_market_data-soak-status.v1"
	coherentMarketDataJournalSchema       = "axiom.coherent_market_data-soak-events.v1"
)

func TestCoherentMarketDataContinuous72HourPublicSoak(t *testing.T) {
	if os.Getenv("AXIOM_COHERENT_MARKET_DATA_SOAK") != "1" {
		t.Skip("AXIOM_COHERENT_MARKET_DATA_SOAK=1 is required")
	}
	runCoherentMarketDataSoak(t, requireCoherentMarketDataOutput(t), coherentMarketDataFormalSoakDuration, coherentMarketDataFormalFlushEvery, coherentMarketDataCoherentSampleEvery, true)
}

func TestCoherentMarketDataPublicSoakHarnessSmoke(t *testing.T) {
	if os.Getenv("AXIOM_COHERENT_MARKET_DATA_SOAK_SMOKE") != "1" {
		t.Skip("AXIOM_COHERENT_MARKET_DATA_SOAK_SMOKE=1 is required")
	}
	runCoherentMarketDataSoak(t, requireCoherentMarketDataOutput(t), 20*time.Second, 5*time.Second, coherentMarketDataCoherentSampleEvery, false)
}

func requireCoherentMarketDataOutput(t *testing.T) string {
	t.Helper()
	root := os.Getenv("AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		t.Fatal("AXIOM_COHERENT_MARKET_DATA_SOAK_OUTPUT must be a dedicated absolute directory")
	}
	return root
}

type coherentMarketDataRecorderEvidence struct {
	ManifestRevision uint64                `json:"manifest_revision"`
	ManifestHash     string                `json:"manifest_hash,omitempty"`
	Pending          recorder.PendingUsage `json:"pending"`
}

type coherentMarketDataTierAEvidence struct {
	Path     string `json:"path,omitempty"`
	Hash     string `json:"hash,omitempty"`
	Complete bool   `json:"complete"`
}

type coherentMarketDataSoakEvidence struct {
	SchemaVersion        string                                               `json:"schema_version"`
	SourceCommit         string                                               `json:"source_commit"`
	Formal               bool                                                 `json:"formal"`
	HarnessPassed        bool                                                 `json:"harness_passed"`
	Qualified            bool                                                 `json:"qualified"`
	TerminalCause        string                                               `json:"terminal_cause"`
	HarnessStartedAt     time.Time                                            `json:"harness_started_at"`
	ReadinessPassedAt    time.Time                                            `json:"readiness_passed_at,omitempty"`
	StartedAt            time.Time                                            `json:"official_started_at,omitempty"`
	EndedAt              time.Time                                            `json:"official_ended_at,omitempty"`
	RequiredDuration     time.Duration                                        `json:"required_duration_nanos"`
	ActualDuration       time.Duration                                        `json:"actual_duration_nanos"`
	ReadinessTimeout     time.Duration                                        `json:"readiness_timeout_nanos"`
	SampleEvery          time.Duration                                        `json:"sample_every_nanos"`
	FlushEvery           time.Duration                                        `json:"flush_every_nanos"`
	Policy               runtimecore.CoherentPolicy                           `json:"coherent_policy"`
	CollectorRegion      string                                               `json:"collector_region"`
	Instruments          []string                                             `json:"instruments"`
	Streams              []string                                             `json:"streams"`
	BookDepth            int                                                  `json:"book_depth"`
	BinanceSnapshotDepth uint32                                               `json:"binance_snapshot_depth"`
	QueueCapacity        int                                                  `json:"queue_capacity"`
	HeapLimitBytes       uint64                                               `json:"heap_limit_bytes"`
	MinimumFreeBytes     uint64                                               `json:"minimum_free_bytes"`
	Memory               []memorySample                                       `json:"memory_samples"`
	Storage              []storageSample                                      `json:"storage_samples"`
	PositiveLeakTrend    bool                                                 `json:"positive_heap_trend"`
	Health               map[string]exchangecontracts.CollectorHealthSnapshot `json:"final_health"`
	Pairs                map[string]coherentMarketDataPairSnapshot            `json:"pairs"`
	BinanceCollectors    map[string]binance.CollectorStatsSnapshot            `json:"binance_collectors"`
	BybitCollectors      map[string]bybit.CollectorStats                      `json:"bybit_collectors"`
	Recorders            map[string]coherentMarketDataRecorderEvidence        `json:"recorders"`
	TierA                coherentMarketDataTierAEvidence                      `json:"tier_a"`
	CoherentSegments     qualificationJournalEvidence                         `json:"coherent_segments"`
	EventJournal         qualificationJournalEvidence                         `json:"event_journal"`
	CollectorRunning     map[string]bool                                      `json:"collector_running"`
	Failures             []string                                             `json:"failures"`
	FailureDetails       []qualificationFailure                               `json:"failure_details,omitempty"`
	root                 string                                               `json:"-"`
}

type coherentMarketDataSoakStatus struct {
	SchemaVersion        string                                               `json:"schema_version"`
	SourceCommit         string                                               `json:"source_commit"`
	Formal               bool                                                 `json:"formal"`
	TerminalCause        string                                               `json:"terminal_cause,omitempty"`
	HarnessStartedAt     time.Time                                            `json:"harness_started_at"`
	OfficialStartedAt    time.Time                                            `json:"official_started_at,omitempty"`
	ObservedAt           time.Time                                            `json:"observed_at"`
	Elapsed              time.Duration                                        `json:"elapsed_nanos"`
	RequiredDuration     time.Duration                                        `json:"required_duration_nanos"`
	ProvisionalQualified bool                                                 `json:"provisional_qualified"`
	ProvisionalFailures  []string                                             `json:"provisional_failures"`
	Health               map[string]exchangecontracts.CollectorHealthSnapshot `json:"combined_health"`
	Pairs                map[string]coherentMarketDataPairSnapshot            `json:"pairs"`
	BinanceCollectors    map[string]binance.CollectorStatsSnapshot            `json:"binance_collectors"`
	BybitCollectors      map[string]bybit.CollectorStats                      `json:"bybit_collectors"`
	Recorders            map[string]coherentMarketDataRecorderEvidence        `json:"recorders"`
	Memory               memorySample                                         `json:"memory"`
	Storage              storageSample                                        `json:"storage"`
	CollectorRunning     map[string]bool                                      `json:"collector_running"`
	FailureDetails       []qualificationFailure                               `json:"failure_details,omitempty"`
	CoherentSequence     uint64                                               `json:"coherent_segment_sequence"`
	CoherentHash         string                                               `json:"coherent_segment_hash,omitempty"`
	CoherentSamples      uint64                                               `json:"coherent_sample_count"`
	EventJournalSequence uint64                                               `json:"event_journal_sequence"`
	EventJournalHash     string                                               `json:"event_journal_hash,omitempty"`
}

type coherentMarketDataSoakComponents struct {
	monotonic         exchangecontracts.MonotonicSource
	profiles          map[string]recorder.CollectorProfile
	instruments       map[string]domain.Instrument
	binanceClient     *binance.PublicClient
	bybitClient       *bybit.PublicClient
	binanceRecorder   *recorder.Recorder
	bybitRecorder     *recorder.Recorder
	binanceCollectors map[string]*binance.InstrumentCollector
	bybitCollectors   map[string]*bybit.InstrumentCollector
}

type coherentMarketDataCollectorResult struct {
	exchange, instrument string
	err                  error
}
type coherentMarketDataLatestManifests struct{ binance, bybit recorder.DatasetManifest }

type coherentMarketDataMonitor struct {
	root       string
	components coherentMarketDataSoakComponents
	results    <-chan coherentMarketDataCollectorResult
	latest     *coherentMarketDataLatestManifests
	evidence   *coherentMarketDataSoakEvidence
	pairs      map[string]*coherentMarketDataPairTracker
	segments   *coherentMarketDataCoherentSegmentWriter
	journal    *qualificationJournal
	status     func(string, coherentMarketDataSoakStatus) error
	resources  func(time.Time, string) (memorySample, storageSample)
}

func runCoherentMarketDataSoak(t *testing.T, root string, duration, flushEvery, sampleEvery time.Duration, formal bool) {
	t.Helper()
	evidence, journal, segmentWriter, components := prepareCoherentMarketDataSoakRun(t, root, duration, flushEvery, sampleEvery, formal)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results, group := startCoherentMarketDataCollectors(ctx, components)
	latest := coherentMarketDataLatestManifests{}
	monitor := coherentMarketDataMonitor{root: root, components: components, results: results, latest: &latest, evidence: &evidence,
		pairs: newCoherentMarketDataPairTrackers(), segments: segmentWriter, journal: journal, status: writeCoherentMarketDataSoakStatus}
	readyAt, failure := awaitCoherentMarketDataReadiness(ctx, monitor, coherentMarketDataReadinessTimeout, coherentMarketDataReadinessPollEvery)
	if failure != "" {
		appendCoherentMarketDataFailure(&evidence, failure, "readiness", "readiness_gate", nil)
		cancel()
	} else {
		evidence.ReadinessPassedAt, evidence.StartedAt = readyAt, readyAt
		appendCoherentMarketDataEvent(journal, &evidence, qualificationEvent{RecordedAt: readyAt, Phase: "readiness", Outcome: "passed"})
		officialEnd := time.NewTimer(duration)
		if failure = monitorCoherentMarketDataSoak(ctx, officialEnd.C, flushEvery, sampleEvery, monitor); failure != "" {
			appendCoherentMarketDataFailure(&evidence, failure, "monitor", "fail_closed", nil)
			cancel()
		}
		officialEnd.Stop()
	}
	cancel()
	group.Wait()
	collectCoherentMarketDataCollectorResults(results, &evidence, journal)
	finishCoherentMarketDataSoak(t, monitor, formal)
}

func prepareCoherentMarketDataSoakRun(t *testing.T, root string, duration, flushEvery, sampleEvery time.Duration, formal bool) (coherentMarketDataSoakEvidence, *qualificationJournal, *coherentMarketDataCoherentSegmentWriter, coherentMarketDataSoakComponents) {
	t.Helper()
	sourceCommit, err := qualificationSourceCommitFromEnv("AXIOM_COHERENT_MARKET_DATA_SOURCE_COMMIT")
	if err != nil {
		t.Fatal(err)
	}
	region := os.Getenv("AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION")
	if !validQualificationLabel(region) {
		t.Fatal("AXIOM_COHERENT_MARKET_DATA_COLLECTOR_REGION must be a nonempty bounded label")
	}
	if err = prepareEmptyRoot(root); err != nil {
		t.Fatal(err)
	}
	harnessStarted := time.Now().UTC()
	evidence := newCoherentMarketDataSoakEvidence(root, sourceCommit, region, harnessStarted, duration, flushEvery, sampleEvery, formal)
	journal, err := newNamedQualificationJournal(root, "coherent_market_data-soak-events.jsonl", coherentMarketDataJournalSchema, "COHERENT_MARKET_DATA_EVENT", sourceCommit, harnessStarted)
	if err != nil {
		t.Fatal("coherent market data qualification event journal could not be created")
	}
	segments, err := newCoherentMarketDataCoherentSegmentWriter(filepath.Join(root, "coherent"), sourceCommit, writeAtomicJSON)
	if err != nil {
		_ = journal.Close()
		t.Fatal("coherent market data coherent segment writer could not be created")
	}
	if !appendCoherentMarketDataEvent(journal, &evidence, qualificationEvent{RecordedAt: harnessStarted, Phase: "preflight", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("coherent market data qualification journal preflight failed")
	}
	components := newCoherentMarketDataSoakComponents(t, root, region, qualificationLifecycleSink{journal: journal})
	if !appendCoherentMarketDataEvent(journal, &evidence, qualificationEvent{Phase: "startup", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("coherent market data startup evidence failed")
	}
	return evidence, journal, segments, components
}

func newCoherentMarketDataSoakEvidence(root, sourceCommit, region string, started time.Time, duration, flushEvery, sampleEvery time.Duration, formal bool) coherentMarketDataSoakEvidence {
	return coherentMarketDataSoakEvidence{SchemaVersion: coherentMarketDataQualificationSchema, SourceCommit: sourceCommit, Formal: formal,
		HarnessStartedAt: started, RequiredDuration: duration, ReadinessTimeout: coherentMarketDataReadinessTimeout,
		SampleEvery: sampleEvery, FlushEvery: flushEvery, Policy: runtimecore.InitialCoherentMarketDataCoherentPolicy(), CollectorRegion: region,
		Instruments: []string{"BTCUSDT", "ETHUSDT", "ETHBTC"}, Streams: []string{"depth", "trade", "ticker", "kline_15m", "kline_1h", "kline_4h"},
		BookDepth: 1000, BinanceSnapshotDepth: 5000, QueueCapacity: 16384, HeapLimitBytes: coherentMarketDataDeclaredHeapLimit,
		MinimumFreeBytes: coherentMarketDataMinimumFreeBytes, Health: make(map[string]exchangecontracts.CollectorHealthSnapshot),
		Pairs: make(map[string]coherentMarketDataPairSnapshot), BinanceCollectors: make(map[string]binance.CollectorStatsSnapshot),
		BybitCollectors: make(map[string]bybit.CollectorStats), Recorders: make(map[string]coherentMarketDataRecorderEvidence),
		CollectorRunning: map[string]bool{"binance:BTCUSDT": true, "binance:ETHUSDT": true, "binance:ETHBTC": true,
			"bybit:BTCUSDT": true, "bybit:ETHUSDT": true, "bybit:ETHBTC": true},
		EventJournal:     qualificationJournalEvidence{Path: "coherent_market_data-soak-events.jsonl"},
		CoherentSegments: qualificationJournalEvidence{Path: filepath.Join("coherent", coherentMarketDataCoherentManifestName)}, root: root}
}

func newCoherentMarketDataSoakComponents(t *testing.T, root, region string, lifecycleSink exchangecontracts.LifecycleEvidenceSink) coherentMarketDataSoakComponents {
	t.Helper()
	clock := &domain.SystemClock{}
	monotonic := exchangecontracts.NewProcessMonotonicSource()
	ordinals := &runtimecore.IngestOrdinals{}
	profiles := map[string]recorder.CollectorProfile{
		"binance": {Instance: coherentMarketDataBinanceInstance, Region: region, MinimumReaderVersion: coherentMarketDataMinimumReader},
		"bybit":   {Instance: coherentMarketDataBybitInstance, Region: region, MinimumReaderVersion: coherentMarketDataMinimumReader},
	}
	binanceRecorder := mustCoherentMarketDataRecorder(t, filepath.Join(root, "binance"), "coherent_market_data-formal-binance", "coherent_market_data-formal-binance", "binance", ordinals, profiles["binance"])
	bybitRecorder := mustCoherentMarketDataRecorder(t, filepath.Join(root, "bybit"), "coherent_market_data-formal-bybit", "coherent_market_data-formal-bybit", "bybit", ordinals, profiles["bybit"])
	binanceSink, err := recorder.NewBinanceStreamSink(binanceRecorder)
	if err != nil {
		t.Fatal(err)
	}
	bybitSink, err := recorder.NewPublicStreamSink(bybitRecorder, "bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		t.Fatal(err)
	}
	binanceClient, err := binance.NewRecorderPublicClientWithMonotonic("market-data-only-v1", clock, monotonic)
	if err != nil {
		t.Fatal(err)
	}
	bybitClient, err := bybit.NewPublicClientWithMonotonic("bybit-public-v1", clock, monotonic)
	if err != nil {
		t.Fatal(err)
	}
	components := coherentMarketDataSoakComponents{monotonic: monotonic, profiles: profiles, instruments: make(map[string]domain.Instrument),
		binanceClient: binanceClient, bybitClient: bybitClient, binanceRecorder: binanceRecorder, bybitRecorder: bybitRecorder,
		binanceCollectors: make(map[string]*binance.InstrumentCollector), bybitCollectors: make(map[string]*bybit.InstrumentCollector)}
	for _, instrument := range coherentMarketDataInstruments(t) {
		symbol := instrument.Symbol()
		components.instruments[symbol] = instrument
		binanceConfig := binance.DefaultCollectorConfig(instrument)
		binanceConfig.BookDepth, binanceConfig.SnapshotDepth, binanceConfig.QueueCapacity = 1000, 5000, 16384
		binanceConfig.CandleIntervals, binanceConfig.LifecycleEvidence = []string{"15m", "1h", "4h"}, lifecycleSink
		components.binanceCollectors[symbol], err = binance.NewInstrumentCollector(binanceConfig, binanceClient, binanceSink, clock)
		if err != nil {
			t.Fatal(err)
		}
		bybitConfig := bybit.DefaultCollectorConfig(instrument)
		bybitConfig.BookDepth, bybitConfig.QueueCapacity = 1000, 16384
		bybitConfig.CandleIntervals, bybitConfig.LifecycleEvidence = []string{"15m", "1h", "4h"}, lifecycleSink
		components.bybitCollectors[symbol], err = bybit.NewInstrumentCollector(bybitConfig, bybitClient, bybitSink, clock)
		if err != nil {
			t.Fatal(err)
		}
	}
	return components
}

func mustCoherentMarketDataRecorder(t *testing.T, root, datasetID, sessionID, exchange string, ordinals *runtimecore.IngestOrdinals, profile recorder.CollectorProfile) *recorder.Recorder {
	t.Helper()
	value, err := recorder.NewCoherentMarketData(root, datasetID, sessionID, exchange, ordinals, func(segments.Manifest) error { return nil }, nil, profile)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func coherentMarketDataInstruments(t *testing.T) []domain.Instrument {
	t.Helper()
	pairs := [][2]domain.AssetSymbol{{"BTC", "USDT"}, {"ETH", "USDT"}, {"ETH", "BTC"}}
	values := make([]domain.Instrument, 0, 3)
	for _, pair := range pairs {
		instrument, err := domain.NewSpotInstrument(pair[0], pair[1])
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, instrument)
	}
	return values
}

func startCoherentMarketDataCollectors(ctx context.Context, components coherentMarketDataSoakComponents) (chan coherentMarketDataCollectorResult, *sync.WaitGroup) {
	results := make(chan coherentMarketDataCollectorResult, 6)
	group := &sync.WaitGroup{}
	for symbol, collector := range components.binanceCollectors {
		group.Add(1)
		go func(instrument string, current *binance.InstrumentCollector) {
			defer group.Done()
			results <- coherentMarketDataCollectorResult{"binance", instrument, current.Run(ctx)}
		}(symbol, collector)
	}
	for symbol, collector := range components.bybitCollectors {
		group.Add(1)
		go func(instrument string, current *bybit.InstrumentCollector) {
			defer group.Done()
			results <- coherentMarketDataCollectorResult{"bybit", instrument, current.Run(ctx)}
		}(symbol, collector)
	}
	return results, group
}

func awaitCoherentMarketDataReadiness(ctx context.Context, monitor coherentMarketDataMonitor, timeout, pollEvery time.Duration) (time.Time, string) {
	deadline, ticker := time.NewTimer(timeout), time.NewTicker(pollEvery)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return time.Time{}, "readiness_canceled"
		case result := <-monitor.results:
			recordCoherentMarketDataCollectorFailure(result, monitor.evidence, monitor.journal)
			return time.Time{}, "collector_failed"
		case <-deadline.C:
			appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{Phase: "readiness", Outcome: "failed", Code: "readiness_timeout"})
			return time.Time{}, "readiness_timeout"
		case observed := <-ticker.C:
			samples, eligible := captureCoherentMarketDataSamples(monitor.components, observed.UTC(), "readiness")
			for _, sample := range samples {
				if err := monitor.segments.Append(sample); err != nil {
					return time.Time{}, "coherent_segment_failed"
				}
			}
			if allCoherentMarketDataHealthEligible(captureCoherentMarketDataHealth(monitor.components)) && eligible {
				return observed.UTC(), ""
			}
		}
	}
}

func monitorCoherentMarketDataSoak(ctx context.Context, officialEnd <-chan time.Time, flushEvery, sampleEvery time.Duration, monitor coherentMarketDataMonitor) string {
	if failure := writeCoherentMarketDataStatusStep(time.Now().UTC(), monitor); failure != "" {
		return failure
	}
	flushTicker, sampleTicker := time.NewTicker(flushEvery), time.NewTicker(sampleEvery)
	defer flushTicker.Stop()
	defer sampleTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ""
		case ended := <-officialEnd:
			return freezeCoherentMarketDataOfficialEnd(ended.UTC(), monitor)
		case result := <-monitor.results:
			if ctx.Err() != nil && normalCollectorCancellation(result.err) {
				return ""
			}
			recordCoherentMarketDataCollectorFailure(result, monitor.evidence, monitor.journal)
			return "collector_failed"
		case <-monitor.components.binanceRecorder.FlushRequired():
			if failure := flushCoherentMarketDataRecorder("binance", "capacity", monitor); failure != "" {
				return failure
			}
		case <-monitor.components.bybitRecorder.FlushRequired():
			if failure := flushCoherentMarketDataRecorder("bybit", "capacity", monitor); failure != "" {
				return failure
			}
		case observed := <-sampleTicker.C:
			if failure := sampleCoherentMarketDataPairs(observed.UTC(), monitor); failure != "" {
				return failure
			}
		case observed := <-flushTicker.C:
			if failure := flushCoherentMarketDataRecorder("binance", "scheduled", monitor); failure != "" {
				return failure
			}
			if failure := flushCoherentMarketDataRecorder("bybit", "scheduled", monitor); failure != "" {
				return failure
			}
			if err := monitor.segments.Flush(); err != nil {
				appendCoherentMarketDataFailure(monitor.evidence, "coherent_segment_failed", "coherent_segment_flush", "atomic_segment", err)
				return "coherent_segment_failed"
			}
			if failure := writeCoherentMarketDataStatusStep(observed.UTC(), monitor); failure != "" {
				return failure
			}
		}
	}
}

func sampleCoherentMarketDataPairs(observed time.Time, monitor coherentMarketDataMonitor) string {
	samples, _ := captureCoherentMarketDataSamples(monitor.components, observed, "official")
	for index := range samples {
		sample := &samples[index]
		tracker := monitor.pairs[sample.Pair]
		if tracker == nil {
			return "coherent_configuration_failed"
		}
		withinLimit := tracker.Observe(sample)
		if err := monitor.segments.Append(*sample); err != nil {
			appendCoherentMarketDataFailure(monitor.evidence, "coherent_segment_failed", "coherent_segment_append", "atomic_segment", err)
			return "coherent_segment_failed"
		}
		if !withinLimit {
			return "coherent_recovery_over_15_seconds"
		}
	}
	return coherentMarketDataCollectorFailure(monitor.components)
}

func captureCoherentMarketDataSamples(components coherentMarketDataSoakComponents, observed time.Time, phase string) ([]coherentMarketDataCoherentSample, bool) {
	samples := make([]coherentMarketDataCoherentSample, 0, 3)
	eligible := true
	for _, symbol := range []string{"BTCUSDT", "ETHUSDT", "ETHBTC"} {
		sample := captureCoherentMarketDataSample(components, symbol, observed, phase)
		if sample.Outcome != "success" {
			eligible = false
		}
		samples = append(samples, sample)
	}
	return samples, eligible
}

func captureCoherentMarketDataSample(components coherentMarketDataSoakComponents, symbol string, observed time.Time, phase string) coherentMarketDataCoherentSample {
	instrument := components.instruments[symbol]
	sample := coherentMarketDataCoherentSample{Phase: phase, Pair: symbol, SampledAt: observed, Policy: runtimecore.InitialCoherentMarketDataCoherentPolicy(),
		Members: []coherentMarketDataCoherentMemberEvidence{{Key: runtimecore.MarketKey{Exchange: "binance", Instrument: instrument}},
			{Key: runtimecore.MarketKey{Exchange: "bybit", Instrument: instrument}}}}
	binanceMember, binanceOK := captureCoherentMarketDataMember("binance", instrument, components.binanceCollectors[symbol].Views(),
		components.binanceCollectors[symbol].HealthSnapshot(), components.profiles["binance"])
	bybitMember, bybitOK := captureCoherentMarketDataMember("bybit", instrument, components.bybitCollectors[symbol].Views(),
		components.bybitCollectors[symbol].HealthSnapshot(), components.profiles["bybit"])
	sample.Members[0], sample.Members[1] = binanceMember, bybitMember
	if !binanceOK || !bybitOK {
		sample.CaptureFailed = true
		sample.Outcome, sample.RejectionCode = "rejected", coherentMarketDataRejectCaptureFailure
		return sample
	}
	left, right := *binanceMember.Reference, *bybitMember.Reference
	triggerMonotonic := uint64(components.monotonic())
	if triggerMonotonic < left.ReceiveMonotonicNanos {
		triggerMonotonic = left.ReceiveMonotonicNanos
	}
	if triggerMonotonic < right.ReceiveMonotonicNanos {
		triggerMonotonic = right.ReceiveMonotonicNanos
	}
	sample.Trigger = runtimecore.AsOfTrigger{MonotonicNanos: triggerMonotonic, IngestOrdinal: max(left.IngestOrdinal, right.IngestOrdinal), UTC: observed}
	identity, code := evaluateCoherentMarketDataCoherentSample(sample)
	if code != "" {
		sample.Outcome, sample.RejectionCode = "rejected", code
	} else {
		sample.Outcome, sample.CoherentID = "success", identity
	}
	return sample
}

func captureCoherentMarketDataMember(exchange string, instrument domain.Instrument, provider marketdata.MarketViewProvider,
	health exchangecontracts.CollectorHealthSnapshot, profile recorder.CollectorProfile) (coherentMarketDataCoherentMemberEvidence, bool) {
	member := coherentMarketDataCoherentMemberEvidence{Key: runtimecore.MarketKey{Exchange: exchange, Instrument: instrument}}
	view, err := provider.Book(exchange, instrument)
	if err != nil {
		return member, false
	}
	member.ActiveGeneration = view.Generation()
	clock := exchangecontracts.ClockHealth{ObservedAt: health.ClockObservedAt, Offset: health.ClockOffset,
		Uncertainty: health.ClockUncertainty, Eligible: health.ClockEligible}
	input, err := marketdata.CoherentInput(view, clock, profile.Instance, profile.Region)
	if err != nil {
		return member, false
	}
	reference := runtimecore.ViewReference(input)
	member.Reference, member.ActiveGeneration = &reference, input.ConnectionGeneration
	return member, true
}

func captureCoherentMarketDataHealth(components coherentMarketDataSoakComponents) map[string]exchangecontracts.CollectorHealthSnapshot {
	values := make(map[string]exchangecontracts.CollectorHealthSnapshot, 6)
	for symbol, collector := range components.binanceCollectors {
		values[coherentMarketDataCollectorKey("binance", symbol)] = collector.HealthSnapshot()
	}
	for symbol, collector := range components.bybitCollectors {
		values[coherentMarketDataCollectorKey("bybit", symbol)] = collector.HealthSnapshot()
	}
	return values
}

func allCoherentMarketDataHealthEligible(values map[string]exchangecontracts.CollectorHealthSnapshot) bool {
	if len(values) != 6 {
		return false
	}
	for _, value := range values {
		if !value.Eligible {
			return false
		}
	}
	return true
}

func flushCoherentMarketDataRecorder(exchange, trigger string, monitor coherentMarketDataMonitor) string {
	var current *recorder.DatasetManifest
	var streamRecorder soakFlusher
	if exchange == "binance" {
		current, streamRecorder = &monitor.latest.binance, monitor.components.binanceRecorder
	} else if exchange == "bybit" {
		current, streamRecorder = &monitor.latest.bybit, monitor.components.bybitRecorder
	} else {
		return "recorder_configuration_failed"
	}
	usage := streamRecorder.PendingUsage()
	phase, failureCode := soakFlushLabels(trigger)
	started := time.Now()
	manifest, flushed, err := streamRecorder.FlushReady()
	if err != nil {
		detail := boundedQualificationFailure(failureCode, phase, "flush_ready", err)
		detail.Instrument = exchange
		monitor.evidence.Failures = append(monitor.evidence.Failures, failureCode)
		monitor.evidence.FailureDetails = append(monitor.evidence.FailureDetails, detail)
		appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{Phase: phase, Trigger: trigger, Instrument: exchange,
			Outcome: "failed", Code: failureCode, PendingRaw: usage.RawRecords, PendingCanonical: usage.CanonicalRecords,
			Duration: time.Since(started), Recorder: detail.Recorder, RecorderUsage: &usage})
		return failureCode
	}
	if flushed {
		*current = manifest
	}
	if !appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{Phase: phase, Trigger: trigger, Instrument: exchange,
		Outcome: "passed", ManifestRevision: current.Revision, PendingRaw: usage.RawRecords,
		PendingCanonical: usage.CanonicalRecords, Duration: time.Since(started), RecorderUsage: &usage}) {
		return "event_journal_failed"
	}
	return ""
}

func writeCoherentMarketDataStatusStep(observed time.Time, monitor coherentMarketDataMonitor) string {
	memory, storage := readCoherentMarketDataResources(monitor, observed)
	monitor.evidence.Memory = append(monitor.evidence.Memory, memory)
	monitor.evidence.Storage = append(monitor.evidence.Storage, storage)
	if failures := coherentMarketDataResourceFailures(memory, storage); len(failures) != 0 {
		for _, failure := range failures {
			appendCoherentMarketDataFailure(monitor.evidence, failure, "resource_sample", "capacity_evidence", nil)
		}
		return failures[0]
	}
	if monitor.status == nil {
		return statusWriteFailure
	}
	status := captureCoherentMarketDataSoakStatus(observed, monitor)
	if err := monitor.status(monitor.root, status); err != nil {
		appendCoherentMarketDataFailure(monitor.evidence, statusWriteFailure, "status", "atomic_status_write", err)
		appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{RecordedAt: observed, Phase: "status", Outcome: "failed", Code: statusWriteFailure})
		return statusWriteFailure
	}
	if !appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{RecordedAt: observed, Phase: "status", Outcome: "passed"}) {
		return "event_journal_failed"
	}
	return ""
}

func captureCoherentMarketDataSoakStatus(observed time.Time, monitor coherentMarketDataMonitor) coherentMarketDataSoakStatus {
	failures := append([]string(nil), monitor.evidence.Failures...)
	if failure := coherentMarketDataCollectorFailure(monitor.components); failure != "" {
		failures = append(failures, failure)
	}
	failures = uniqueSortedFailures(failures)
	memory, storage := readCoherentMarketDataResources(monitor, observed)
	if len(monitor.evidence.Memory) != 0 {
		memory = monitor.evidence.Memory[len(monitor.evidence.Memory)-1]
	}
	if len(monitor.evidence.Storage) != 0 {
		storage = monitor.evidence.Storage[len(monitor.evidence.Storage)-1]
	}
	binanceStats, bybitStats := captureCoherentMarketDataCollectorStats(monitor.components)
	journalSequence, journalHash := monitor.journal.Snapshot()
	coherentSequence, coherentHash, coherentSamples := monitor.segments.Snapshot()
	elapsed := time.Duration(0)
	if !monitor.evidence.StartedAt.IsZero() {
		elapsed = observed.Sub(monitor.evidence.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
	}
	return coherentMarketDataSoakStatus{SchemaVersion: coherentMarketDataStatusSchema, SourceCommit: monitor.evidence.SourceCommit, Formal: monitor.evidence.Formal,
		TerminalCause: monitor.evidence.TerminalCause, HarnessStartedAt: monitor.evidence.HarnessStartedAt,
		OfficialStartedAt: monitor.evidence.StartedAt, ObservedAt: observed, Elapsed: elapsed,
		RequiredDuration: monitor.evidence.RequiredDuration, ProvisionalQualified: monitor.evidence.Formal && len(failures) == 0,
		ProvisionalFailures: failures, Health: captureCoherentMarketDataHealth(monitor.components), Pairs: captureCoherentMarketDataPairSnapshots(monitor.pairs),
		BinanceCollectors: binanceStats, BybitCollectors: bybitStats,
		Recorders: map[string]coherentMarketDataRecorderEvidence{"binance": coherentMarketDataRecorderStatus(monitor.components.binanceRecorder, monitor.latest.binance),
			"bybit": coherentMarketDataRecorderStatus(monitor.components.bybitRecorder, monitor.latest.bybit)},
		Memory: memory, Storage: storage, CollectorRunning: cloneCollectorRunning(monitor.evidence.CollectorRunning),
		FailureDetails: append([]qualificationFailure(nil), monitor.evidence.FailureDetails...), CoherentSequence: coherentSequence,
		CoherentHash: coherentHash, CoherentSamples: coherentSamples, EventJournalSequence: journalSequence, EventJournalHash: journalHash}
}

func coherentMarketDataRecorderStatus(value *recorder.Recorder, manifest recorder.DatasetManifest) coherentMarketDataRecorderEvidence {
	return coherentMarketDataRecorderEvidence{ManifestRevision: manifest.Revision, ManifestHash: manifest.Hash, Pending: value.PendingUsage()}
}

func captureCoherentMarketDataCollectorStats(components coherentMarketDataSoakComponents) (map[string]binance.CollectorStatsSnapshot, map[string]bybit.CollectorStats) {
	binanceStats := make(map[string]binance.CollectorStatsSnapshot, 3)
	bybitStats := make(map[string]bybit.CollectorStats, 3)
	for symbol, collector := range components.binanceCollectors {
		binanceStats[symbol] = collector.Stats()
	}
	for symbol, collector := range components.bybitCollectors {
		bybitStats[symbol] = collector.Stats()
	}
	return binanceStats, bybitStats
}

func newCoherentMarketDataPairTrackers() map[string]*coherentMarketDataPairTracker {
	return map[string]*coherentMarketDataPairTracker{"BTCUSDT": {}, "ETHUSDT": {}, "ETHBTC": {}}
}

func captureCoherentMarketDataPairSnapshots(trackers map[string]*coherentMarketDataPairTracker) map[string]coherentMarketDataPairSnapshot {
	values := make(map[string]coherentMarketDataPairSnapshot, len(trackers))
	for pair, tracker := range trackers {
		values[pair] = tracker.Snapshot()
	}
	return values
}

func freezeCoherentMarketDataOfficialEnd(ended time.Time, monitor coherentMarketDataMonitor) string {
	monitor.evidence.EndedAt = ended
	monitor.evidence.Health = captureCoherentMarketDataHealth(monitor.components)
	monitor.evidence.Pairs = captureCoherentMarketDataPairSnapshots(monitor.pairs)
	for pair, snapshot := range monitor.evidence.Pairs {
		if !snapshot.DegradedSince.IsZero() {
			appendCoherentMarketDataFailure(monitor.evidence, "coherent_degradation_unresolved", "official_end", pair, nil)
		}
	}
	if !appendCoherentMarketDataEvent(monitor.journal, monitor.evidence, qualificationEvent{RecordedAt: ended, Phase: "official_end_state_frozen", Outcome: "passed"}) {
		return "event_journal_failed"
	}
	if containsFailure(monitor.evidence.Failures, "coherent_degradation_unresolved") {
		return "coherent_degradation_unresolved"
	}
	return ""
}

func collectCoherentMarketDataCollectorResults(results chan coherentMarketDataCollectorResult, evidence *coherentMarketDataSoakEvidence, journal *qualificationJournal) {
	close(results)
	for result := range results {
		evidence.CollectorRunning[coherentMarketDataCollectorKey(result.exchange, result.instrument)] = false
		if !normalCollectorCancellation(result.err) {
			recordCoherentMarketDataCollectorFailure(result, evidence, journal)
		}
	}
}

func normalCollectorCancellation(err error) bool {
	return err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func recordCoherentMarketDataCollectorFailure(result coherentMarketDataCollectorResult, evidence *coherentMarketDataSoakEvidence, journal *qualificationJournal) {
	key := coherentMarketDataCollectorKey(result.exchange, result.instrument)
	evidence.CollectorRunning[key] = false
	cause := "unexpected_clean_exit"
	if result.err != nil {
		cause = "collector_terminal_error"
	}
	detail := boundedQualificationFailure("collector_failed", "collector_terminal", cause, result.err)
	detail.Instrument = key
	if !containsFailure(evidence.Failures, detail.Code) {
		evidence.Failures = append(evidence.Failures, detail.Code)
	}
	evidence.FailureDetails = append(evidence.FailureDetails, detail)
	appendCoherentMarketDataEvent(journal, evidence, qualificationEvent{Phase: "collector_terminal", Instrument: key, Outcome: "failed", Code: detail.Code, Recorder: detail.Recorder})
}

func finishCoherentMarketDataSoak(t *testing.T, monitor coherentMarketDataMonitor, formal bool) {
	t.Helper()
	evidence := monitor.evidence
	if evidence.EndedAt.IsZero() {
		evidence.EndedAt = time.Now().UTC()
	}
	if !evidence.StartedAt.IsZero() {
		evidence.ActualDuration = evidence.EndedAt.Sub(evidence.StartedAt)
	}
	finishCoherentMarketDataRecorder("binance", monitor.components.binanceRecorder, &monitor.latest.binance, evidence, monitor.journal)
	finishCoherentMarketDataRecorder("bybit", monitor.components.bybitRecorder, &monitor.latest.bybit, evidence, monitor.journal)
	if err := monitor.segments.Flush(); err != nil {
		appendCoherentMarketDataFailure(evidence, "coherent_segment_failed", "final_coherent_flush", "atomic_segment", err)
	}
	coherentSequence, coherentHash, _ := monitor.segments.Snapshot()
	evidence.CoherentSegments.Sequence, evidence.CoherentSegments.TerminalHash = coherentSequence, coherentHash
	if coherentSequence == 0 {
		appendCoherentMarketDataFailure(evidence, "coherent_segment_empty", "coherent_verification", "empty", nil)
	} else if _, err := verifyCoherentMarketDataCoherentSegments(filepath.Join(monitor.root, "coherent"), evidence.SourceCommit, coherentSequence, coherentHash); err != nil {
		appendCoherentMarketDataFailure(evidence, "coherent_segment_verification_failed", "coherent_verification", "checksum_hash_replay", err)
	}
	completeCoherentMarketDataEvidence(monitor)
	finalizeCoherentMarketDataJournal(monitor)
	evidence.Failures = uniqueSortedFailures(evidence.Failures)
	evidence.HarnessPassed = len(evidence.Failures) == 0 && (!formal || evidence.ActualDuration >= coherentMarketDataFormalSoakDuration)
	evidence.Qualified = formal && evidence.HarnessPassed
	if evidence.Qualified {
		evidence.TerminalCause = "qualification_passed"
	} else if evidence.HarnessPassed {
		evidence.TerminalCause = "non_formal_smoke_passed"
	} else {
		evidence.TerminalCause = "qualification_failed"
	}
	status := captureCoherentMarketDataSoakStatus(time.Now().UTC(), monitor)
	status.TerminalCause, status.ProvisionalFailures, status.ProvisionalQualified = evidence.TerminalCause, append([]string(nil), evidence.Failures...), evidence.Qualified
	if err := writeCoherentMarketDataSoakStatus(monitor.root, status); err != nil {
		appendCoherentMarketDataFailure(evidence, statusWriteFailure, "final_status", "atomic_status_write", err)
		evidence.HarnessPassed, evidence.Qualified, evidence.TerminalCause = false, false, "qualification_failed"
	}
	if err := writeAtomicJSON(filepath.Join(monitor.root, "coherent_market_data-soak-evidence.json"), evidence); err != nil {
		t.Fatal("coherent market data terminal evidence failed")
	}
	if !evidence.HarnessPassed {
		t.Fatalf("coherent market data public qualification harness failed: %v", evidence.Failures)
	}
}

func finishCoherentMarketDataRecorder(exchange string, streamRecorder pendingSoakFlusher, latest *recorder.DatasetManifest,
	evidence *coherentMarketDataSoakEvidence, journal *qualificationJournal) {
	manifest, err := finalFlush(streamRecorder, *latest)
	if err != nil {
		appendCoherentMarketDataFailure(evidence, "final_flush_failed", "final_flush", exchange, err)
		appendCoherentMarketDataEvent(journal, evidence, qualificationEvent{Phase: "final_flush", Instrument: exchange, Outcome: "failed", Code: "final_flush_failed"})
		return
	}
	*latest = manifest
	appendCoherentMarketDataEvent(journal, evidence, qualificationEvent{Phase: "final_flush", Instrument: exchange, Outcome: "passed", ManifestRevision: manifest.Revision})
}

func completeCoherentMarketDataEvidence(monitor coherentMarketDataMonitor) {
	evidence := monitor.evidence
	evidence.PositiveLeakTrend = positiveLeakTrendWithLimit(evidence.Memory, coherentMarketDataDeclaredHeapLimit)
	if evidence.PositiveLeakTrend {
		appendCoherentMarketDataFailure(evidence, "positive_heap_trend", "resource_finalization", "heap_trend", nil)
	}
	for _, sample := range evidence.Memory {
		if sample.HeapAlloc > coherentMarketDataDeclaredHeapLimit {
			appendCoherentMarketDataFailure(evidence, "heap_limit_exceeded", "resource_finalization", "heap_limit", nil)
			break
		}
	}
	if len(evidence.Health) == 0 {
		evidence.Health = captureCoherentMarketDataHealth(monitor.components)
	}
	if len(evidence.Pairs) == 0 {
		evidence.Pairs = captureCoherentMarketDataPairSnapshots(monitor.pairs)
	}
	evidence.BinanceCollectors, evidence.BybitCollectors = captureCoherentMarketDataCollectorStats(monitor.components)
	if failure := coherentMarketDataCollectorFailure(monitor.components); failure != "" {
		appendCoherentMarketDataFailure(evidence, failure, "collector_finalization", "bounded_stats", nil)
	}
	evidence.Recorders = map[string]coherentMarketDataRecorderEvidence{"binance": coherentMarketDataRecorderStatus(monitor.components.binanceRecorder, monitor.latest.binance),
		"bybit": coherentMarketDataRecorderStatus(monitor.components.bybitRecorder, monitor.latest.bybit)}
	completeCoherentMarketDataTierA(monitor)
}

func completeCoherentMarketDataTierA(monitor coherentMarketDataMonitor) {
	evidence := monitor.evidence
	roots := map[string]string{"binance": filepath.Join(monitor.root, "binance"), "bybit": filepath.Join(monitor.root, "bybit")}
	manifests := []recorder.DatasetManifest{monitor.latest.binance, monitor.latest.bybit}
	for _, manifest := range manifests {
		root := roots[manifest.Exchange]
		if manifest.Revision == 0 || manifest.RawRecordCount == 0 || manifest.CanonicalCount == 0 || !manifest.Complete || len(manifest.Gaps) != 0 {
			appendCoherentMarketDataFailure(evidence, "child_dataset_incomplete", "tier_a_finalization", manifest.Exchange, nil)
			continue
		}
		if err := recorder.VerifyManifestChain(root, manifest); err != nil {
			appendCoherentMarketDataFailure(evidence, "child_manifest_chain_failed", "tier_a_finalization", manifest.Exchange, err)
		}
		if _, err := recorder.VerifyDataset(root, manifest); err != nil {
			appendCoherentMarketDataFailure(evidence, "child_dataset_replay_failed", "tier_a_finalization", manifest.Exchange, err)
		}
	}
	tierA, err := recorder.BuildTierAManifest("coherent_market_data-formal-tier-a", time.Now().UTC(), roots, manifests)
	if err != nil {
		appendCoherentMarketDataFailure(evidence, "tier_a_finalization_failed", "tier_a_finalization", "aggregate", err)
		return
	}
	path, err := recorder.WriteTierAManifest(filepath.Join(monitor.root, "qualification"), tierA)
	if err != nil {
		appendCoherentMarketDataFailure(evidence, "tier_a_retention_failed", "tier_a_finalization", "atomic_manifest", err)
		return
	}
	retained, err := recorder.ReadTierAManifest(path)
	if err != nil || retained.Hash != tierA.Hash || retained.QualityTier != "A" || !retained.Complete || retained.HiddenGapCount != 0 || len(retained.Members) != 2 {
		appendCoherentMarketDataFailure(evidence, "tier_a_verification_failed", "tier_a_finalization", "retained_manifest", err)
		return
	}
	evidence.TierA = coherentMarketDataTierAEvidence{Path: filepath.Join("qualification", filepath.Base(path)), Hash: retained.Hash, Complete: retained.Complete}
}

func finalizeCoherentMarketDataJournal(monitor coherentMarketDataMonitor) {
	evidence := monitor.evidence
	outcome := "passed"
	if len(evidence.Failures) != 0 || (evidence.Formal && evidence.ActualDuration < coherentMarketDataFormalSoakDuration) {
		outcome = "failed"
	}
	appendCoherentMarketDataEvent(monitor.journal, evidence, qualificationEvent{RecordedAt: evidence.EndedAt, Phase: "terminal", Outcome: outcome})
	if err := monitor.journal.Close(); err != nil {
		appendCoherentMarketDataFailure(evidence, "event_journal_close_failed", "terminal", "journal_close", err)
	}
	evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash = monitor.journal.Snapshot()
	if err := verifyNamedQualificationJournal(monitor.journal.path, coherentMarketDataJournalSchema, evidence.SourceCommit,
		evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash); err != nil {
		appendCoherentMarketDataFailure(evidence, "event_journal_verification_failed", "terminal", "hash_chain_verification", err)
	}
}

func coherentMarketDataCollectorFailure(components coherentMarketDataSoakComponents) string {
	for _, collector := range components.binanceCollectors {
		stats := collector.Stats()
		if stats.DiagnosticsDropped != 0 {
			return "diagnostic_loss"
		}
		if stats.DecoderErrors != 0 {
			return "decoder_error"
		}
		if stats.Gaps != 0 {
			return "source_gap"
		}
		if stats.HotPathP99 > 10*time.Millisecond {
			return "hot_path_p99_exceeded"
		}
		if stats.ResyncOver15Seconds != 0 || stats.ResyncP95 > 15*time.Second {
			return "collector_recovery_over_15_seconds"
		}
	}
	for _, collector := range components.bybitCollectors {
		stats := collector.Stats()
		if stats.DiagnosticsDropped != 0 {
			return "diagnostic_loss"
		}
		if stats.DecoderErrors != 0 {
			return "decoder_error"
		}
		if stats.SequenceGaps != 0 || stats.QueueOverflows != 0 {
			return "source_gap"
		}
		if stats.HotPathP99 > 10*time.Millisecond {
			return "hot_path_p99_exceeded"
		}
		if stats.ResyncOver15Seconds != 0 || stats.ResyncP95 > 15*time.Second {
			return "collector_recovery_over_15_seconds"
		}
	}
	return ""
}

func readCoherentMarketDataResources(monitor coherentMarketDataMonitor, observed time.Time) (memorySample, storageSample) {
	if monitor.resources != nil {
		return monitor.resources(observed, monitor.root)
	}
	return readMemory(observed), readStorage(observed, monitor.root)
}

func coherentMarketDataResourceFailures(memory memorySample, storage storageSample) []string {
	var failures []string
	if !memory.ProcStatusAvailable || !memory.OpenFDsAvailable || !storage.StatfsAvailable {
		failures = append(failures, "capacity_evidence_unavailable")
	}
	if memory.HeapAlloc > coherentMarketDataDeclaredHeapLimit {
		failures = append(failures, "heap_limit_exceeded")
	}
	if storage.StatfsAvailable && (storage.AvailableBytes < coherentMarketDataMinimumFreeBytes || storage.AvailableInodes == 0) {
		failures = append(failures, "storage_capacity_failed")
	}
	return failures
}

func writeCoherentMarketDataSoakStatus(root string, status coherentMarketDataSoakStatus) error {
	return writeAtomicJSON(filepath.Join(root, "coherent_market_data-soak-status.json"), status)
}

func appendCoherentMarketDataEvent(journal *qualificationJournal, evidence *coherentMarketDataSoakEvidence, event qualificationEvent) bool {
	if err := journal.Append(event); err != nil {
		appendCoherentMarketDataFailure(evidence, "event_journal_failed", event.Phase, "journal_append", err)
		return false
	}
	return true
}

func appendCoherentMarketDataFailure(evidence *coherentMarketDataSoakEvidence, code, phase, cause string, err error) {
	if evidence == nil {
		return
	}
	if !containsFailure(evidence.Failures, code) {
		evidence.Failures = append(evidence.Failures, code)
	}
	evidence.FailureDetails = append(evidence.FailureDetails, boundedQualificationFailure(code, phase, cause, err))
}

func coherentMarketDataCollectorKey(exchange, instrument string) string {
	return exchange + ":" + instrument
}

func validQualificationLabel(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') &&
			character != '-' && character != '_' && character != '.' && character != ':' {
			return false
		}
	}
	return true
}
