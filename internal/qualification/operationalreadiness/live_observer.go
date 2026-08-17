package operationalReadiness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"
)

const (
	DrillObservationSchema = "axiom.operational_readiness.drill-observation.v1"
	MaximumDrillAge        = 24 * time.Hour
)

// DatabaseTelemetry contains bounded facts read through the read-only database
// role. It deliberately contains no identifiers, payloads, URLs, or errors.
type DatabaseTelemetry struct {
	ObservedAt               time.Time `json:"observed_at"`
	StaleDecisions           uint64    `json:"stale_decisions"`
	UninvalidatedGaps        uint64    `json:"uninvalidated_gaps"`
	DuplicateOrders          uint64    `json:"duplicate_orders"`
	LostFills                uint64    `json:"lost_fills"`
	DoublePostedFills        uint64    `json:"double_posted_fills"`
	UnbalancedJournals       uint64    `json:"unbalanced_journals"`
	DiskLevel                string    `json:"disk_level"`
	DiskObservedAt           time.Time `json:"disk_observed_at"`
	ProductionTargetObserved bool      `json:"production_target_observed"`
}

// RuntimeTelemetry contains live, credential-free service and metrics facts.
type RuntimeTelemetry struct {
	ObservedAt             time.Time `json:"observed_at"`
	DecodeBookP99Millis    uint64    `json:"decode_book_p99_ms"`
	StrategyRiskP99Millis  uint64    `json:"strategy_risk_p99_ms"`
	ResyncP95Millis        uint64    `json:"resync_p95_ms"`
	ResidentMemoryBytes    uint64    `json:"resident_memory_bytes"`
	MemoryLimitBytes       uint64    `json:"memory_limit_bytes"`
	AllDeclaredLoadHealthy bool      `json:"all_declared_load_healthy"`
}

// DrillObservation is produced by the separately controlled rehearsal/fault
// orchestrator. These are measured outcomes that cannot safely be inferred by
// a read-only continuously running observer.
type DrillObservation struct {
	SchemaVersion                string    `json:"schema_version"`
	ObservedAt                   time.Time `json:"observed_at"`
	ReplayMismatches             uint64    `json:"replay_mismatches"`
	CriticalAlertMillis          uint64    `json:"critical_alert_ms"`
	ExternalAlertP95Millis       uint64    `json:"external_alert_p95_ms"`
	GracefulShutdownMillis       uint64    `json:"graceful_shutdown_ms"`
	ShadowRecoveryMillis         uint64    `json:"shadow_recovery_ms"`
	SandboxRecoveryMillis        uint64    `json:"sandbox_recovery_ms"`
	DatabaseCommitRPOZero        bool      `json:"database_commit_rpo_zero"`
	RecorderWithinFlushRPO       bool      `json:"recorder_within_flush_rpo"`
	HeavyJobsRejectedAtHigh      bool      `json:"heavy_jobs_rejected_at_high"`
	RecordingPausedAtCritical    bool      `json:"recording_paused_at_critical"`
	JournalAuditWritable         bool      `json:"journal_audit_writable"`
	DeclaredLoadExercised        bool      `json:"declared_load_exercised"`
	ProductionTargetObserved     bool      `json:"production_target_observed"`
	ProhibitedCapabilityObserved bool      `json:"prohibited_capability_observed"`
}

type DatabaseTelemetrySource interface {
	Observe(context.Context, time.Time, time.Time) (DatabaseTelemetry, error)
}

type RuntimeTelemetrySource interface {
	Observe(context.Context, time.Time) (RuntimeTelemetry, error)
}

type DrillObservationSource interface {
	Observe(time.Time) (DrillObservation, error)
}

// LiveObserver assembles one sample only when all independent sources succeed.
type LiveObserver struct {
	Database DatabaseTelemetrySource
	Runtime  RuntimeTelemetrySource
	Drill    DrillObservationSource
	Window   time.Duration
}

func (observer LiveObserver) Observe(ctx context.Context, revision uint64, observedAt time.Time) (Sample, error) {
	if observer.Database == nil || observer.Runtime == nil || observer.Drill == nil ||
		observer.Window <= 0 || observer.Window > 24*time.Hour || revision == 0 || observedAt.IsZero() {
		return Sample{}, fmt.Errorf("operational_readiness_live_observer_invalid")
	}
	observedAt = observedAt.UTC()
	database, err := observer.Database.Observe(ctx, observedAt.Add(-observer.Window), observedAt)
	if err != nil || !freshSource(database.ObservedAt, observedAt, 2*time.Minute) ||
		!freshSource(database.DiskObservedAt, observedAt, 2*time.Minute) {
		return Sample{}, fmt.Errorf("operational_readiness_database_telemetry_unavailable")
	}
	runtime, err := observer.Runtime.Observe(ctx, observedAt)
	if err != nil || !freshSource(runtime.ObservedAt, observedAt, 30*time.Second) ||
		runtime.MemoryLimitBytes == 0 {
		return Sample{}, fmt.Errorf("operational_readiness_runtime_telemetry_unavailable")
	}
	drill, err := observer.Drill.Observe(observedAt)
	if err != nil || drill.SchemaVersion != DrillObservationSchema ||
		!freshSource(drill.ObservedAt, observedAt, MaximumDrillAge) {
		return Sample{}, fmt.Errorf("operational_readiness_drill_telemetry_unavailable")
	}
	databaseHash, err := evidenceDigest(database)
	if err != nil {
		return Sample{}, fmt.Errorf("operational_readiness_database_telemetry_unavailable")
	}
	runtimeHash, err := evidenceDigest(runtime)
	if err != nil {
		return Sample{}, fmt.Errorf("operational_readiness_runtime_telemetry_unavailable")
	}
	drillHash, err := evidenceDigest(drill)
	if err != nil {
		return Sample{}, fmt.Errorf("operational_readiness_drill_telemetry_unavailable")
	}
	return Sample{
		ObservedAt: observedAt, SourceRevision: revision, DatabaseEvidenceHash: databaseHash,
		RuntimeEvidenceHash: runtimeHash, DrillEvidenceHash: drillHash,
		StaleDecisions: database.StaleDecisions, UninvalidatedGaps: database.UninvalidatedGaps,
		DuplicateOrders: database.DuplicateOrders, LostFills: database.LostFills,
		DoublePostedFills: database.DoublePostedFills, UnbalancedJournals: database.UnbalancedJournals,
		ReplayMismatches:    drill.ReplayMismatches,
		DecodeBookP99Millis: runtime.DecodeBookP99Millis, StrategyRiskP99Millis: runtime.StrategyRiskP99Millis,
		ResyncP95Millis:     runtime.ResyncP95Millis,
		CriticalAlertMillis: drill.CriticalAlertMillis, ExternalAlertP95Millis: drill.ExternalAlertP95Millis,
		GracefulShutdownMillis: drill.GracefulShutdownMillis, ShadowRecoveryMillis: drill.ShadowRecoveryMillis,
		SandboxRecoveryMillis: drill.SandboxRecoveryMillis,
		DatabaseCommitRPOZero: drill.DatabaseCommitRPOZero, RecorderWithinFlushRPO: drill.RecorderWithinFlushRPO,
		ResidentMemoryBytes: runtime.ResidentMemoryBytes, MemoryLimitBytes: runtime.MemoryLimitBytes,
		DiskLevel: database.DiskLevel, HeavyJobsRejectedAtHigh: drill.HeavyJobsRejectedAtHigh,
		RecordingPausedAtCritical:    drill.RecordingPausedAtCritical,
		JournalAuditWritable:         drill.JournalAuditWritable,
		AllDeclaredLoadHealthy:       runtime.AllDeclaredLoadHealthy && drill.DeclaredLoadExercised,
		ProductionTargetObserved:     database.ProductionTargetObserved || drill.ProductionTargetObserved,
		ProhibitedCapabilityObserved: drill.ProhibitedCapabilityObserved,
	}, nil
}

func freshSource(sourceAt, observedAt time.Time, maximumAge time.Duration) bool {
	if sourceAt.IsZero() || sourceAt.After(observedAt.Add(5*time.Second)) {
		return false
	}
	return observedAt.Sub(sourceAt.UTC()) <= maximumAge
}

func evidenceDigest(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

// FileDrillObservation strictly reads one bounded orchestrator result.
type FileDrillObservation struct{ Path string }

func (source FileDrillObservation) Observe(time.Time) (DrillObservation, error) {
	var value DrillObservation
	if readStrictJSON(source.Path, &value) != nil {
		return DrillObservation{}, fmt.Errorf("operational_readiness_drill_observation_invalid")
	}
	value.ObservedAt = value.ObservedAt.UTC()
	return value, nil
}

// WriteLiveSample atomically replaces only the rolling sample source.
func WriteLiveSample(path string, sample Sample) error {
	if path == "" || filepath.Base(path) == "." || filepath.Base(path) == string(filepath.Separator) {
		return fmt.Errorf("operational_readiness_sample_path_invalid")
	}
	payload, err := json.MarshalIndent(sample, "", "  ")
	if err != nil {
		return fmt.Errorf("operational_readiness_sample_encode_failed")
	}
	payload = append(payload, '\n')
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".operational-readiness-sample-*")
	if err != nil {
		return fmt.Errorf("operational_readiness_sample_write_failed")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0o440); err == nil {
		_, err = file.Write(payload)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil || os.Rename(temporary, path) != nil {
		return fmt.Errorf("operational_readiness_sample_write_failed")
	}
	if err = syncDirectory(directory); err != nil {
		return fmt.Errorf("operational_readiness_sample_write_failed")
	}
	return nil
}

func NextLiveSampleRevision(path string) (uint64, error) {
	var sample Sample
	err := readStrictJSON(path, &sample)
	if os.IsNotExist(err) {
		return 1, nil
	}
	if err != nil || sample.SourceRevision == math.MaxUint64 {
		return 0, fmt.Errorf("operational_readiness_sample_revision_invalid")
	}
	return sample.SourceRevision + 1, nil
}
