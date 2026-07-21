package qualification

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/recorder"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/storage/segments"
)

const (
	formalSoakDuration = 72 * time.Hour
	formalFlushEvery   = 5 * time.Minute
	formalSampleEvery  = 5 * time.Minute
	declaredHeapLimit  = 512 * 1024 * 1024
)

func TestA7Continuous72HourPublicSoak(t *testing.T) {
	if os.Getenv("AXIOM_A7_SOAK") != "1" {
		t.Skip("set AXIOM_A7_SOAK=1 and AXIOM_A7_SOAK_OUTPUT to run the formal 72-hour gate")
	}
	root := os.Getenv("AXIOM_A7_SOAK_OUTPUT")
	if !filepath.IsAbs(root) || filepath.Clean(root) == string(filepath.Separator) {
		t.Fatal("AXIOM_A7_SOAK_OUTPUT must be a dedicated absolute directory")
	}
	runA7Soak(t, root, formalSoakDuration, formalFlushEvery, formalSampleEvery, true)
}

func TestA7PublicSoakHarnessSmoke(t *testing.T) {
	if os.Getenv("AXIOM_A7_SOAK_SMOKE") != "1" {
		t.Skip("set AXIOM_A7_SOAK_SMOKE=1 to exercise the public qualification harness")
	}
	runA7Soak(t, t.TempDir(), 20*time.Second, 5*time.Second, 2*time.Second, false)
}

type soakEvidence struct {
	SchemaVersion       string                                    `json:"schema_version"`
	SourceCommit        string                                    `json:"source_commit"`
	Formal              bool                                      `json:"formal"`
	Qualified           bool                                      `json:"qualified"`
	StartedAt           time.Time                                 `json:"started_at"`
	EndedAt             time.Time                                 `json:"ended_at"`
	RequiredDuration    time.Duration                             `json:"required_duration_nanos"`
	ActualDuration      time.Duration                             `json:"actual_duration_nanos"`
	EndpointSet         string                                    `json:"endpoint_set"`
	Instruments         []string                                  `json:"instruments"`
	Streams             []string                                  `json:"streams"`
	SnapshotDepth       uint32                                    `json:"snapshot_depth"`
	QueueCapacity       int                                       `json:"queue_capacity"`
	FlushEvery          time.Duration                             `json:"flush_every_nanos"`
	HeapLimitBytes      uint64                                    `json:"heap_limit_bytes"`
	Memory              []memorySample                            `json:"memory_samples"`
	Storage             []storageSample                           `json:"storage_samples"`
	PositiveLeakTrend   bool                                      `json:"positive_leak_trend"`
	Incidents           []incidentSample                          `json:"incidents"`
	Collectors          map[string]binance.CollectorStatsSnapshot `json:"collectors"`
	FinalBooks          map[string]bookSample                     `json:"final_books"`
	ManifestRevision    uint64                                    `json:"manifest_revision"`
	ManifestHash        string                                    `json:"manifest_hash"`
	ManifestGapCount    int                                       `json:"manifest_gap_count"`
	DatasetVerification recorder.DatasetVerification              `json:"dataset_verification"`
	Failures            []string                                  `json:"failures"`
	FailureDetails      []qualificationFailure                    `json:"failure_details,omitempty"`
	EventJournal        qualificationJournalEvidence              `json:"event_journal"`
	root                string                                    `json:"-"`
}

type qualificationJournalEvidence struct {
	Path         string `json:"path"`
	Sequence     uint64 `json:"sequence"`
	TerminalHash string `json:"terminal_hash"`
}

type memorySample struct {
	ObservedAt          time.Time `json:"observed_at"`
	ProcStatusAvailable bool      `json:"proc_status_available"`
	HeapAlloc           uint64    `json:"heap_alloc_bytes"`
	HeapInUse           uint64    `json:"heap_in_use_bytes"`
	Sys                 uint64    `json:"sys_bytes"`
	RSS                 uint64    `json:"rss_bytes"`
	RSSHighWater        uint64    `json:"rss_high_water_bytes"`
	OpenFDsAvailable    bool      `json:"open_fds_available"`
	OpenFDs             uint64    `json:"open_fds"`
}

type storageSample struct {
	ObservedAt      time.Time `json:"observed_at"`
	StatfsAvailable bool      `json:"statfs_available"`
	AvailableBytes  uint64    `json:"available_bytes"`
	AvailableInodes uint64    `json:"available_inodes"`
	TotalBytes      uint64    `json:"total_bytes"`
}

type incidentSample struct {
	ObservedAt time.Time `json:"observed_at"`
	Instrument string    `json:"instrument"`
	Reconnects uint64    `json:"reconnects"`
	Rebuilds   uint64    `json:"rebuilds"`
	Gaps       uint64    `json:"gaps"`
}

type bookSample struct {
	Health     string `json:"health"`
	Generation uint64 `json:"generation"`
	Sequence   uint64 `json:"sequence"`
	Version    uint64 `json:"version"`
	Eligible   bool   `json:"eligible"`
}

type soakComponents struct {
	client     *binance.PublicClient
	recorder   *recorder.Recorder
	collectors map[string]*binance.InstrumentCollector
}

func runA7Soak(t *testing.T, root string, duration, flushEvery, sampleEvery time.Duration, formal bool) {
	t.Helper()
	sourceCommit, err := qualificationSourceCommit()
	if err != nil {
		t.Fatal(err)
	}
	if err = prepareEmptyRoot(root); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	evidence := newSoakEvidence(started, flushEvery, formal, sourceCommit)
	journal, err := newQualificationJournal(root, sourceCommit, started)
	if err != nil {
		evidence.root = root
		writeEmergencyQualificationEvent(qualificationEvent{RecordedAt: started, Phase: "preflight",
			Outcome: "failed", Code: "event_journal_create_failed"}, "event_journal_create_failed")
		t.Fatal("A7 qualification event journal could not be created")
	}
	if !appendQualificationEvent(journal, &evidence, qualificationEvent{RecordedAt: started,
		Phase: "preflight", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("A7 qualification event journal preflight failed")
	}
	components := newSoakComponents(t, root)
	if !appendQualificationEvent(journal, &evidence, qualificationEvent{Phase: "startup", Outcome: "passed"}) {
		_ = journal.Close()
		t.Fatal("A7 qualification startup evidence failed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	collectorErrors, group := startSoakCollectors(ctx, components.collectors)
	var latestManifest recorder.DatasetManifest
	monitorFailure := monitorSoakFailClosed(ctx, root, components.client, components.recorder,
		components.collectors, flushEvery, sampleEvery, &latestManifest, &evidence, writeSoakStatus, journal)
	if monitorFailure != "" {
		if !containsFailure(evidence.Failures, monitorFailure) {
			evidence.Failures = append(evidence.Failures, monitorFailure)
		}
		cancel()
	}
	group.Wait()
	collectSoakErrors(collectorErrors, &evidence)
	manifest := finishSoakRecorder(components.recorder, latestManifest, &evidence, journal)
	finishA7Soak(t, root, sourceCommit, started, formal, components, manifest, &evidence, journal)
}

func finishSoakRecorder(
	streamRecorder pendingSoakFlusher,
	latest recorder.DatasetManifest,
	evidence *soakEvidence,
	journal *qualificationJournal,
) recorder.DatasetManifest {
	manifest, err := finalFlush(streamRecorder, latest)
	if err != nil {
		evidence.Failures = append(evidence.Failures, "final_flush_failed")
		detail := boundedQualificationFailure("final_flush_failed", "final_flush", "flush", err)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendQualificationEvent(journal, evidence, qualificationEvent{Phase: "final_flush", Outcome: "failed",
			Code: "final_flush_failed", Recorder: detail.Recorder})
		return manifest
	}
	appendQualificationEvent(journal, evidence, qualificationEvent{Phase: "final_flush", Outcome: "passed",
		ManifestRevision: manifest.Revision})
	return manifest
}

func finishA7Soak(
	t *testing.T,
	root string,
	sourceCommit string,
	started time.Time,
	formal bool,
	components soakComponents,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
	journal *qualificationJournal,
) {
	t.Helper()
	evidence.EndedAt, evidence.ActualDuration = time.Now().UTC(), time.Since(started)
	completeSoakEvidence(root, components.client, components.collectors, manifest, evidence)
	terminalOutcome := "passed"
	if len(evidence.Failures) != 0 || (formal && evidence.ActualDuration < formalSoakDuration) {
		terminalOutcome = "failed"
	}
	appendQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: evidence.EndedAt,
		Phase: "terminal", Outcome: terminalOutcome, ManifestRevision: manifest.Revision})
	if err := journal.Close(); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_close_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_close_failed", "terminal", "journal_close", err))
	}
	evidence.EventJournal.Sequence, evidence.EventJournal.TerminalHash = journal.Snapshot()
	if err := verifyQualificationJournal(journal.path, sourceCommit, evidence.EventJournal.Sequence,
		evidence.EventJournal.TerminalHash); err != nil {
		evidence.Failures = append(evidence.Failures, "event_journal_verification_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("event_journal_verification_failed", "terminal", "hash_chain_verification", err))
		writeEmergencyQualificationEvent(qualificationEvent{Phase: "terminal", Outcome: "failed",
			Code: "event_journal_verification_failed"}, "event_journal_verification_failed")
	}
	if err := writeFinalSoakStatus(root, components.client, components.collectors, manifest, evidence, journal); err != nil {
		evidence.Failures = append(evidence.Failures, "status_write_failed")
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure("status_write_failed", "final_status", "atomic_status_write", err))
	}
	evidence.Failures = uniqueSortedFailures(evidence.Failures)
	evidence.Qualified = len(evidence.Failures) == 0 && (!formal || evidence.ActualDuration >= formalSoakDuration)
	if err := writeSoakEvidence(root, *evidence); err != nil {
		t.Fatal(err)
	}
	if !evidence.Qualified {
		t.Fatalf("A7 public soak did not qualify: %v", evidence.Failures)
	}
}

func newSoakComponents(t *testing.T, root string) soakComponents {
	t.Helper()
	clock := &domain.SystemClock{}
	client, err := binance.NewPublicClient("market-data-only-v1", clock)
	if err != nil {
		t.Fatal(err)
	}
	streamRecorder, err := recorder.New(root, "a7-public-soak", "a7-public-soak", "binance",
		&runtimecore.IngestOrdinals{}, func(segments.Manifest) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := recorder.NewBinanceStreamSink(streamRecorder)
	if err != nil {
		t.Fatal(err)
	}
	instruments := soakInstruments(t)
	collectors := make(map[string]*binance.InstrumentCollector, len(instruments))
	for _, instrument := range instruments {
		config := binance.DefaultCollectorConfig(instrument)
		collector, collectorErr := binance.NewInstrumentCollector(config, client, sink, clock)
		if collectorErr != nil {
			t.Fatal(collectorErr)
		}
		collectors[instrument.Symbol()] = collector
	}
	return soakComponents{client: client, recorder: streamRecorder, collectors: collectors}
}

type collectorResult struct {
	instrument string
	err        error
}

func startSoakCollectors(
	ctx context.Context,
	collectors map[string]*binance.InstrumentCollector,
) (chan collectorResult, *sync.WaitGroup) {
	collectorErrors := make(chan collectorResult, len(collectors))
	group := &sync.WaitGroup{}
	for symbol, collector := range collectors {
		group.Add(1)
		go func(instrument string, instrumentCollector *binance.InstrumentCollector) {
			defer group.Done()
			collectorErrors <- collectorResult{instrument: instrument, err: instrumentCollector.Run(ctx)}
		}(symbol, collector)
	}
	return collectorErrors, group
}

func newSoakEvidence(started time.Time, flushEvery time.Duration, formal bool, sourceCommit string) soakEvidence {
	return soakEvidence{SchemaVersion: "axiom.a7-soak.v3", SourceCommit: sourceCommit,
		Formal: formal, StartedAt: started, RequiredDuration: formalSoakDuration,
		EndpointSet: "market-data-only-v1", Instruments: []string{"BTCUSDT", "ETHUSDT"},
		Streams: []string{"depth@100ms", "trade", "kline_4h"}, SnapshotDepth: 5000,
		QueueCapacity: 8192, FlushEvery: flushEvery, HeapLimitBytes: declaredHeapLimit,
		Collectors: make(map[string]binance.CollectorStatsSnapshot), FinalBooks: make(map[string]bookSample),
		EventJournal: qualificationJournalEvidence{Path: "a7-soak-events.jsonl"}}
}

func collectSoakErrors(collectorErrors chan collectorResult, evidence *soakEvidence) {
	close(collectorErrors)
	for result := range collectorErrors {
		if result.err != nil {
			evidence.Failures = append(evidence.Failures, "collector_failed")
			detail := boundedQualificationFailure("collector_failed", "collector_terminal",
				"collector_terminal_error", result.err)
			detail.Instrument = result.instrument
			evidence.FailureDetails = append(evidence.FailureDetails, detail)
		}
	}
}

func completeSoakEvidence(
	root string,
	client *binance.PublicClient,
	collectors map[string]*binance.InstrumentCollector,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
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
	for symbol, collector := range collectors {
		stats := collector.Stats()
		evidence.Collectors[symbol] = stats
		if stats.HotPathP99 > 10*time.Millisecond || stats.ResyncP95 > 15*time.Second || stats.Rebuilds == 0 {
			evidence.Failures = append(evidence.Failures, symbol+"_slo_failed")
		}
		base, _ := domain.ParseAssetSymbol(symbol[:3])
		quote, _ := domain.ParseAssetSymbol(symbol[3:])
		instrument, _ := domain.NewSpotInstrument(base, quote)
		view, err := collector.Views().Book("binance", instrument)
		if err != nil {
			evidence.Failures = append(evidence.Failures, symbol+"_view_failed")
			continue
		}
		eligible := view.Eligible(client.MonotonicOffset(), 5*time.Second)
		evidence.FinalBooks[symbol] = bookSample{Health: string(view.Health()), Generation: view.Generation(),
			Sequence: view.Sequence(), Version: view.Version(), Eligible: eligible}
		if !eligible {
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

func finalFlush(
	streamRecorder pendingSoakFlusher,
	latest recorder.DatasetManifest,
) (recorder.DatasetManifest, error) {
	raw, canonical := streamRecorder.PendingCounts()
	if raw == 0 && canonical == 0 && latest.Revision != 0 {
		return latest, nil
	}
	if raw == 0 || raw != canonical {
		return recorder.DatasetManifest{}, errors.New("recorder pending rows are incomplete")
	}
	manifest, err := streamRecorder.Flush()
	return manifest, err
}

func prepareEmptyRoot(root string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("A7 soak output directory must be empty")
	}
	return nil
}

func soakInstruments(t *testing.T) []domain.Instrument {
	t.Helper()
	btc, btcErr := domain.NewSpotInstrument("BTC", "USDT")
	eth, ethErr := domain.NewSpotInstrument("ETH", "USDT")
	if btcErr != nil || ethErr != nil {
		t.Fatal(btcErr, ethErr)
	}
	return []domain.Instrument{btc, eth}
}

func readMemory(observedAt time.Time) memorySample {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	sample := memorySample{ObservedAt: observedAt, HeapAlloc: stats.HeapAlloc, HeapInUse: stats.HeapInuse, Sys: stats.Sys}
	file, err := os.Open("/proc/self/status")
	if err == nil {
		scanner := bufio.NewScanner(file)
		sample.ProcStatusAvailable = true
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 2 {
				continue
			}
			value, parseErr := strconv.ParseUint(fields[1], 10, 64)
			if parseErr != nil {
				continue
			}
			switch fields[0] {
			case "VmRSS:":
				sample.RSS = value * 1024
			case "VmHWM:":
				sample.RSSHighWater = value * 1024
			}
		}
		_ = file.Close()
	}
	if descriptors, readErr := os.ReadDir("/proc/self/fd"); readErr == nil {
		sample.OpenFDs = uint64(len(descriptors))
		sample.OpenFDsAvailable = true
	}
	return sample
}

func readStorage(observedAt time.Time, root string) storageSample {
	sample := storageSample{ObservedAt: observedAt}
	if root == "" {
		return sample
	}
	var stats syscall.Statfs_t
	if err := syscall.Statfs(root, &stats); err != nil {
		return sample
	}
	sample.AvailableBytes = stats.Bavail * uint64(stats.Bsize)
	sample.StatfsAvailable = true
	sample.TotalBytes = stats.Blocks * uint64(stats.Bsize)
	sample.AvailableInodes = stats.Ffree
	return sample
}

func positiveLeakTrend(samples []memorySample) bool {
	if len(samples) < 24 {
		return false
	}
	warm := len(samples) / 6
	window := len(samples) / 12
	first := medianHeap(samples[warm : warm+window])
	last := medianHeap(samples[len(samples)-window:])
	tolerance := uint64(8 * 1024 * 1024)
	if percent := first / 20; percent > tolerance {
		tolerance = percent
	}
	return last > first+tolerance || last > declaredHeapLimit
}

func medianHeap(samples []memorySample) uint64 {
	values := make([]uint64, len(samples))
	for index := range samples {
		values[index] = samples[index].HeapAlloc
	}
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	return values[len(values)/2]
}

func writeSoakEvidence(root string, evidence soakEvidence) error {
	return writeAtomicJSON(filepath.Join(root, "a7-soak-evidence.json"), evidence)
}

func containsFailure(failures []string, target string) bool {
	for _, failure := range failures {
		if failure == target {
			return true
		}
	}
	return false
}

func uniqueSortedFailures(failures []string) []string {
	if len(failures) == 0 {
		return nil
	}
	sort.Strings(failures)
	unique := failures[:1]
	for _, failure := range failures[1:] {
		if failure != unique[len(unique)-1] {
			unique = append(unique, failure)
		}
	}
	return unique
}
