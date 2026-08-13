package operationalReadiness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FilePreflight reads a host-generated, bounded preflight document.
type FilePreflight struct{ Path string }

// Check strictly reads one bounded preflight document.
func (source FilePreflight) Check(context.Context) (Preflight, error) {
	var value Preflight
	if readStrictJSON(source.Path, &value) != nil {
		return Preflight{}, fmt.Errorf("operational_readiness_preflight_read_failed")
	}
	value.CheckedAt = value.CheckedAt.UTC()
	return value, nil
}

// FileProbe reads the latest atomic live sample document each interval.
type FileProbe struct {
	Path         string
	mutex        sync.Mutex
	lastRevision uint64
}

// Observe strictly reads a fresh, increasing live sample document.
func (source *FileProbe) Observe(_ context.Context, _ uint64, observedAt time.Time) (Sample, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	var sample Sample
	if readStrictJSON(source.Path, &sample) != nil || sample.SourceRevision <= source.lastRevision ||
		sample.ObservedAt.IsZero() || observedAt.Sub(sample.ObservedAt.UTC()) > 2*time.Minute ||
		sample.ObservedAt.After(observedAt.Add(5*time.Second)) {
		return Sample{}, fmt.Errorf("operational_readiness_sample_source_invalid")
	}
	source.lastRevision = sample.SourceRevision
	sample.Ordinal, sample.ObservedAt, sample.PriorSampleHash, sample.SampleHash = 0, time.Time{}, "", ""
	return sample, nil
}

// FileFaultSource reads terminal drill outcomes written by the approved orchestrator.
type FileFaultSource struct{ Path string }

// Events strictly reads the terminal approved-drill evidence list.
func (source FileFaultSource) Events(_ context.Context, runID string) ([]FaultEvent, error) {
	var events []FaultEvent
	if readStrictJSON(source.Path, &events) != nil {
		return nil, fmt.Errorf("operational_readiness_fault_evidence_read_failed")
	}
	for index := range events {
		if events[index].RunID != runID {
			return nil, fmt.Errorf("operational_readiness_fault_evidence_run_mismatch")
		}
		events[index].OccurredAt = events[index].OccurredAt.UTC()
	}
	return events, nil
}

// FileStore creates one no-replace run directory and fsyncs every sample.
type FileStore struct {
	Root    string
	mutex   sync.Mutex
	runID   string
	samples *os.File
}

// Begin creates the unique run directory and immutable start evidence.
func (store *FileStore) Begin(_ context.Context, configuration Config, preflight Preflight, started time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.Root == "" || store.samples != nil || filepath.Base(configuration.Identity.RunID) != configuration.Identity.RunID {
		return fmt.Errorf("operational_readiness_evidence_store_invalid")
	}
	if err := os.MkdirAll(store.Root, 0o750); err != nil {
		return err
	}
	runRoot := filepath.Join(store.Root, configuration.Identity.RunID)
	if err := os.Mkdir(runRoot, 0o750); err != nil {
		return fmt.Errorf("operational_readiness_run_already_exists")
	}
	start := struct {
		SchemaVersion string    `json:"schema_version"`
		Config        Config    `json:"config"`
		Preflight     Preflight `json:"preflight"`
		StartedAt     time.Time `json:"started_at"`
	}{"axiom.operationalReadiness.start.v1", configuration, preflight, started}
	if err := writeJSONNoReplace(filepath.Join(runRoot, "start.json"), start); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(runRoot, "samples.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o440)
	if err != nil {
		return err
	}
	store.runID, store.samples = configuration.Identity.RunID, file
	return syncDirectory(runRoot)
}

// AppendSample appends and fsyncs one hash-chained observation.
func (store *FileStore) AppendSample(_ context.Context, runID string, sample Sample) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.samples == nil || runID != store.runID {
		return fmt.Errorf("operational_readiness_sample_store_unavailable")
	}
	payload, err := json.Marshal(sample)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if _, err = store.samples.Write(payload); err != nil {
		return err
	}
	return store.samples.Sync()
}

// Finish closes the sample chain and writes one no-replace terminal verdict.
func (store *FileStore) Finish(_ context.Context, evidence Evidence) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if store.samples == nil || evidence.Identity.RunID != store.runID {
		return fmt.Errorf("operational_readiness_evidence_store_unavailable")
	}
	if err := store.samples.Sync(); err != nil {
		return err
	}
	if err := store.samples.Close(); err != nil {
		return err
	}
	store.samples = nil
	runRoot := filepath.Join(store.Root, store.runID)
	if err := writeJSONNoReplace(filepath.Join(runRoot, "verdict.json"), evidence); err != nil {
		return err
	}
	return syncDirectory(runRoot)
}

func readStrictJSON(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 10<<20 {
		return fmt.Errorf("json_source_invalid")
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("json_source_trailing_data")
	}
	return nil
}

func writeJSONNoReplace(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o440)
	if err != nil {
		return err
	}
	if _, err = file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
