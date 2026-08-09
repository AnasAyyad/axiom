package c6

import (
	"fmt"
	"regexp"
	"slices"
	"time"
)

// FormalDuration, AlertSLO, and RecoveryRTO define the immutable formal C6
// observation window and fail-closed service objectives.
const (
	FormalDuration = 72 * time.Hour
	AlertSLO       = 5 * time.Second
	RecoveryRTO    = 2 * time.Minute
)

// Mode distinguishes the bounded smoke runner from the exact formal observer.
type Mode string

// C6 qualification modes.
const (
	ModeSmoke  Mode = "smoke"
	ModeFormal Mode = "formal"
)

// TerminalState is the immutable outcome recorded in C6 evidence.
type TerminalState string

// C6 terminal evidence states.
const (
	StateSmokePassed TerminalState = "SMOKE_PASSED"
	StatePassed      TerminalState = "PASSED"
	StateFailed      TerminalState = "FAILED"
)

var failureReasons = []string{
	"duplicate_create", "lost_fill", "double_posted_fill",
	"unresolved_unknown", "reconciliation_mismatch", "suspense",
	"stale_data", "lease_loss", "persistence_failure", "unsafe_restart",
	"production_target", "cap_violation", "memory_leak",
	"critical_alert_slo", "operator_abort", "evidence_failure",
	"recovery_expired", "recovery_repeated", "recovery_unrecoverable",
}

// AccountIdentity binds one observed sandbox account to its immutable epoch,
// credential generation, exchange, environment, and configuration.
type AccountIdentity struct {
	ID                   string `json:"id"`
	Exchange             string `json:"exchange"`
	Environment          string `json:"environment"`
	AccountEpoch         uint64 `json:"account_epoch"`
	CredentialGeneration uint64 `json:"credential_generation"`
	ConfigurationHash    string `json:"configuration_hash"`
}

// Identity binds a C6 run to exact source, build, executable, image,
// configuration, and account identities.
type Identity struct {
	RunID             string            `json:"run_id"`
	Mode              Mode              `json:"mode"`
	CommitSHA         string            `json:"commit_sha"`
	BuildHash         string            `json:"build_hash"`
	ExecutableHash    string            `json:"executable_hash"`
	ImageHash         string            `json:"image_hash,omitempty"`
	ConfigurationHash string            `json:"configuration_hash"`
	SourceDirty       bool              `json:"source_dirty"`
	Accounts          []AccountIdentity `json:"accounts"`
}

// Config contains the explicit, default-off C6 runner configuration.
type Config struct {
	Enabled        bool
	Identity       Identity
	Duration       time.Duration
	SampleInterval time.Duration
	EvidencePath   string
}

// Sample is one bounded, redacted observation of C6 safety and SLO state.
type Sample struct {
	Ordinal                    uint64               `json:"ordinal"`
	ObservedAt                 time.Time            `json:"observed_at"`
	OrdersAcknowledged         uint64               `json:"orders_acknowledged"`
	DuplicateCreates           uint64               `json:"duplicate_creates"`
	LostFills                  uint64               `json:"lost_fills"`
	DoublePostedFills          uint64               `json:"double_posted_fills"`
	UnknownOrders              uint64               `json:"unknown_orders"`
	OldestUnknownSeconds       uint64               `json:"oldest_unknown_seconds"`
	ReconciliationMismatches   uint64               `json:"reconciliation_mismatches"`
	SuspenseItems              uint64               `json:"suspense_items"`
	Reconnects                 uint64               `json:"reconnects"`
	Restarts                   uint64               `json:"restarts"`
	RecoveryDurationMillis     uint64               `json:"recovery_duration_ms"`
	CriticalAlertLatencyMillis uint64               `json:"critical_alert_latency_ms"`
	ResidentMemoryBytes        uint64               `json:"resident_memory_bytes"`
	DailySubmittedMicrounits   uint64               `json:"daily_submitted_microunits"`
	LargestOrderMicrounits     uint64               `json:"largest_order_microunits"`
	MaximumAccountOpen         uint64               `json:"maximum_account_open"`
	GlobalOpen                 uint64               `json:"global_open"`
	AllAccountsFresh           bool                 `json:"all_accounts_fresh"`
	AllLeasesHeld              bool                 `json:"all_leases_held"`
	PersistenceHealthy         bool                 `json:"persistence_healthy"`
	RestartSafe                bool                 `json:"restart_safe"`
	EntrySafe                  bool                 `json:"entry_safe"`
	ProductionTargetObserved   bool                 `json:"production_target_observed"`
	RecoveryActive             bool                 `json:"recovery_active"`
	Accounts                   []AccountObservation `json:"accounts,omitempty"`
}

// AccountObservation is the redacted per-account C6 recovery projection.
type AccountObservation struct {
	ID                  string                 `json:"id"`
	Exchange            string                 `json:"exchange"`
	Environment         string                 `json:"environment"`
	Epoch               uint64                 `json:"epoch"`
	State               string                 `json:"state"`
	StreamHealthy       bool                   `json:"stream_healthy"`
	EvidenceHealthy     bool                   `json:"evidence_healthy"`
	LeaseHeld           bool                   `json:"lease_held"`
	AccountSafe         bool                   `json:"account_safe"`
	ReconciliationClean bool                   `json:"reconciliation_clean"`
	RecoveryState       string                 `json:"recovery_state"`
	RecoveryEvent       string                 `json:"recovery_event,omitempty"`
	IncidentSource      string                 `json:"incident_source,omitempty"`
	FailureKind         string                 `json:"failure_kind,omitempty"`
	CauseCode           string                 `json:"cause_code,omitempty"`
	DeadlineAt          *time.Time             `json:"deadline_at,omitempty"`
	CleanCheckCount     uint8                  `json:"clean_check_count"`
	RecoveryTimestamp   *time.Time             `json:"recovery_timestamp,omitempty"`
	RecoveryEvents      []AccountRecoveryEvent `json:"recovery_events,omitempty"`
}

// AccountRecoveryEvent is one runtime-derived lifecycle transition awaiting
// immutable binding to the formal C6 run. It contains no raw exchange data.
type AccountRecoveryEvent struct {
	Event             string     `json:"event"`
	State             string     `json:"state"`
	IncidentSource    string     `json:"incident_source"`
	FailureKind       string     `json:"failure_kind"`
	CauseCode         string     `json:"cause_code"`
	DeadlineAt        time.Time  `json:"deadline_at"`
	CleanCheckCount   uint8      `json:"clean_check_count"`
	RecoveryTimestamp *time.Time `json:"recovery_timestamp,omitempty"`
	OccurredAt        time.Time  `json:"occurred_at"`
}

// RecoveryEvent is one immutable, redacted C6 recovery lifecycle fact.
type RecoveryEvent struct {
	RunID             string     `json:"run_id"`
	AccountID         string     `json:"account_id"`
	Exchange          string     `json:"exchange"`
	Environment       string     `json:"environment"`
	AccountEpoch      uint64     `json:"account_epoch"`
	Event             string     `json:"event"`
	State             string     `json:"state"`
	IncidentSource    string     `json:"incident_source"`
	FailureKind       string     `json:"failure_kind"`
	CauseCode         string     `json:"cause_code"`
	DeadlineAt        time.Time  `json:"deadline_at"`
	CleanCheckCount   uint8      `json:"clean_check_count"`
	RecoveryTimestamp *time.Time `json:"recovery_timestamp,omitempty"`
	EvidenceHash      string     `json:"evidence_hash"`
	OccurredAt        time.Time  `json:"occurred_at"`
}

// Failure records one stable fail-closed reason and its evidence identity.
type Failure struct {
	Reason       string    `json:"reason"`
	EvidenceHash string    `json:"evidence_hash"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// ChaosEvent records one deterministic scenario outcome without raw secrets.
type ChaosEvent struct {
	Scenario              string    `json:"scenario"`
	Outcome               string    `json:"outcome"`
	DeterministicSeedHash string    `json:"deterministic_seed_hash"`
	EvidenceHash          string    `json:"evidence_hash"`
	OccurredAt            time.Time `json:"occurred_at"`
}

// Evidence is the immutable terminal C6 qualification document.
type Evidence struct {
	SchemaVersion           string          `json:"schema_version"`
	Identity                Identity        `json:"identity"`
	State                   TerminalState   `json:"state"`
	StartedAt               time.Time       `json:"started_at"`
	EndedAt                 time.Time       `json:"ended_at"`
	RequiredDurationSeconds int64           `json:"required_duration_seconds"`
	ObservedDurationSeconds int64           `json:"observed_duration_seconds"`
	ProfitabilityEvidence   bool            `json:"profitability_evidence"`
	Qualified               bool            `json:"qualified"`
	Caps                    Caps            `json:"caps"`
	SLO                     SLO             `json:"slo"`
	Samples                 []Sample        `json:"samples"`
	RecoveryEvents          []RecoveryEvent `json:"recovery_events"`
	Chaos                   []ChaosEvent    `json:"chaos"`
	Failures                []Failure       `json:"failures"`
	EvidenceHash            string          `json:"evidence_hash"`
}

// Caps records the fixed sandbox notional, concurrency, and arm-duration
// limits checked throughout qualification.
type Caps struct {
	MaximumOrderMicrounits uint64 `json:"maximum_order_microunits"`
	MaximumDailyMicrounits uint64 `json:"maximum_daily_microunits"`
	MaximumOpenPerAccount  uint64 `json:"maximum_open_per_account"`
	MaximumOpenGlobal      uint64 `json:"maximum_open_global"`
	ArmDurationSeconds     uint64 `json:"arm_duration_seconds"`
}

// SLO summarizes terminal alert, recovery, correctness, and memory outcomes.
type SLO struct {
	CriticalAlertLatencyMillis uint64 `json:"critical_alert_latency_ms"`
	RecoveryDurationMillis     uint64 `json:"recovery_duration_ms"`
	DuplicateCreates           uint64 `json:"duplicate_creates"`
	LostFills                  uint64 `json:"lost_fills"`
	DoublePostedFills          uint64 `json:"double_posted_fills"`
	PositiveMemoryLeakTrend    bool   `json:"positive_memory_leak_trend"`
}

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	imagePattern  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ValidateConfig rejects disabled, incomplete, dirty, production-targeted, or
// otherwise non-canonical C6 runner configurations.
func ValidateConfig(configuration Config) error {
	if !configuration.Enabled || configuration.Identity.RunID == "" ||
		!commitPattern.MatchString(configuration.Identity.CommitSHA) ||
		!sha256Pattern.MatchString(configuration.Identity.BuildHash) ||
		!sha256Pattern.MatchString(configuration.Identity.ExecutableHash) ||
		!sha256Pattern.MatchString(configuration.Identity.ConfigurationHash) ||
		(configuration.Identity.ImageHash != "" &&
			!imagePattern.MatchString(configuration.Identity.ImageHash)) ||
		configuration.SampleInterval <= 0 ||
		configuration.EvidencePath == "" {
		return fmt.Errorf("c6_configuration_rejected")
	}
	switch configuration.Identity.Mode {
	case ModeFormal:
		if configuration.Duration != FormalDuration ||
			configuration.Identity.SourceDirty ||
			!imagePattern.MatchString(configuration.Identity.ImageHash) {
			return fmt.Errorf("c6_formal_configuration_rejected")
		}
	case ModeSmoke:
		if configuration.Duration < time.Second ||
			configuration.Duration > 15*time.Minute {
			return fmt.Errorf("c6_smoke_configuration_rejected")
		}
	default:
		return fmt.Errorf("c6_mode_rejected")
	}
	if len(configuration.Identity.Accounts) != 2 {
		return fmt.Errorf("c6_account_set_rejected")
	}
	exchanges := make(map[string]bool, 2)
	ids := make(map[string]bool, 2)
	for _, account := range configuration.Identity.Accounts {
		allowed := account.Exchange == "binance" &&
			account.Environment == "spot_testnet" ||
			account.Exchange == "bybit" && account.Environment == "demo"
		if !allowed || account.ID == "" || account.AccountEpoch == 0 ||
			account.CredentialGeneration == 0 ||
			!sha256Pattern.MatchString(account.ConfigurationHash) ||
			ids[account.ID] || exchanges[account.Exchange] {
			return fmt.Errorf("c6_account_set_rejected")
		}
		ids[account.ID] = true
		exchanges[account.Exchange] = true
	}
	return nil
}

func validFailureReason(value string) bool {
	return slices.Contains(failureReasons, value)
}
