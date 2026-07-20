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
	statusWriteFailure   = "status_write_failed"
)

type soakFlusher interface {
	Flush() (recorder.DatasetManifest, error)
}

type pendingSoakFlusher interface {
	soakFlusher
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
	Books                map[string]bookSample                     `json:"books"`
	ManifestRevision     uint64                                    `json:"manifest_revision"`
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
	flushEvery, sampleEvery time.Duration,
	latestManifest *recorder.DatasetManifest,
	evidence *soakEvidence,
	writer soakStatusWriter,
) string {
	if writer == nil || writer(root, captureSoakStatus(time.Now().UTC(), client, collectors, *latestManifest, evidence)) != nil {
		return statusWriteFailure
	}
	flushTicker, sampleTicker := time.NewTicker(flushEvery), time.NewTicker(sampleEvery)
	defer flushTicker.Stop()
	defer sampleTicker.Stop()
	previous := make(map[string]binance.CollectorStatsSnapshot, len(collectors))
	for {
		select {
		case <-ctx.Done():
			return ""
		case <-flushTicker.C:
			manifest, err := streamRecorder.Flush()
			if err != nil {
				return periodicFlushFailure
			}
			*latestManifest = manifest
			if writer(root, captureSoakStatus(time.Now().UTC(), client, collectors, manifest, evidence)) != nil {
				return statusWriteFailure
			}
		case observed := <-sampleTicker.C:
			evidence.Memory = append(evidence.Memory, readMemory(observed.UTC()))
			for symbol, collector := range collectors {
				current, prior := collector.Stats(), previous[symbol]
				if current.Reconnects != prior.Reconnects || current.Rebuilds != prior.Rebuilds ||
					current.Gaps != prior.Gaps {
					evidence.Incidents = append(evidence.Incidents, incidentSample{ObservedAt: observed.UTC(),
						Instrument: symbol, Reconnects: current.Reconnects, Rebuilds: current.Rebuilds,
						Gaps: current.Gaps})
				}
				previous[symbol] = current
			}
			if writer(root, captureSoakStatus(observed.UTC(), client, collectors, *latestManifest, evidence)) != nil {
				return statusWriteFailure
			}
		}
	}
}

func captureSoakStatus(
	observed time.Time,
	client *binance.PublicClient,
	collectors map[string]*binance.InstrumentCollector,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
) soakStatus {
	failures := append([]string(nil), evidence.Failures...)
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
	sort.Strings(failures)
	memory := readMemory(observed)
	if count := len(evidence.Memory); count > 0 {
		memory = evidence.Memory[count-1]
	}
	elapsed := observed.Sub(evidence.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	return soakStatus{SchemaVersion: "axiom.a7-soak-status.v1", SourceCommit: evidence.SourceCommit,
		Formal: evidence.Formal, StartedAt: evidence.StartedAt, ObservedAt: observed, Elapsed: elapsed,
		RequiredDuration: evidence.RequiredDuration, ProvisionalQualified: len(failures) == 0,
		ProvisionalFailures: failures, ProvisionalSLOs: slos, Collectors: statsBySymbol,
		Memory: memory, Books: books, ManifestRevision: manifest.Revision}
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
	collectors map[string]*binance.InstrumentCollector,
	manifest recorder.DatasetManifest,
	evidence *soakEvidence,
) error {
	status := captureSoakStatus(time.Now().UTC(), client, collectors, manifest, evidence)
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
