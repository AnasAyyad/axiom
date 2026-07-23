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
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

const (
	b1FormalSoakDuration = 72 * time.Hour
	b1FormalFlushEvery   = 5 * time.Minute
	b1FormalSampleEvery  = 5 * time.Minute
)

func TestB1Continuous72HourPublicSoak(t *testing.T) {
	if os.Getenv("AXIOM_B1_SOAK") != "1" {
		t.Skip("set AXIOM_B1_SOAK=1 and AXIOM_B1_SOAK_OUTPUT to run the formal 72-hour gate")
	}
	root := os.Getenv("AXIOM_B1_SOAK_OUTPUT")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		t.Fatal("AXIOM_B1_SOAK_OUTPUT must be a dedicated absolute directory")
	}
	runB1Soak(t, root, b1FormalSoakDuration, b1FormalFlushEvery, b1FormalSampleEvery, true)
}

func TestB1PublicSoakHarnessSmoke(t *testing.T) {
	if os.Getenv("AXIOM_B1_SOAK_SMOKE") != "1" {
		t.Skip("set AXIOM_B1_SOAK_SMOKE=1 to exercise the Bybit public qualification harness")
	}
	runB1Soak(t, t.TempDir(), 20*time.Second, 5*time.Second, 2*time.Second, false)
}

type b1SoakEvidence struct {
	SchemaVersion       string                          `json:"schema_version"`
	SourceCommit        string                          `json:"source_commit"`
	Formal              bool                            `json:"formal"`
	Qualified           bool                            `json:"qualified"`
	StartedAt           time.Time                       `json:"started_at"`
	EndedAt             time.Time                       `json:"ended_at"`
	RequiredDuration    time.Duration                   `json:"required_duration_nanos"`
	ActualDuration      time.Duration                   `json:"actual_duration_nanos"`
	EndpointSet         string                          `json:"endpoint_set"`
	Instruments         []string                        `json:"instruments"`
	Streams             []string                        `json:"streams"`
	SnapshotDepth       int                             `json:"snapshot_depth"`
	QueueCapacity       int                             `json:"queue_capacity"`
	FlushEvery          time.Duration                   `json:"flush_every_nanos"`
	HeapLimitBytes      uint64                          `json:"heap_limit_bytes"`
	Memory              []memorySample                  `json:"memory_samples"`
	Storage             []storageSample                 `json:"storage_samples"`
	PositiveLeakTrend   bool                            `json:"positive_leak_trend"`
	Incidents           []b1IncidentSample              `json:"incidents"`
	Collectors          map[string]bybit.CollectorStats `json:"collectors"`
	FinalBooks          map[string]bookSample           `json:"final_books"`
	ManifestRevision    uint64                          `json:"manifest_revision"`
	ManifestHash        string                          `json:"manifest_hash"`
	ManifestGapCount    int                             `json:"manifest_gap_count"`
	DatasetVerification recorder.DatasetVerification    `json:"dataset_verification"`
	Failures            []string                        `json:"failures"`
	FailureDetails      []qualificationFailure          `json:"failure_details,omitempty"`
	EventJournal        qualificationJournalEvidence    `json:"event_journal"`
	root                string                          `json:"-"`
}

type b1IncidentSample struct {
	ObservedAt   time.Time `json:"observed_at"`
	Instrument   string    `json:"instrument"`
	Reconnects   uint64    `json:"reconnects"`
	Snapshots    uint64    `json:"snapshots"`
	SequenceGaps uint64    `json:"sequence_gaps"`
}

type b1ProvisionalSLO struct {
	HotPathP99WithinTarget bool          `json:"hot_path_p99_within_target"`
	ResyncP95WithinTarget  bool          `json:"resync_p95_within_target"`
	ResyncSamples          uint64        `json:"resync_samples"`
	ResyncOver15Seconds    uint64        `json:"resync_over_15_seconds"`
	ResyncP95              time.Duration `json:"resync_p95_nanos"`
	ResyncMax              time.Duration `json:"resync_max_nanos"`
	BookEligible           bool          `json:"book_eligible"`
}

type b1SoakStatus struct {
	SchemaVersion        string                          `json:"schema_version"`
	SourceCommit         string                          `json:"source_commit"`
	Formal               bool                            `json:"formal"`
	StartedAt            time.Time                       `json:"started_at"`
	ObservedAt           time.Time                       `json:"observed_at"`
	Elapsed              time.Duration                   `json:"elapsed_nanos"`
	RequiredDuration     time.Duration                   `json:"required_duration_nanos"`
	ProvisionalQualified bool                            `json:"provisional_qualified"`
	ProvisionalFailures  []string                        `json:"provisional_failures"`
	ProvisionalSLOs      map[string]b1ProvisionalSLO     `json:"provisional_slos"`
	Collectors           map[string]bybit.CollectorStats `json:"collectors"`
	Memory               memorySample                    `json:"memory"`
	Storage              storageSample                   `json:"storage"`
	Books                map[string]bookSample           `json:"books"`
	ManifestRevision     uint64                          `json:"manifest_revision"`
	FailureDetails       []qualificationFailure          `json:"failure_details,omitempty"`
	EventJournalSequence uint64                          `json:"event_journal_sequence"`
	EventJournalHash     string                          `json:"event_journal_hash,omitempty"`
}

type b1SoakComponents struct {
	client     *bybit.PublicClient
	recorder   *recorder.Recorder
	collectors map[string]*bybit.InstrumentCollector
}

type b1CollectorResult struct {
	instrument string
	err        error
}

func runB1Soak(t *testing.T, root string, duration, flushEvery, sampleEvery time.Duration, formal bool) {
	t.Helper()
	sourceCommit, err := b1QualificationSourceCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareEmptyRoot(root); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	evidence := newB1SoakEvidence(started, flushEvery, formal, sourceCommit, root)
	journal, err := newNamedQualificationJournal(root, "b1-soak-events.jsonl",
		b1QualificationJournalSchema, "B1_EVENT", sourceCommit, started)
	if err != nil {
		t.Fatal("B1 qualification event journal could not be created")
	}
	if !appendB1QualificationEvent(journal, &evidence,
		qualificationEvent{RecordedAt: started, Phase: "preflight", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("B1 qualification event journal preflight failed")
	}
	components := newB1SoakComponents(t, root)
	if !appendB1QualificationEvent(journal, &evidence,
		qualificationEvent{Phase: "startup", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("B1 qualification startup evidence failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	results, group := startB1Collectors(ctx, components.collectors)
	var latest recorder.DatasetManifest
	if failure := monitorB1Soak(ctx, root, components, flushEvery, sampleEvery,
		&latest, &evidence, journal); failure != "" {
		evidence.Failures = append(evidence.Failures, failure)
		cancel()
	}
	group.Wait()
	collectB1Errors(results, &evidence)
	manifest := finishB1Recorder(components.recorder, latest, &evidence, journal)
	finishB1Soak(t, root, sourceCommit, started, formal, components, manifest, &evidence, journal)
}

func finishB1Recorder(
	streamRecorder pendingSoakFlusher,
	latest recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) recorder.DatasetManifest {
	manifest, err := finalFlush(streamRecorder, latest)
	if err == nil {
		appendB1QualificationEvent(journal, evidence, qualificationEvent{
			Phase: "final_flush", Outcome: "passed", ManifestRevision: manifest.Revision})
		return manifest
	}
	detail := boundedQualificationFailure("final_flush_failed", "final_flush", "flush", err)
	evidence.Failures = append(evidence.Failures, detail.Code)
	evidence.FailureDetails = append(evidence.FailureDetails, detail)
	appendB1QualificationEvent(journal, evidence, qualificationEvent{
		Phase: "final_flush", Outcome: "failed", Code: detail.Code, Recorder: detail.Recorder})
	return manifest
}

func b1QualificationSourceCommit() (string, error) {
	commit := os.Getenv("AXIOM_B1_SOURCE_COMMIT")
	if !validGitCommit(commit) {
		return "", errors.New("AXIOM_B1_SOURCE_COMMIT must be the exact 40-character source commit")
	}
	return commit, nil
}

func newB1SoakEvidence(started time.Time, flushEvery time.Duration, formal bool, sourceCommit, root string) b1SoakEvidence {
	return b1SoakEvidence{SchemaVersion: "axiom.b1-soak.v1", SourceCommit: sourceCommit,
		Formal: formal, StartedAt: started, RequiredDuration: b1FormalSoakDuration,
		EndpointSet: "bybit-public-v1", Instruments: []string{"BTCUSDT", "ETHUSDT"},
		Streams:       []string{"orderbook.1000", "publicTrade", "tickers", "kline.15", "kline.60", "kline.240"},
		SnapshotDepth: 1000, QueueCapacity: 8192, FlushEvery: flushEvery,
		HeapLimitBytes: declaredHeapLimit, Collectors: make(map[string]bybit.CollectorStats),
		FinalBooks:   make(map[string]bookSample),
		EventJournal: qualificationJournalEvidence{Path: "b1-soak-events.jsonl"}, root: root}
}

func newB1SoakComponents(t *testing.T, root string) b1SoakComponents {
	t.Helper()
	clock := &domain.SystemClock{}
	client, err := bybit.NewPublicClient("bybit-public-v1", clock)
	if err != nil {
		t.Fatal(err)
	}
	streamRecorder, err := recorder.New(root, "b1-public-soak", "b1-public-soak", "bybit",
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
		collector, collectorErr := bybit.NewInstrumentCollector(
			bybit.DefaultCollectorConfig(instrument), client, sink, clock)
		if collectorErr != nil {
			t.Fatal(collectorErr)
		}
		collectors[instrument.Symbol()] = collector
	}
	return b1SoakComponents{client: client, recorder: streamRecorder, collectors: collectors}
}

func startB1Collectors(
	ctx context.Context,
	collectors map[string]*bybit.InstrumentCollector,
) (chan b1CollectorResult, *sync.WaitGroup) {
	results := make(chan b1CollectorResult, len(collectors))
	group := &sync.WaitGroup{}
	for symbol, collector := range collectors {
		group.Add(1)
		go func(instrument string, current *bybit.InstrumentCollector) {
			defer group.Done()
			results <- b1CollectorResult{instrument: instrument, err: current.Run(ctx)}
		}(symbol, collector)
	}
	return results, group
}

func collectB1Errors(results chan b1CollectorResult, evidence *b1SoakEvidence) {
	close(results)
	for result := range results {
		if result.err == nil || errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
			continue
		}
		detail := boundedQualificationFailure("collector_failed", "collector_terminal",
			"collector_terminal_error", result.err)
		detail.Instrument = result.instrument
		evidence.Failures = append(evidence.Failures, detail.Code)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
	}
}

func monitorB1Soak(
	ctx context.Context,
	root string,
	components b1SoakComponents,
	flushEvery, sampleEvery time.Duration,
	latest *recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) string {
	if err := writeB1SoakStatus(root, captureB1SoakStatus(time.Now().UTC(), components, *latest, evidence, journal)); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "initial_status", "atomic_status_write", err))
		return statusWriteFailure
	}
	if !appendB1QualificationEvent(journal, evidence, qualificationEvent{Phase: "initial_status", Outcome: "passed"}) {
		return "event_journal_failed"
	}
	flushTicker, sampleTicker := time.NewTicker(flushEvery), time.NewTicker(sampleEvery)
	defer flushTicker.Stop()
	defer sampleTicker.Stop()
	previous := make(map[string]bybit.CollectorStats, len(components.collectors))
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-flushTicker.C:
			if failure := flushB1SoakStep(root, components, latest, evidence, journal); failure != "" {
				return failure
			}
		case observed := <-sampleTicker.C:
			if failure := sampleB1SoakStep(observed.UTC(), root, components, previous,
				*latest, evidence, journal); failure != "" {
				return failure
			}
		}
	}
}

func flushB1SoakStep(
	root string,
	components b1SoakComponents,
	latest *recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) string {
	raw, canonical := components.recorder.PendingCounts()
	started := time.Now()
	manifest, err := components.recorder.Flush()
	elapsed := time.Since(started)
	if err != nil {
		detail := boundedQualificationFailure(periodicFlushFailure, "recorder_flush", "flush", err)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendB1QualificationEvent(journal, evidence, qualificationEvent{Phase: "recorder_flush",
			Outcome: "failed", Code: periodicFlushFailure, PendingRaw: raw,
			PendingCanonical: canonical, Duration: elapsed, Recorder: detail.Recorder})
		return periodicFlushFailure
	}
	*latest = manifest
	if !appendB1QualificationEvent(journal, evidence, qualificationEvent{Phase: "recorder_flush",
		Outcome: "passed", ManifestRevision: manifest.Revision, PendingRaw: raw,
		PendingCanonical: canonical, Duration: elapsed}) {
		return "event_journal_failed"
	}
	status := captureB1SoakStatus(time.Now().UTC(), components, manifest, evidence, journal)
	if err = writeB1SoakStatus(root, status); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "periodic_status", "atomic_status_write", err))
		appendB1QualificationEvent(journal, evidence, qualificationEvent{Phase: "periodic_status",
			Outcome: "failed", Code: statusWriteFailure, ManifestRevision: manifest.Revision})
		return statusWriteFailure
	}
	if !appendB1QualificationEvent(journal, evidence, qualificationEvent{RecordedAt: status.ObservedAt,
		Phase: "periodic_status", Outcome: "passed", ManifestRevision: manifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func sampleB1SoakStep(
	observed time.Time,
	root string,
	components b1SoakComponents,
	previous map[string]bybit.CollectorStats,
	manifest recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) string {
	evidence.Memory = append(evidence.Memory, readMemory(observed))
	evidence.Storage = append(evidence.Storage, readStorage(observed, root))
	for symbol, collector := range components.collectors {
		current, prior := collector.Stats(), previous[symbol]
		if current.Reconnects != prior.Reconnects || current.Snapshots != prior.Snapshots ||
			current.SequenceGaps != prior.SequenceGaps {
			evidence.Incidents = append(evidence.Incidents, b1IncidentSample{
				ObservedAt: observed, Instrument: symbol, Reconnects: current.Reconnects,
				Snapshots: current.Snapshots, SequenceGaps: current.SequenceGaps})
		}
		previous[symbol] = current
	}
	if err := writeB1SoakStatus(root,
		captureB1SoakStatus(observed, components, manifest, evidence, journal)); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "sample_status", "atomic_status_write", err))
		appendB1QualificationEvent(journal, evidence, qualificationEvent{RecordedAt: observed,
			Phase: "sample_status", Outcome: "failed", Code: statusWriteFailure,
			ManifestRevision: manifest.Revision})
		return statusWriteFailure
	}
	if !appendB1QualificationEvent(journal, evidence, qualificationEvent{
		RecordedAt: observed, Phase: "sample_status", Outcome: "passed",
		ManifestRevision: manifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func captureB1SoakStatus(
	observed time.Time,
	components b1SoakComponents,
	manifest recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) b1SoakStatus {
	failures, collectors, slos, books := captureB1Collectors(components, evidence.Failures)
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
	return b1SoakStatus{SchemaVersion: "axiom.b1-soak-status.v1", SourceCommit: evidence.SourceCommit,
		Formal: evidence.Formal, StartedAt: evidence.StartedAt, ObservedAt: observed, Elapsed: elapsed,
		RequiredDuration: evidence.RequiredDuration, ProvisionalQualified: len(failures) == 0,
		ProvisionalFailures: failures, ProvisionalSLOs: slos, Collectors: collectors,
		Memory: memory, Storage: storage, Books: books, ManifestRevision: manifest.Revision,
		FailureDetails:       append([]qualificationFailure(nil), evidence.FailureDetails...),
		EventJournalSequence: sequence, EventJournalHash: hash}
}

func captureB1Collectors(
	components b1SoakComponents,
	priorFailures []string,
) ([]string, map[string]bybit.CollectorStats, map[string]b1ProvisionalSLO, map[string]bookSample) {
	failures := append([]string(nil), priorFailures...)
	collectors := make(map[string]bybit.CollectorStats, len(components.collectors))
	slos := make(map[string]b1ProvisionalSLO, len(components.collectors))
	books := make(map[string]bookSample, len(components.collectors))
	for symbol, collector := range components.collectors {
		stats := collector.Stats()
		book := currentB1BookSample(symbol, components.client, collector)
		collectors[symbol], books[symbol] = stats, book
		slo := b1ProvisionalSLO{HotPathP99WithinTarget: stats.HotPathP99 <= 10*time.Millisecond,
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

func currentB1BookSample(symbol string, client *bybit.PublicClient, collector *bybit.InstrumentCollector) bookSample {
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
	return bookSample{Health: string(view.Health()), Generation: view.Generation(), Sequence: view.Sequence(),
		Version: view.Version(), Eligible: view.Eligible(client.MonotonicOffset(), 5*time.Second)}
}

func finishB1Soak(
	t *testing.T,
	root, sourceCommit string,
	started time.Time,
	formal bool,
	components b1SoakComponents,
	manifest recorder.DatasetManifest,
	evidence *b1SoakEvidence,
	journal *qualificationJournal,
) {
	t.Helper()
	evidence.EndedAt, evidence.ActualDuration = time.Now().UTC(), time.Since(started)
	completeB1Evidence(root, components, manifest, evidence)
	outcome := "passed"
	if len(evidence.Failures) != 0 || (formal && evidence.ActualDuration < b1FormalSoakDuration) {
		outcome = "failed"
	}
	appendB1QualificationEvent(journal, evidence, qualificationEvent{
		RecordedAt: evidence.EndedAt, Phase: "terminal", Outcome: outcome, ManifestRevision: manifest.Revision})
	if err := journal.Close(); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_close_failed")
	}
	evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash = journal.Snapshot()
	if err := verifyNamedQualificationJournal(journal.path, b1QualificationJournalSchema, sourceCommit,
		evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_verification_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_verification_failed", "terminal",
				"hash_chain_verification", err))
	}
	evidence.Failures = uniqueSortedFailures(evidence.Failures)
	evidence.Qualified = len(evidence.Failures) == 0 && (!formal || evidence.ActualDuration >= b1FormalSoakDuration)
	status := captureB1SoakStatus(time.Now().UTC(), components, manifest, evidence, journal)
	status.ProvisionalFailures = append([]string(nil), evidence.Failures...)
	status.ProvisionalQualified = len(status.ProvisionalFailures) == 0
	if err := writeB1SoakStatus(root, status); err != nil {
		evidence.Failures = append(evidence.Failures, statusWriteFailure)
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "final_status", "atomic_status_write", err))
		evidence.Qualified = false
	}
	if err := writeAtomicJSON(filepath.Join(root, "b1-soak-evidence.json"), evidence); err != nil {
		t.Fatal(err)
	}
	if !evidence.Qualified {
		t.Fatalf("B1 public soak did not qualify: %v", evidence.Failures)
	}
}

func completeB1Evidence(
	root string,
	components b1SoakComponents,
	manifest recorder.DatasetManifest,
	evidence *b1SoakEvidence,
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
		book := currentB1BookSample(symbol, components.client, collector)
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

func writeB1SoakStatus(root string, status b1SoakStatus) error {
	return writeAtomicJSON(filepath.Join(root, "b1-soak-status.json"), status)
}

func appendB1QualificationEvent(
	journal *qualificationJournal,
	evidence *b1SoakEvidence,
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
