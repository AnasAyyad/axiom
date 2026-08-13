package qualification

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/bybit"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

const (
	exchangeExpansionFormalSoakDuration = 72 * time.Hour
	exchangeExpansionFormalFlushEvery   = 5 * time.Minute
	exchangeExpansionFormalSampleEvery  = 5 * time.Minute
)

func TestExchangeExpansionContinuous72HourPublicSoak(t *testing.T) {
	if os.Getenv("AXIOM_EXCHANGE_EXPANSION_SOAK") != "1" {
		t.Skip("set AXIOM_EXCHANGE_EXPANSION_SOAK=1 and AXIOM_EXCHANGE_EXPANSION_SOAK_OUTPUT to run the formal 72-hour gate")
	}
	root := os.Getenv("AXIOM_EXCHANGE_EXPANSION_SOAK_OUTPUT")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		t.Fatal("AXIOM_EXCHANGE_EXPANSION_SOAK_OUTPUT must be a dedicated absolute directory")
	}
	runExchangeExpansionSoak(t, root, exchangeExpansionFormalSoakDuration, exchangeExpansionFormalFlushEvery, exchangeExpansionFormalSampleEvery, true)
}

func TestExchangeExpansionPublicSoakHarnessSmoke(t *testing.T) {
	if os.Getenv("AXIOM_EXCHANGE_EXPANSION_SOAK_SMOKE") != "1" {
		t.Skip("set AXIOM_EXCHANGE_EXPANSION_SOAK_SMOKE=1 to exercise the Bybit public qualification harness")
	}
	runExchangeExpansionSoak(t, t.TempDir(), 20*time.Second, 5*time.Second, 2*time.Second, false)
}

type exchangeExpansionSoakEvidence struct {
	SchemaVersion       string                            `json:"schema_version"`
	SourceCommit        string                            `json:"source_commit"`
	Formal              bool                              `json:"formal"`
	Qualified           bool                              `json:"qualified"`
	TerminalCause       string                            `json:"terminal_cause,omitempty"`
	StartedAt           time.Time                         `json:"started_at"`
	EndedAt             time.Time                         `json:"ended_at"`
	RequiredDuration    time.Duration                     `json:"required_duration_nanos"`
	ActualDuration      time.Duration                     `json:"actual_duration_nanos"`
	EndpointSet         string                            `json:"endpoint_set"`
	Instruments         []string                          `json:"instruments"`
	Streams             []string                          `json:"streams"`
	SnapshotDepth       int                               `json:"snapshot_depth"`
	QueueCapacity       int                               `json:"queue_capacity"`
	FlushEvery          time.Duration                     `json:"flush_every_nanos"`
	HeapLimitBytes      uint64                            `json:"heap_limit_bytes"`
	Memory              []memorySample                    `json:"memory_samples"`
	Storage             []storageSample                   `json:"storage_samples"`
	PositiveLeakTrend   bool                              `json:"positive_leak_trend"`
	Incidents           []exchangeExpansionIncidentSample `json:"incidents"`
	Collectors          map[string]bybit.CollectorStats   `json:"collectors"`
	FinalBooks          map[string]bookSample             `json:"final_books"`
	ManifestRevision    uint64                            `json:"manifest_revision"`
	ManifestHash        string                            `json:"manifest_hash"`
	ManifestGapCount    int                               `json:"manifest_gap_count"`
	DatasetVerification recorder.DatasetVerification      `json:"dataset_verification"`
	Failures            []string                          `json:"failures"`
	Recorder            recorder.PendingUsage             `json:"recorder"`
	CollectorRunning    map[string]bool                   `json:"collector_running"`
	FailureDetails      []qualificationFailure            `json:"failure_details,omitempty"`
	EventJournal        qualificationJournalEvidence      `json:"event_journal"`
	root                string                            `json:"-"`
}

type exchangeExpansionIncidentSample struct {
	ObservedAt   time.Time `json:"observed_at"`
	Instrument   string    `json:"instrument"`
	Reconnects   uint64    `json:"reconnects"`
	Snapshots    uint64    `json:"snapshots"`
	SequenceGaps uint64    `json:"sequence_gaps"`
}

type exchangeExpansionProvisionalSLO struct {
	HotPathP99WithinTarget bool          `json:"hot_path_p99_within_target"`
	ResyncP95WithinTarget  bool          `json:"resync_p95_within_target"`
	ResyncSamples          uint64        `json:"resync_samples"`
	ResyncOver15Seconds    uint64        `json:"resync_over_15_seconds"`
	ResyncP95              time.Duration `json:"resync_p95_nanos"`
	ResyncMax              time.Duration `json:"resync_max_nanos"`
	BookEligible           bool          `json:"book_eligible"`
}

type exchangeExpansionSoakStatus struct {
	SchemaVersion        string                                     `json:"schema_version"`
	SourceCommit         string                                     `json:"source_commit"`
	Formal               bool                                       `json:"formal"`
	TerminalCause        string                                     `json:"terminal_cause,omitempty"`
	StartedAt            time.Time                                  `json:"started_at"`
	ObservedAt           time.Time                                  `json:"observed_at"`
	Elapsed              time.Duration                              `json:"elapsed_nanos"`
	RequiredDuration     time.Duration                              `json:"required_duration_nanos"`
	ProvisionalQualified bool                                       `json:"provisional_qualified"`
	ProvisionalFailures  []string                                   `json:"provisional_failures"`
	ProvisionalSLOs      map[string]exchangeExpansionProvisionalSLO `json:"provisional_slos"`
	Collectors           map[string]bybit.CollectorStats            `json:"collectors"`
	Memory               memorySample                               `json:"memory"`
	Storage              storageSample                              `json:"storage"`
	Books                map[string]bookSample                      `json:"books"`
	ManifestRevision     uint64                                     `json:"manifest_revision"`
	Recorder             recorder.PendingUsage                      `json:"recorder"`
	CollectorRunning     map[string]bool                            `json:"collector_running"`
	FailureDetails       []qualificationFailure                     `json:"failure_details,omitempty"`
	EventJournalSequence uint64                                     `json:"event_journal_sequence"`
	EventJournalHash     string                                     `json:"event_journal_hash,omitempty"`
}

type exchangeExpansionSoakComponents struct {
	client     *bybit.PublicClient
	recorder   pendingSoakFlusher
	collectors map[string]*bybit.InstrumentCollector
}

type exchangeExpansionCollectorResult struct {
	instrument string
	err        error
}

func runExchangeExpansionSoak(t *testing.T, root string, duration, flushEvery, sampleEvery time.Duration, formal bool) {
	t.Helper()
	sourceCommit, err := exchangeExpansionQualificationSourceCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareEmptyRoot(root); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	evidence := newExchangeExpansionSoakEvidence(started, flushEvery, formal, sourceCommit, root)
	journal, err := newNamedQualificationJournal(root, "exchange_expansion-soak-events.jsonl",
		exchangeExpansionQualificationJournalSchema, "EXCHANGE_EXPANSION_EVENT", sourceCommit, started)
	if err != nil {
		t.Fatal("exchange expansion qualification event journal could not be created")
	}
	if !appendExchangeExpansionQualificationEvent(journal, &evidence,
		qualificationEvent{RecordedAt: started, Phase: "preflight", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("exchange expansion qualification event journal preflight failed")
	}
	components := newExchangeExpansionSoakComponents(t, root, qualificationLifecycleSink{journal: journal})
	if !appendExchangeExpansionQualificationEvent(journal, &evidence,
		qualificationEvent{Phase: "startup", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("exchange expansion qualification startup evidence failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results, group := startExchangeExpansionCollectors(ctx, components.collectors)
	var latest recorder.DatasetManifest
	officialEnd := time.NewTimer(duration)
	if failure := monitorExchangeExpansionSoakUntil(ctx, officialEnd.C, root, components, results, flushEvery,
		sampleEvery, &latest, &evidence, journal); failure != "" {
		if !containsFailure(evidence.Failures, failure) {
			evidence.Failures = append(evidence.Failures, failure)
		}
		cancel()
	}
	officialEnd.Stop()
	cancel()
	group.Wait()
	collectExchangeExpansionErrors(results, &evidence, journal)
	manifest := finishExchangeExpansionRecorder(components.recorder, latest, &evidence, journal)
	finishExchangeExpansionSoak(t, root, sourceCommit, started, formal, components, manifest, &evidence, journal)
}

func finishExchangeExpansionRecorder(
	streamRecorder pendingSoakFlusher,
	latest recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) recorder.DatasetManifest {
	manifest, err := finalFlush(streamRecorder, latest)
	if err == nil {
		appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
			Phase: "final_flush", Outcome: "passed", ManifestRevision: manifest.Revision})
		return manifest
	}
	detail := boundedQualificationFailure("final_flush_failed", "final_flush", "flush", err)
	evidence.Failures = append(evidence.Failures, detail.Code)
	evidence.FailureDetails = append(evidence.FailureDetails, detail)
	appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
		Phase: "final_flush", Outcome: "failed", Code: detail.Code, Recorder: detail.Recorder})
	return manifest
}

func exchangeExpansionQualificationSourceCommit() (string, error) {
	return qualificationSourceCommitFromEnv("AXIOM_EXCHANGE_EXPANSION_SOURCE_COMMIT")
}

func newExchangeExpansionSoakEvidence(started time.Time, flushEvery time.Duration, formal bool, sourceCommit, root string) exchangeExpansionSoakEvidence {
	return exchangeExpansionSoakEvidence{SchemaVersion: "axiom.exchange_expansion-soak.v3", SourceCommit: sourceCommit,
		Formal: formal, StartedAt: started, RequiredDuration: exchangeExpansionFormalSoakDuration,
		EndpointSet: "bybit-public-v1", Instruments: []string{"BTCUSDT", "ETHUSDT"},
		Streams:       []string{"orderbook.1000", "publicTrade", "tickers", "kline.15", "kline.60", "kline.240"},
		SnapshotDepth: 1000, QueueCapacity: 8192, FlushEvery: flushEvery,
		HeapLimitBytes: declaredHeapLimit, Collectors: make(map[string]bybit.CollectorStats),
		FinalBooks:       make(map[string]bookSample),
		CollectorRunning: map[string]bool{"BTCUSDT": true, "ETHUSDT": true},
		EventJournal:     qualificationJournalEvidence{Path: "exchange_expansion-soak-events.jsonl"}, root: root}
}

func newExchangeExpansionSoakComponents(t *testing.T, root string,
	lifecycleSink exchangecontracts.LifecycleEvidenceSink) exchangeExpansionSoakComponents {
	t.Helper()
	clock := &domain.SystemClock{}
	client, err := bybit.NewPublicClient("bybit-public-v1", clock)
	if err != nil {
		t.Fatal(err)
	}
	streamRecorder, err := recorder.New(root, "exchange_expansion-public-soak", "exchange_expansion-public-soak", "bybit",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := recorder.NewPublicStreamSink(streamRecorder,
		"bybit-public-parser.v1", "bybit-public-normalizer.v1")
	if err != nil {
		t.Fatal(err)
	}
	instruments := soakInstruments(t)
	collectors := make(map[string]*bybit.InstrumentCollector, len(instruments))
	for _, instrument := range instruments {
		config := bybit.DefaultCollectorConfig(instrument)
		config.LifecycleEvidence = lifecycleSink
		collector, collectorErr := bybit.NewInstrumentCollector(
			config, client, sink, clock)
		if collectorErr != nil {
			t.Fatal(collectorErr)
		}
		collectors[instrument.Symbol()] = collector
	}
	return exchangeExpansionSoakComponents{client: client, recorder: streamRecorder, collectors: collectors}
}

func startExchangeExpansionCollectors(
	ctx context.Context,
	collectors map[string]*bybit.InstrumentCollector,
) (chan exchangeExpansionCollectorResult, *sync.WaitGroup) {
	results := make(chan exchangeExpansionCollectorResult, len(collectors))
	group := &sync.WaitGroup{}
	for symbol, collector := range collectors {
		group.Add(1)
		go func(instrument string, current *bybit.InstrumentCollector) {
			defer group.Done()
			results <- exchangeExpansionCollectorResult{instrument: instrument, err: current.Run(ctx)}
		}(symbol, collector)
	}
	return results, group
}

func collectExchangeExpansionErrors(results chan exchangeExpansionCollectorResult, evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal) {
	close(results)
	for result := range results {
		evidence.CollectorRunning[result.instrument] = false
		if result.err == nil || errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
			continue
		}
		recordExchangeExpansionCollectorFailure(result, evidence, journal)
	}
}

func recordExchangeExpansionCollectorFailure(result exchangeExpansionCollectorResult, evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal) {
	evidence.CollectorRunning[result.instrument] = false
	cause := "unexpected_clean_exit"
	if result.err != nil {
		cause = "collector_terminal_error"
	}
	detail := boundedQualificationFailure("collector_failed", "collector_terminal", cause, result.err)
	detail.Instrument = result.instrument
	if !containsFailure(evidence.Failures, detail.Code) {
		evidence.Failures = append(evidence.Failures, detail.Code)
	}
	evidence.FailureDetails = append(evidence.FailureDetails, detail)
	appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{Phase: "collector_terminal",
		Instrument: result.instrument, Outcome: "failed", Code: detail.Code, Recorder: detail.Recorder})
}

func monitorExchangeExpansionSoak(
	ctx context.Context,
	root string,
	components exchangeExpansionSoakComponents,
	collectorResults <-chan exchangeExpansionCollectorResult,
	flushEvery, sampleEvery time.Duration,
	latest *recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) string {
	return monitorExchangeExpansionSoakUntil(ctx, nil, root, components, collectorResults, flushEvery,
		sampleEvery, latest, evidence, journal)
}

func monitorExchangeExpansionSoakUntil(
	ctx context.Context,
	officialEnd <-chan time.Time,
	root string,
	components exchangeExpansionSoakComponents,
	collectorResults <-chan exchangeExpansionCollectorResult,
	flushEvery, sampleEvery time.Duration,
	latest *recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) string {
	if failure := initializeExchangeExpansionSoakMonitor(root, components, latest, evidence, journal); failure != "" {
		return failure
	}
	flushTicker, sampleTicker := time.NewTicker(flushEvery), time.NewTicker(sampleEvery)
	defer flushTicker.Stop()
	defer sampleTicker.Stop()
	previous := make(map[string]bybit.CollectorStats, len(components.collectors))
	for {
		select {
		case <-ctx.Done():
			return ""
		case ended := <-officialEnd:
			evidence.EndedAt = ended.UTC()
			for symbol, collector := range components.collectors {
				evidence.FinalBooks[symbol] = currentExchangeExpansionBookSample(symbol, components.client, collector)
			}
			if !appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
				RecordedAt: evidence.EndedAt, Phase: "official_end_health_frozen", Outcome: "passed"}) {
				return "event_journal_failed"
			}
			return ""
		case result := <-collectorResults:
			return handleExchangeExpansionCollectorTerminal(ctx, result, root, components, latest, evidence, journal)
		case <-components.recorder.FlushRequired():
			if failure := flushExchangeExpansionSoakStep("capacity", root, components, latest, evidence, journal); failure != "" {
				return failure
			}
		case <-flushTicker.C:
			if failure := flushExchangeExpansionSoakStep("scheduled", root, components, latest, evidence, journal); failure != "" {
				return failure
			}
		case observed := <-sampleTicker.C:
			if failure := sampleExchangeExpansionSoakStep(observed.UTC(), root, components, previous,
				*latest, evidence, journal); failure != "" {
				return failure
			}
		}
	}
}

func initializeExchangeExpansionSoakMonitor(root string, components exchangeExpansionSoakComponents,
	latest *recorder.DatasetManifest, evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal) string {
	status := captureExchangeExpansionSoakStatus(time.Now().UTC(), components, *latest, evidence, journal)
	if err := writeExchangeExpansionSoakStatus(root, status); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "initial_status", "atomic_status_write", err))
		return statusWriteFailure
	}
	if !appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
		Phase: "initial_status", Outcome: "passed"}) {
		return "event_journal_failed"
	}
	return ""
}

func handleExchangeExpansionCollectorTerminal(ctx context.Context, result exchangeExpansionCollectorResult, root string,
	components exchangeExpansionSoakComponents, latest *recorder.DatasetManifest, evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal) string {
	if ctx.Err() != nil && (result.err == nil || errors.Is(result.err, context.Canceled) ||
		errors.Is(result.err, context.DeadlineExceeded)) {
		evidence.CollectorRunning[result.instrument] = false
		return ""
	}
	recordExchangeExpansionCollectorFailure(result, evidence, journal)
	status := captureExchangeExpansionSoakStatus(time.Now().UTC(), components, *latest, evidence, journal)
	if err := writeExchangeExpansionSoakStatus(root, status); err != nil {
		detail := boundedQualificationFailure(statusWriteFailure,
			"collector_terminal_status", "atomic_status_write", err)
		evidence.Failures = append(evidence.Failures, statusWriteFailure)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{Phase: "collector_terminal_status",
			Instrument: result.instrument, Outcome: "failed", Code: statusWriteFailure})
		return statusWriteFailure
	}
	return "collector_failed"
}

func flushExchangeExpansionSoakStep(
	trigger string,
	root string,
	components exchangeExpansionSoakComponents,
	latest *recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) string {
	usage := components.recorder.PendingUsage()
	phase, failureCode := soakFlushLabels(trigger)
	started := time.Now()
	manifest, flushed, err := components.recorder.FlushReady()
	elapsed := time.Since(started)
	if err != nil {
		detail := boundedQualificationFailure(failureCode, phase, "flush_ready", err)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{Phase: phase, Trigger: trigger,
			Outcome: "failed", Code: failureCode, PendingRaw: usage.RawRecords,
			PendingCanonical: usage.CanonicalRecords, Duration: elapsed,
			Recorder: detail.Recorder, RecorderUsage: &usage})
		return failureCode
	}
	if flushed {
		*latest = manifest
	} else {
		manifest = *latest
	}
	if !appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{Phase: phase, Trigger: trigger,
		Outcome: "passed", ManifestRevision: manifest.Revision, PendingRaw: usage.RawRecords,
		PendingCanonical: usage.CanonicalRecords, Duration: elapsed, RecorderUsage: &usage}) {
		return "event_journal_failed"
	}
	status := captureExchangeExpansionSoakStatus(time.Now().UTC(), components, manifest, evidence, journal)
	if err = writeExchangeExpansionSoakStatus(root, status); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "periodic_status", "atomic_status_write", err))
		appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{Phase: "periodic_status",
			Outcome: "failed", Code: statusWriteFailure, ManifestRevision: manifest.Revision})
		return statusWriteFailure
	}
	if !appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: status.ObservedAt,
		Phase: "periodic_status", Outcome: "passed", ManifestRevision: manifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func sampleExchangeExpansionSoakStep(
	observed time.Time,
	root string,
	components exchangeExpansionSoakComponents,
	previous map[string]bybit.CollectorStats,
	manifest recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) string {
	evidence.Memory = append(evidence.Memory, readMemory(observed))
	evidence.Storage = append(evidence.Storage, readStorage(observed, root))
	for symbol, collector := range components.collectors {
		current, prior := collector.Stats(), previous[symbol]
		if current.Reconnects != prior.Reconnects || current.Snapshots != prior.Snapshots ||
			current.SequenceGaps != prior.SequenceGaps {
			evidence.Incidents = append(evidence.Incidents, exchangeExpansionIncidentSample{
				ObservedAt: observed, Instrument: symbol, Reconnects: current.Reconnects,
				Snapshots: current.Snapshots, SequenceGaps: current.SequenceGaps})
		}
		previous[symbol] = current
	}
	if err := writeExchangeExpansionSoakStatus(root,
		captureExchangeExpansionSoakStatus(observed, components, manifest, evidence, journal)); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "sample_status", "atomic_status_write", err))
		appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: observed,
			Phase: "sample_status", Outcome: "failed", Code: statusWriteFailure,
			ManifestRevision: manifest.Revision})
		return statusWriteFailure
	}
	if !appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
		RecordedAt: observed, Phase: "sample_status", Outcome: "passed",
		ManifestRevision: manifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func captureExchangeExpansionSoakStatus(
	observed time.Time,
	components exchangeExpansionSoakComponents,
	manifest recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) exchangeExpansionSoakStatus {
	failures, collectors, slos, books := captureExchangeExpansionCollectors(components, evidence.Failures)
	memory := readMemory(observed)
	if len(evidence.Memory) != 0 {
		memory = evidence.Memory[len(evidence.Memory)-1]
	}
	storage := readStorage(observed, evidence.root)
	if len(evidence.Storage) != 0 {
		storage = evidence.Storage[len(evidence.Storage)-1]
	}
	sequence, hash := journal.Snapshot()
	elapsed := observed.Sub(evidence.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	return exchangeExpansionSoakStatus{SchemaVersion: "axiom.exchange_expansion-soak-status.v3", SourceCommit: evidence.SourceCommit,
		Formal: evidence.Formal, StartedAt: evidence.StartedAt, ObservedAt: observed, Elapsed: elapsed,
		TerminalCause:    evidence.TerminalCause,
		RequiredDuration: evidence.RequiredDuration, ProvisionalQualified: len(failures) == 0,
		ProvisionalFailures: failures, ProvisionalSLOs: slos, Collectors: collectors,
		Memory: memory, Storage: storage, Books: books, Recorder: components.recorder.PendingUsage(),
		CollectorRunning: cloneCollectorRunning(evidence.CollectorRunning), ManifestRevision: manifest.Revision,
		FailureDetails:       append([]qualificationFailure(nil), evidence.FailureDetails...),
		EventJournalSequence: sequence, EventJournalHash: hash}
}

func captureExchangeExpansionCollectors(
	components exchangeExpansionSoakComponents,
	priorFailures []string,
) ([]string, map[string]bybit.CollectorStats, map[string]exchangeExpansionProvisionalSLO, map[string]bookSample) {
	failures := append([]string(nil), priorFailures...)
	collectors := make(map[string]bybit.CollectorStats, len(components.collectors))
	slos := make(map[string]exchangeExpansionProvisionalSLO, len(components.collectors))
	books := make(map[string]bookSample, len(components.collectors))
	for symbol, collector := range components.collectors {
		stats := collector.Stats()
		book := currentExchangeExpansionBookSample(symbol, components.client, collector)
		collectors[symbol], books[symbol] = stats, book
		slo := exchangeExpansionProvisionalSLO{HotPathP99WithinTarget: stats.HotPathP99 <= 10*time.Millisecond,
			ResyncP95WithinTarget: stats.ResyncP95 <= 15*time.Second, ResyncSamples: stats.ResyncSamples,
			ResyncOver15Seconds: stats.ResyncOver15Seconds, ResyncP95: stats.ResyncP95,
			ResyncMax: stats.ResyncMax, BookEligible: book.Eligible}
		slos[symbol] = slo
		if !slo.HotPathP99WithinTarget || !slo.ResyncP95WithinTarget {
			failures = append(failures, symbol+"_slo_failed")
		}
		if !book.Eligible {
			failures = append(failures, symbol+"_ineligible")
		}
		if stats.DiagnosticsDropped != 0 {
			failures = append(failures, symbol+"_diagnostics_dropped")
		}
	}
	return uniqueSortedFailures(failures), collectors, slos, books
}

func currentExchangeExpansionBookSample(symbol string, client *bybit.PublicClient, collector *bybit.InstrumentCollector) bookSample {
	if len(symbol) != 7 || client == nil {
		return bookSample{}
	}
	instrument, err := domain.NewSpotInstrument(domain.AssetSymbol(symbol[:3]), domain.AssetSymbol(symbol[3:]))
	if err != nil {
		return bookSample{}
	}
	view, err := collector.Views().Book("bybit", instrument)
	if err != nil {
		return bookSample{}
	}
	health := collector.HealthSnapshot()
	return bookSample{Health: string(view.Health()), Generation: view.Generation(), Sequence: view.Sequence(),
		Version: view.Version(), BookEligible: health.BookEligible, ClockEligible: health.ClockEligible,
		Eligible: health.Eligible, DegradedSince: health.DegradedSince}
}

func finishExchangeExpansionSoak(
	t *testing.T,
	root, sourceCommit string,
	started time.Time,
	formal bool,
	components exchangeExpansionSoakComponents,
	manifest recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) {
	t.Helper()
	if evidence.EndedAt.IsZero() {
		evidence.EndedAt = time.Now().UTC()
	}
	evidence.ActualDuration = evidence.EndedAt.Sub(started)
	completeExchangeExpansionEvidence(root, components, manifest, evidence)
	evidence.Recorder = components.recorder.PendingUsage()
	finalizeExchangeExpansionJournal(sourceCommit, formal, manifest.Revision, evidence, journal)
	evidence.Failures = uniqueSortedFailures(evidence.Failures)
	evidence.Qualified = len(evidence.Failures) == 0 && (!formal || evidence.ActualDuration >= exchangeExpansionFormalSoakDuration)
	status := captureExchangeExpansionSoakStatus(time.Now().UTC(), components, manifest, evidence, journal)
	status.ProvisionalFailures = append([]string(nil), evidence.Failures...)
	status.ProvisionalQualified = len(status.ProvisionalFailures) == 0
	if err := writeExchangeExpansionSoakStatus(root, status); err != nil {
		evidence.Failures = append(evidence.Failures, statusWriteFailure)
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "final_status", "atomic_status_write", err))
		evidence.Qualified = false
		evidence.TerminalCause = "qualification_failed"
	}
	if err := writeAtomicJSON(filepath.Join(root, "exchange_expansion-soak-evidence.json"), evidence); err != nil {
		t.Fatal(err)
	}
	if !evidence.Qualified {
		t.Fatalf("exchange expansion public soak did not qualify: %v", evidence.Failures)
	}
}

func finalizeExchangeExpansionJournal(
	sourceCommit string,
	formal bool,
	manifestRevision uint64,
	evidence *exchangeExpansionSoakEvidence,
	journal *qualificationJournal,
) {
	outcome := "passed"
	if len(evidence.Failures) != 0 || (formal && evidence.ActualDuration < exchangeExpansionFormalSoakDuration) {
		outcome = "failed"
	}
	appendExchangeExpansionQualificationEvent(journal, evidence, qualificationEvent{
		RecordedAt:       evidence.EndedAt,
		Phase:            "terminal",
		Outcome:          outcome,
		ManifestRevision: manifestRevision,
	})
	if err := journal.Close(); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_close_failed")
	}
	evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash = journal.Snapshot()
	if err := verifyNamedQualificationJournal(journal.path, exchangeExpansionQualificationJournalSchema, sourceCommit,
		evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_verification_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_verification_failed", "terminal",
				"hash_chain_verification", err))
	}
	evidence.TerminalCause = "qualification_passed"
	if len(evidence.Failures) != 0 || (formal && evidence.ActualDuration < exchangeExpansionFormalSoakDuration) {
		evidence.TerminalCause = "qualification_failed"
	}
}

func completeExchangeExpansionEvidence(
	root string,
	components exchangeExpansionSoakComponents,
	manifest recorder.DatasetManifest,
	evidence *exchangeExpansionSoakEvidence,
) {
	evidence.PositiveLeakTrend = positiveLeakTrend(evidence.Memory)
	if evidence.PositiveLeakTrend {
		evidence.Failures = append(evidence.Failures, "positive_heap_trend")
	}
	for _, sample := range evidence.Memory {
		if sample.HeapAlloc > declaredHeapLimit {
			evidence.Failures = append(evidence.Failures, "heap_limit_exceeded")
			break
		}
	}
	for symbol, collector := range components.collectors {
		stats := collector.Stats()
		evidence.Collectors[symbol] = stats
		book, frozen := evidence.FinalBooks[symbol]
		if !frozen {
			book = currentExchangeExpansionBookSample(symbol, components.client, collector)
		}
		evidence.FinalBooks[symbol] = book
		if stats.DiagnosticsDropped != 0 {
			evidence.Failures = append(evidence.Failures, symbol+"_diagnostics_dropped")
		}
		if evidence.Formal && (stats.HotPathP99 > 10*time.Millisecond ||
			stats.ResyncP95 > 15*time.Second || stats.Snapshots == 0) {
			evidence.Failures = append(evidence.Failures, symbol+"_slo_failed")
		}
		if evidence.Formal && !book.Eligible {
			evidence.Failures = append(evidence.Failures, symbol+"_ineligible")
		}
	}
	evidence.ManifestRevision, evidence.ManifestHash = manifest.Revision, manifest.Hash
	evidence.ManifestGapCount = len(manifest.Gaps)
	verification, err := recorder.VerifyDataset(root, manifest)
	if err != nil {
		evidence.Failures = append(evidence.Failures, "dataset_verification_failed")
	} else {
		evidence.DatasetVerification = verification
	}
	sort.Strings(evidence.Failures)
}

func writeExchangeExpansionSoakStatus(root string, status exchangeExpansionSoakStatus) error {
	return writeAtomicJSON(filepath.Join(root, "exchange_expansion-soak-status.json"), status)
}

func appendExchangeExpansionQualificationEvent(
	journal *qualificationJournal,
	evidence *exchangeExpansionSoakEvidence,
	event qualificationEvent,
) bool {
	if err := journal.Append(event); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_failed", event.Phase, "journal_append", err))
		return false
	}
	return true
}
