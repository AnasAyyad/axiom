package qualification

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/exchanges/binance"
	"axiom/internal/recorder"
)

const (
	periodicFlushFailure = "periodic_flush_failed"
	capacityFlushFailure = "capacity_flush_failed"
	statusWriteFailure   = "status_write_failed"
)

type soakFlusher interface {
	FlushReady() (recorder.DatasetManifest, bool, error)
	PendingUsage() recorder.PendingUsage
	FlushRequired() <-chan struct{}
}

type pendingSoakFlusher interface {
	soakFlusher
	Flush() (recorder.DatasetManifest, error)
	PendingCounts() (uint64, uint64)
}

type soakStatusWriter func(string, soakStatus) error

type soakStatus struct {
	SchemaVersion        string                                    `json:"schema_version"`
	SourceCommit         string                                    `json:"source_commit"`
	Formal               bool                                      `json:"formal"`
	StartedAt            time.Time                                 `json:"started_at"`
	ObservedAt           time.Time                                 `json:"observed_at"`
	Elapsed              time.Duration                             `json:"elapsed_nanos"`
	RequiredDuration     time.Duration                             `json:"required_duration_nanos"`
	ProvisionalQualified bool                                      `json:"provisional_qualified"`
	ProvisionalFailures  []string                                  `json:"provisional_failures"`
	ProvisionalSLOs      map[string]provisionalCollectorSLO        `json:"provisional_slos"`
	Collectors           map[string]binance.CollectorStatsSnapshot `json:"collectors"`
	Memory               memorySample                              `json:"memory"`
	Storage              storageSample                             `json:"storage"`
	Books                map[string]bookSample                     `json:"books"`
	ManifestRevision     uint64                                    `json:"manifest_revision"`
	Recorder             recorder.PendingUsage                     `json:"recorder"`
	CollectorRunning     map[string]bool                           `json:"collector_running"`
	FailureDetails       []qualificationFailure                    `json:"failure_details,omitempty"`
	EventJournalSequence uint64                                    `json:"event_journal_sequence"`
	EventJournalHash     string                                    `json:"event_journal_hash,omitempty"`
}

type provisionalCollectorSLO struct {
	HotPathP99WithinTarget bool          `json:"hot_path_p99_within_target"`
	ResyncP95WithinTarget  bool          `json:"resync_p95_within_target"`
	ResyncSamples          uint64        `json:"resync_samples"`
	ResyncOver15Seconds    uint64        `json:"resync_over_15_seconds"`
	ResyncP95              time.Duration `json:"resync_p95_nanos"`
	ResyncMax              time.Duration `json:"resync_max_nanos"`
	BookEligible           bool          `json:"book_eligible"`
}

func qualificationSourceCommit() (string, error) {
	commit := os.Getenv("AXIOM_A7_SOURCE_COMMIT")
	if !validGitCommit(commit) {
		return "", errors.New("AXIOM_A7_SOURCE_COMMIT must be the exact 40-character source commit")
	}
	return commit, nil
}

func validGitCommit(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, value := range commit {
		if (value < '0' || value > '9') && (value < 'a' || value > 'f') {
			return false
		}
	}
	return true
}

func monitorSoakFailClosed(
	ctx context.Context,
	root string,
	client *binance.PublicClient,
	streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector,
	collectorResults <-chan collectorResult,
	flushEvery, sampleEvery time.Duration,
	latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence,
	writer soakStatusWriter,
	journal *qualificationJournal,
) string {
	if failure := initializeSoakMonitor(root, client, streamRecorder, collectors,
		latestManifest, evidence, writer, journal); failure != "" {
		return failure
	}
	flushTicker, sampleTicker := time.NewTicker(flushEvery), time.NewTicker(sampleEvery)
	defer flushTicker.Stop()
	defer sampleTicker.Stop()
	previous := make(map[string]binance.CollectorStatsSnapshot, len(collectors))
	for {
		select {
		case <-ctx.Done():
			return ""
		case result := <-collectorResults:
			return handleSoakCollectorTerminal(ctx, result, root, client, streamRecorder,
				collectors, latestManifest, evidence, writer, journal)
		case <-streamRecorder.FlushRequired():
			if failure := flushSoakStep("capacity", root, client, streamRecorder, collectors,
				latestManifest, evidence, writer, journal); failure != "" {
				return failure
			}
		case <-flushTicker.C:
			if failure := flushSoakStep("scheduled", root, client, streamRecorder, collectors,
				latestManifest, evidence, writer, journal); failure != "" {
				return failure
			}
		case observed := <-sampleTicker.C:
			if failure := sampleSoakStep(observed.UTC(), root, client, streamRecorder, collectors,
				previous, latestManifest, evidence, writer, journal); failure != "" {
				return failure
			}
		}
	}
}

func initializeSoakMonitor(root string, client *binance.PublicClient, streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector, latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence, writer soakStatusWriter, journal *qualificationJournal) string {
	if writer == nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "initial_status", "writer_missing", nil))
		return statusWriteFailure
	}
	initial := captureSoakStatus(time.Now().UTC(), client, streamRecorder, collectors,
		*latestManifest, evidence, journal)
	if err := writer(root, initial); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "initial_status", "atomic_status_write", err))
		return statusWriteFailure
	}
	if !appendQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: initial.ObservedAt,
		Phase: "initial_status", Outcome: "passed"}) {
		return "event_journal_failed"
	}
	return ""
}

func handleSoakCollectorTerminal(ctx context.Context, result collectorResult, root string,
	client *binance.PublicClient, streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector, latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence, writer soakStatusWriter, journal *qualificationJournal) string {
	if ctx.Err() != nil && (result.err == nil || errors.Is(result.err, context.Canceled) ||
		errors.Is(result.err, context.DeadlineExceeded)) {
		evidence.CollectorRunning[result.instrument] = false
		return ""
	}
	recordSoakCollectorFailure(result, evidence, journal)
	status := captureSoakStatus(time.Now().UTC(), client, streamRecorder,
		collectors, *latestManifest, evidence, journal)
	if err := writer(root, status); err != nil {
		detail := boundedQualificationFailure(statusWriteFailure,
			"collector_terminal_status", "atomic_status_write", err)
		evidence.Failures = append(evidence.Failures, statusWriteFailure)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendQualificationEvent(journal, evidence, qualificationEvent{Phase: "collector_terminal_status",
			Instrument: result.instrument, Outcome: "failed", Code: statusWriteFailure})
		return statusWriteFailure
	}
	return "collector_failed"
}

func soakFlushLabels(trigger string) (string, string) {
	if trigger == "capacity" {
		return "recorder_capacity_flush", capacityFlushFailure
	}
	return "recorder_flush", periodicFlushFailure
}

func flushSoakStep(
	trigger string,
	root string,
	client *binance.PublicClient,
	streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector,
	latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence,
	writer soakStatusWriter,
	journal *qualificationJournal,
) string {
	usage := streamRecorder.PendingUsage()
	phase, failureCode := soakFlushLabels(trigger)
	started := time.Now()
	manifest, flushed, err := streamRecorder.FlushReady()
	duration := time.Since(started)
	if err != nil {
		detail := boundedQualificationFailure(failureCode, phase, "flush_ready", err)
		evidence.FailureDetails = append(evidence.FailureDetails, detail)
		appendQualificationEvent(journal, evidence, qualificationEvent{Phase: phase, Trigger: trigger,
			Outcome: "failed", Code: failureCode, PendingRaw: usage.RawRecords,
			PendingCanonical: usage.CanonicalRecords, Duration: duration,
			Recorder: detail.Recorder, RecorderUsage: &usage})
		return failureCode
	}
	if flushed {
		*latestManifest = manifest
	} else {
		manifest = *latestManifest
	}
	if !appendQualificationEvent(journal, evidence, qualificationEvent{Phase: phase, Trigger: trigger,
		Outcome: "passed", ManifestRevision: manifest.Revision, PendingRaw: usage.RawRecords,
		PendingCanonical: usage.CanonicalRecords, Duration: duration, RecorderUsage: &usage}) {
		return "event_journal_failed"
	}
	status := captureSoakStatus(time.Now().UTC(), client, streamRecorder, collectors, manifest, evidence, journal)
	if err = writer(root, status); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "periodic_status", "atomic_status_write", err))
		appendQualificationEvent(journal, evidence, qualificationEvent{Phase: "periodic_status", Outcome: "failed",
			Code: statusWriteFailure, ManifestRevision: manifest.Revision})
		return statusWriteFailure
	}
	if !appendQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: status.ObservedAt,
		Phase: "periodic_status", Outcome: "passed", ManifestRevision: manifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func sampleSoakStep(
	observed time.Time,
	root string,
	client *binance.PublicClient,
	streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector,
	previous map[string]binance.CollectorStatsSnapshot,
	latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence,
	writer soakStatusWriter,
	journal *qualificationJournal,
) string {
	evidence.Memory = append(evidence.Memory, readMemory(observed))
	evidence.Storage = append(evidence.Storage, readStorage(observed, evidence.root))
	for symbol, collector := range collectors {
		current, prior := collector.Stats(), previous[symbol]
		if current.Reconnects != prior.Reconnects || current.Rebuilds != prior.Rebuilds || current.Gaps != prior.Gaps {
			evidence.Incidents = append(evidence.Incidents, incidentSample{ObservedAt: observed,
				Instrument: symbol, Reconnects: current.Reconnects, Rebuilds: current.Rebuilds, Gaps: current.Gaps})
		}
		previous[symbol] = current
	}
	status := captureSoakStatus(observed, client, streamRecorder, collectors,
		*latestManifest, evidence, journal)
	if err := writer(root, status); err != nil {
		evidence.FailureDetails = append(evidence.FailureDetails,
			boundedQualificationFailure(statusWriteFailure, "sample_status", "atomic_status_write", err))
		appendQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: observed,
			Phase: "sample_status", Outcome: "failed", Code: statusWriteFailure,
			ManifestRevision: latestManifest.Revision})
		return statusWriteFailure
	}
	if !appendQualificationEvent(journal, evidence, qualificationEvent{RecordedAt: observed,
		Phase: "sample_status", Outcome: "passed", ManifestRevision: latestManifest.Revision}) {
		return "event_journal_failed"
	}
	return ""
}

func captureSoakStatus(
	observed time.Time,
	client *binance.PublicClient,
	streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
	journal *qualificationJournal,
) soakStatus {
	failures, statsBySymbol, slos, books := provisionalCollectorSnapshots(client, collectors, evidence.Failures)
	sort.Strings(failures)
	memory := readMemory(observed)
	if count := len(evidence.Memory); count > 0 {
		memory = evidence.Memory[count-1]
	}
	storage := readStorage(observed, evidence.root)
	if count := len(evidence.Storage); count > 0 {
		storage = evidence.Storage[count-1]
	}
	elapsed := observed.Sub(evidence.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	journalSequence, journalHash := journal.Snapshot()
	return soakStatus{SchemaVersion: "axiom.a7-soak-status.v3", SourceCommit: evidence.SourceCommit,
		Formal: evidence.Formal, StartedAt: evidence.StartedAt, ObservedAt: observed, Elapsed: elapsed,
		RequiredDuration: evidence.RequiredDuration, ProvisionalQualified: len(failures) == 0,
		ProvisionalFailures: failures, ProvisionalSLOs: slos, Collectors: statsBySymbol,
		Memory: memory, Storage: storage, Books: books, Recorder: streamRecorder.PendingUsage(),
		CollectorRunning: cloneCollectorRunning(evidence.CollectorRunning), ManifestRevision: manifest.Revision,
		FailureDetails:       append([]qualificationFailure(nil), evidence.FailureDetails...),
		EventJournalSequence: journalSequence, EventJournalHash: journalHash}
}

func cloneCollectorRunning(source map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(source))
	for instrument, running := range source {
		clone[instrument] = running
	}
	return clone
}

func provisionalCollectorSnapshots(
	client *binance.PublicClient,
	collectors map[string]*binance.InstrumentCollector,
	priorFailures []string,
) ([]string, map[string]binance.CollectorStatsSnapshot, map[string]provisionalCollectorSLO, map[string]bookSample) {
	failures := append([]string(nil), priorFailures...)
	statsBySymbol := make(map[string]binance.CollectorStatsSnapshot, len(collectors))
	slos := make(map[string]provisionalCollectorSLO, len(collectors))
	books := make(map[string]bookSample, len(collectors))
	for symbol, collector := range collectors {
		stats := collector.Stats()
		statsBySymbol[symbol] = stats
		book := currentBookSample(symbol, client, collector)
		books[symbol] = book
		slo := provisionalCollectorSLO{
			HotPathP99WithinTarget: stats.HotPathP99 <= 10*time.Millisecond,
			ResyncP95WithinTarget:  stats.ResyncP95 <= 15*time.Second,
			ResyncSamples:          stats.ResyncSamples,
			ResyncOver15Seconds:    stats.ResyncOver15Seconds,
			ResyncP95:              stats.ResyncP95,
			ResyncMax:              stats.ResyncMax,
			BookEligible:           book.Eligible,
		}
		slos[symbol] = slo
		if !slo.HotPathP99WithinTarget || !slo.ResyncP95WithinTarget {
			failures = append(failures, symbol+"_slo_failed")
		}
		if !slo.BookEligible {
			failures = append(failures, symbol+"_ineligible")
		}
	}
	return failures, statsBySymbol, slos, books
}

func currentBookSample(
	symbol string,
	client *binance.PublicClient,
	collector *binance.InstrumentCollector,
) bookSample {
	if len(symbol) != 7 || client == nil {
		return bookSample{}
	}
	base, baseErr := domain.ParseAssetSymbol(symbol[:3])
	quote, quoteErr := domain.ParseAssetSymbol(symbol[3:])
	if baseErr != nil || quoteErr != nil {
		return bookSample{}
	}
	instrument, err := domain.NewSpotInstrument(base, quote)
	if err != nil {
		return bookSample{}
	}
	view, err := collector.Views().Book("binance", instrument)
	if err != nil {
		return bookSample{}
	}
	return bookSample{Health: string(view.Health()), Generation: view.Generation(), Sequence: view.Sequence(),
		Version: view.Version(), Eligible: view.Eligible(client.MonotonicOffset(), 5*time.Second)}
}

func writeSoakStatus(root string, status soakStatus) error {
	return writeAtomicJSON(filepath.Join(root, "a7-soak-status.json"), status)
}

func writeFinalSoakStatus(
	root string,
	client *binance.PublicClient,
	streamRecorder soakFlusher,
	collectors map[string]*binance.InstrumentCollector,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
	journal *qualificationJournal,
) error {
	status := captureSoakStatus(time.Now().UTC(), client, streamRecorder, collectors,
		manifest, evidence, journal)
	status.ProvisionalFailures = append([]string(nil), evidence.Failures...)
	sort.Strings(status.ProvisionalFailures)
	status.ProvisionalQualified = len(status.ProvisionalFailures) == 0
	return writeSoakStatus(root, status)
}

func writeAtomicJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	directory, base := filepath.Dir(path), filepath.Base(path)
	file, err := os.CreateTemp(directory, "."+base+".*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
		_ = os.Remove(temporary)
	}()
	if err = file.Chmod(0o640); err != nil {
		return err
	}
	if _, err = file.Write(payload); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	closed = true
	if err = os.Rename(temporary, path); err != nil {
		return err
	}
	directoryFile, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryFile.Close()
	return directoryFile.Sync()
}
