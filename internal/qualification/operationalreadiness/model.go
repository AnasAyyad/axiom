// Package operational_readiness implements the separate authenticated owner-console release seven-day readiness qualification.
package operationalReadiness

import (
	"crypto/ed25519"
	"fmt"
	"regexp"
	"slices"
	"time"
)

// Immutable operational readiness qualification durations and service-level thresholds.
const (
	// FormalDuration is the exact uninterrupted operational readiness qualification clock.
	FormalDuration = 7 * 24 * time.Hour
	// MaximumSmokeDuration bounds a non-qualifying local runner exercise.
	MaximumSmokeDuration = 15 * time.Minute
	// CriticalAlertSLO is the maximum in-application critical-alert latency.
	CriticalAlertSLO = 5 * time.Second
	// ExternalAlertSLO is the maximum external delivery p95 latency.
	ExternalAlertSLO = 60 * time.Second
	// GracefulShutdownSLO is the maximum declared graceful shutdown duration.
	GracefulShutdownSLO = 60 * time.Second
	// ShadowRecoveryRTO is the maximum production-public shadow recovery time.
	ShadowRecoveryRTO = 5 * time.Minute
	// SandboxRecoveryRTO is the maximum Testnet or Demo recovery time.
	SandboxRecoveryRTO = 10 * time.Minute
	// CleanRestoreRTO is the initial database and market-data restore objective.
	CleanRestoreRTO = 4 * time.Hour
	// MaximumPreflightAge bounds evidence age before the official clock starts.
	MaximumPreflightAge = 15 * time.Minute
	// ClockThresholdMillis is the immutable maximum route clock uncertainty.
	ClockThresholdMillis = 100
)

// Mode distinguishes non-qualifying smoke from the exact formal run.
type Mode string

// Supported operational readiness qualification modes.
const (
	// ModeSmoke can test runner mechanics but can never qualify operational readiness.
	ModeSmoke Mode = "smoke"
	// ModeFormal requires the exact clean release and seven-day duration.
	ModeFormal Mode = "formal"
)

// TerminalState is the closed set of final readiness verdict states.
type TerminalState string

// Closed terminal verdict states written by the operational readiness runner.
const (
	// StateSmokePassed records successful non-qualifying runner exercise.
	StateSmokePassed TerminalState = "SMOKE_PASSED"
	// StatePassed records a qualifying complete formal run.
	StatePassed TerminalState = "PASSED"
	// StateFailed records an immutable terminal failure.
	StateFailed TerminalState = "FAILED"
)

var requiredImages = []string{"app", "backup", "caddy", "grafana", "postgres", "prometheus"}

var failureReasons = []string{
	"preflight_failed", "sample_unavailable", "sample_chain_invalid", "stale_decision",
	"uninvalidated_gap", "duplicate_order", "lost_fill", "double_posted_fill",
	"unbalanced_journal", "replay_mismatch", "latency_slo", "alert_slo",
	"shutdown_slo", "recovery_rto", "rpo_breach", "resource_limit",
	"memory_leak", "disk_pressure_unsafe", "fault_schedule_incomplete",
	"production_target", "prohibited_capability", "operator_abort", "evidence_failure",
}

// Identity binds a readiness run to exact release and server inputs.
type Identity struct {
	RunID             string            `json:"run_id"`
	Mode              Mode              `json:"mode"`
	SourceSHA         string            `json:"source_sha"`
	SourceDirty       bool              `json:"source_dirty"`
	ImageDigests      map[string]string `json:"image_digests"`
	ConfigurationHash string            `json:"configuration_hash"`
	ServerIdentity    string            `json:"server_identity"`
	DatasetIdentity   string            `json:"dataset_identity"`
	TestManifestHash  string            `json:"test_manifest_hash"`
}

// DeclaredLoad proves the official run exercises the full operational readiness platform surface.
type DeclaredLoad struct {
	CollectorsRecording      bool `json:"collectors_recording"`
	CoherentSampling         bool `json:"coherent_sampling"`
	StrategiesRiskAccounting bool `json:"strategies_risk_accounting"`
	ShadowVirtualExecution   bool `json:"shadow_virtual_execution"`
	APISSEUI                 bool `json:"api_sse_ui"`
	LabJobs                  bool `json:"lab_jobs"`
	ReportsExportsAlerts     bool `json:"reports_exports_alerts"`
	BackupsRestore           bool `json:"backups_restore"`
	ResourceLimits           bool `json:"resource_limits"`
}

func (load DeclaredLoad) complete() bool {
	return load.CollectorsRecording && load.CoherentSampling && load.StrategiesRiskAccounting &&
		load.ShadowVirtualExecution && load.APISSEUI && load.LabJobs &&
		load.ReportsExportsAlerts && load.BackupsRestore && load.ResourceLimits
}

// Preflight is immutable evidence captured before the official clock starts.
type Preflight struct {
	CheckedAt                             time.Time `json:"checked_at"`
	ReferenceServerApproved               bool      `json:"reference_server_approved"`
	ClockSynchronized                     bool      `json:"clock_synchronized"`
	ClockOffsetMillis                     uint64    `json:"clock_offset_ms"`
	ClockThresholdMillis                  uint64    `json:"clock_threshold_ms"`
	RouteClockThresholdPassed             bool      `json:"route_clock_threshold_passed"`
	TLSValid                              bool      `json:"tls_valid"`
	PinnedImageDigests                    bool      `json:"pinned_image_digests"`
	NonRootExecution                      bool      `json:"non_root_execution"`
	ResourceLimitsPassed                  bool      `json:"resource_limits_passed"`
	DiskCapacityPassed                    bool      `json:"disk_capacity_passed"`
	RemoteBackupIndependent               bool      `json:"remote_backup_independent"`
	BackupAgeSeconds                      uint64    `json:"backup_age_seconds"`
	CleanRestorePassed                    bool      `json:"clean_restore_passed"`
	CleanRestoreDurationSeconds           uint64    `json:"clean_restore_duration_seconds"`
	MarketDataRecoveryPassed              bool      `json:"market_data_recovery_passed"`
	SchemaUpgradePassed                   bool      `json:"schema_upgrade_passed"`
	RollbackForwardFixPassed              bool      `json:"rollback_forward_fix_passed"`
	SBOMPresent                           bool      `json:"sbom_present"`
	SecurityScanPassed                    bool      `json:"security_scan_passed"`
	ProductionPrivateSubmissionImpossible bool      `json:"production_private_submission_impossible"`
}

// FaultSpec is one version-controlled drill and its offset from the official start.
type FaultSpec struct {
	Scenario      string `json:"scenario"`
	OffsetSeconds uint64 `json:"offset_seconds"`
}

// FaultSchedule is the versioned ordered drill contract for one run.
type FaultSchedule struct {
	SchemaVersion string      `json:"schema_version"`
	Faults        []FaultSpec `json:"faults"`
}

// Config is the default-off, exact readiness-run contract.
type Config struct {
	Enabled        bool               `json:"enabled"`
	Identity       Identity           `json:"identity"`
	Duration       time.Duration      `json:"duration"`
	SampleInterval time.Duration      `json:"sample_interval"`
	EvidenceRoot   string             `json:"evidence_root"`
	DeclaredLoad   DeclaredLoad       `json:"declared_load"`
	FaultSchedule  FaultSchedule      `json:"fault_schedule"`
	SigningKey     ed25519.PrivateKey `json:"-"`
}

// Sample is one bounded all-subsystem observation in the authenticated chain.
type Sample struct {
	Ordinal                      uint64    `json:"ordinal"`
	ObservedAt                   time.Time `json:"observed_at"`
	SourceRevision               uint64    `json:"source_revision"`
	PriorSampleHash              string    `json:"prior_sample_hash,omitempty"`
	SampleHash                   string    `json:"sample_hash"`
	StaleDecisions               uint64    `json:"stale_decisions"`
	UninvalidatedGaps            uint64    `json:"uninvalidated_gaps"`
	DuplicateOrders              uint64    `json:"duplicate_orders"`
	LostFills                    uint64    `json:"lost_fills"`
	DoublePostedFills            uint64    `json:"double_posted_fills"`
	UnbalancedJournals           uint64    `json:"unbalanced_journals"`
	ReplayMismatches             uint64    `json:"replay_mismatches"`
	DecodeBookP99Millis          uint64    `json:"decode_book_p99_ms"`
	StrategyRiskP99Millis        uint64    `json:"strategy_risk_p99_ms"`
	ResyncP95Millis              uint64    `json:"resync_p95_ms"`
	CriticalAlertMillis          uint64    `json:"critical_alert_ms"`
	ExternalAlertP95Millis       uint64    `json:"external_alert_p95_ms"`
	GracefulShutdownMillis       uint64    `json:"graceful_shutdown_ms"`
	ShadowRecoveryMillis         uint64    `json:"shadow_recovery_ms"`
	SandboxRecoveryMillis        uint64    `json:"sandbox_recovery_ms"`
	DatabaseCommitRPOZero        bool      `json:"database_commit_rpo_zero"`
	RecorderWithinFlushRPO       bool      `json:"recorder_within_flush_rpo"`
	ResidentMemoryBytes          uint64    `json:"resident_memory_bytes"`
	MemoryLimitBytes             uint64    `json:"memory_limit_bytes"`
	DiskLevel                    string    `json:"disk_level"`
	HeavyJobsRejectedAtHigh      bool      `json:"heavy_jobs_rejected_at_high"`
	RecordingPausedAtCritical    bool      `json:"recording_paused_at_critical"`
	JournalAuditWritable         bool      `json:"journal_audit_writable"`
	AllDeclaredLoadHealthy       bool      `json:"all_declared_load_healthy"`
	ProductionTargetObserved     bool      `json:"production_target_observed"`
	ProhibitedCapabilityObserved bool      `json:"prohibited_capability_observed"`
}

// FaultEvent records one terminal orchestrator drill outcome.
type FaultEvent struct {
	RunID        string    `json:"run_id"`
	Scenario     string    `json:"scenario"`
	State        string    `json:"state"`
	OccurredAt   time.Time `json:"occurred_at"`
	EvidenceHash string    `json:"evidence_hash"`
}

// Failure records one stable terminal failure reason and evidence identity.
type Failure struct {
	Reason       string    `json:"reason"`
	OccurredAt   time.Time `json:"occurred_at"`
	EvidenceHash string    `json:"evidence_hash"`
}

// Evidence is the immutable authenticated operational readiness terminal verdict.
type Evidence struct {
	SchemaVersion           string        `json:"schema_version"`
	Identity                Identity      `json:"identity"`
	Preflight               Preflight     `json:"preflight"`
	DeclaredLoad            DeclaredLoad  `json:"declared_load"`
	FaultSchedule           FaultSchedule `json:"fault_schedule"`
	State                   TerminalState `json:"state"`
	Qualified               bool          `json:"qualified"`
	ProfitabilityEvidence   bool          `json:"profitability_evidence"`
	StartedAt               time.Time     `json:"started_at"`
	EndedAt                 time.Time     `json:"ended_at"`
	RequiredDurationSeconds int64         `json:"required_duration_seconds"`
	ObservedDurationSeconds int64         `json:"observed_duration_seconds"`
	Samples                 []Sample      `json:"samples"`
	Faults                  []FaultEvent  `json:"faults"`
	Failures                []Failure     `json:"failures"`
	EvidenceHash            string        `json:"evidence_hash"`
	SigningKeyFingerprint   string        `json:"signing_key_fingerprint"`
	Signature               string        `json:"signature"`
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var imagePattern = regexp.MustCompile(`^[^@[:space:]]+@sha256:[0-9a-f]{64}$`)

// ValidateConfig enforces the immutable identity, duration, load, and drill contract.
func ValidateConfig(configuration Config) error {
	identity := configuration.Identity
	if !configuration.Enabled || identity.RunID == "" || !commitPattern.MatchString(identity.SourceSHA) ||
		!shaPattern.MatchString(identity.ConfigurationHash) || !shaPattern.MatchString(identity.ServerIdentity) ||
		!shaPattern.MatchString(identity.DatasetIdentity) || !shaPattern.MatchString(identity.TestManifestHash) ||
		configuration.SampleInterval <= 0 || configuration.EvidenceRoot == "" ||
		len(configuration.SigningKey) != ed25519.PrivateKeySize || !configuration.DeclaredLoad.complete() ||
		validateImages(identity.ImageDigests) != nil || validateSchedule(configuration.FaultSchedule, configuration.Duration) != nil {
		return fmt.Errorf("operational_readiness_configuration_rejected")
	}
	switch identity.Mode {
	case ModeFormal:
		if configuration.Duration != FormalDuration || identity.SourceDirty ||
			configuration.SampleInterval < time.Minute || configuration.SampleInterval > 5*time.Minute {
			return fmt.Errorf("operational_readiness_formal_configuration_rejected")
		}
	case ModeSmoke:
		if configuration.Duration < time.Second || configuration.Duration > MaximumSmokeDuration ||
			configuration.SampleInterval > time.Minute {
			return fmt.Errorf("operational_readiness_smoke_configuration_rejected")
		}
	default:
		return fmt.Errorf("operational_readiness_mode_rejected")
	}
	return nil
}

func validateImages(images map[string]string) error {
	if len(images) != len(requiredImages) {
		return fmt.Errorf("operational_readiness_image_set_rejected")
	}
	for _, name := range requiredImages {
		if !imagePattern.MatchString(images[name]) {
			return fmt.Errorf("operational_readiness_image_set_rejected")
		}
	}
	return nil
}

func validateSchedule(schedule FaultSchedule, duration time.Duration) error {
	if schedule.SchemaVersion != "axiom.operationalReadiness.fault-schedule.v1" || len(schedule.Faults) < 5 || duration <= 0 {
		return fmt.Errorf("operational_readiness_fault_schedule_rejected")
	}
	seen := make(map[string]bool, len(schedule.Faults))
	var prior uint64
	for index, fault := range schedule.Faults {
		if fault.Scenario == "" || seen[fault.Scenario] ||
			time.Duration(fault.OffsetSeconds)*time.Second >= duration || (index > 0 && fault.OffsetSeconds <= prior) {
			return fmt.Errorf("operational_readiness_fault_schedule_rejected")
		}
		seen[fault.Scenario], prior = true, fault.OffsetSeconds
	}
	return nil
}

func validFailureReason(value string) bool { return slices.Contains(failureReasons, value) }
