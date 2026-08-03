package sandbox

import (
	"errors"
	"sync"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
)

// EngineState is one fail-closed authenticated sandbox engine state.
type EngineState string

// Engine states never imply automatic submission readiness.
const (
	EngineLocked      EngineState = "LOCKED"
	EngineReadyPaused EngineState = "READY_PAUSED"
	EngineArmed       EngineState = "ARMED"
	EngineDegraded    EngineState = "DEGRADED"
	EngineQuarantined EngineState = "QUARANTINED"
)

// StartupStage is one ordered recovery prerequisite.
type StartupStage string

// Startup stages form the exact locked-to-ready-paused sequence.
const (
	StartupAcquireLease                 StartupStage = "acquire_lease"
	StartupEnterLocked                  StartupStage = "enter_locked"
	StartupValidateBuildConfiguration   StartupStage = "validate_build_configuration"
	StartupValidateCredentialGeneration StartupStage = "validate_credential_account_generation"
	StartupRecoverOutboxInbox           StartupStage = "recover_outbox_inbox"
	StartupLoadBalancesOrdersFills      StartupStage = "load_balances_orders_fills"
	StartupResolveUnknownOrders         StartupStage = "resolve_unknown_orders"
	StartupReconcileJournalReservations StartupStage = "reconcile_journal_reservations"
	StartupSynchronizeFiltersBookClock  StartupStage = "synchronize_filters_book_clock"
	StartupStartPrivateStream           StartupStage = "start_private_stream"
	StartupEnterReadyPaused             StartupStage = "enter_ready_paused"
)

var sandboxStartupStages = [...]StartupStage{
	StartupAcquireLease,
	StartupEnterLocked,
	StartupValidateBuildConfiguration,
	StartupValidateCredentialGeneration,
	StartupRecoverOutboxInbox,
	StartupLoadBalancesOrdersFills,
	StartupResolveUnknownOrders,
	StartupReconcileJournalReservations,
	StartupSynchronizeFiltersBookClock,
	StartupStartPrivateStream,
	StartupEnterReadyPaused,
}

// StartupObservation contains bounded evidence for one completed startup stage.
type StartupObservation struct {
	At                           time.Time
	LeaseHeld                    bool
	BuildValid                   bool
	ConfigurationValid           bool
	CredentialValid              bool
	AccountGenerationOK          bool
	OutboxRecovered              bool
	InboxRecovered               bool
	ExchangeStateLoaded          bool
	UnknownResolvedOrQuarantined bool
	JournalReconciled            bool
	ReservationsReconciled       bool
	Eligibility                  EligibilitySnapshot
	FiltersSynchronized          bool
	PrivateStreamHealthy         bool
	EvidenceHealthy              bool
}

// StartupGate records the exact C3 startup order. It never arms entry after
// recovery; successful completion stops at READY_PAUSED.
type StartupGate struct {
	mutex     sync.Mutex
	exchange  Exchange
	sink      exchangecontracts.LifecycleEvidenceSink
	completed int
	state     EngineState
	cycle     uint64
}

// NewStartupGate constructs an ordered fail-closed startup evidence gate.
func NewStartupGate(
	exchange Exchange,
	sink exchangecontracts.LifecycleEvidenceSink,
	cycle uint64,
) (*StartupGate, error) {
	if (exchange != ExchangeBinance && exchange != ExchangeBybit) || sink == nil || cycle == 0 {
		return nil, contractError("startup_gate_invalid")
	}
	return &StartupGate{exchange: exchange, sink: sink, state: EngineLocked, cycle: cycle}, nil
}

// Complete records exactly the next valid startup stage.
func (gate *StartupGate) Complete(stage StartupStage, observation StartupObservation) error {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.completed >= len(sandboxStartupStages) || sandboxStartupStages[gate.completed] != stage ||
		observation.At.IsZero() || observation.At.Location() != time.UTC ||
		!startupObservationAccepts(stage, observation) {
		return contractError("startup_stage_rejected")
	}
	evidence := exchangecontracts.CollectorLifecycleEvidence{
		ObservedAt:     observation.At,
		Exchange:       string(gate.exchange),
		Instrument:     "account",
		Cycle:          gate.cycle,
		Attempt:        1,
		Phase:          "sandbox_startup",
		Stage:          string(stage),
		Action:         exchangecontracts.RecoveryReconnect,
		Attribution:    exchangecontracts.AttributionRecovered,
		ReachedHealthy: stage == StartupEnterReadyPaused,
	}
	if err := gate.sink.AppendCollectorLifecycle(evidence); err != nil {
		gate.state = EngineLocked
		return contractError("startup_evidence_failed")
	}
	gate.completed++
	if stage == StartupEnterReadyPaused {
		gate.state = EngineReadyPaused
	}
	return nil
}

func startupObservationAccepts(stage StartupStage, value StartupObservation) bool {
	switch stage {
	case StartupAcquireLease:
		return value.LeaseHeld
	case StartupEnterLocked:
		return value.LeaseHeld
	case StartupValidateBuildConfiguration:
		return value.LeaseHeld && value.BuildValid && value.ConfigurationValid
	case StartupValidateCredentialGeneration:
		return value.LeaseHeld && value.CredentialValid && value.AccountGenerationOK
	case StartupRecoverOutboxInbox:
		return value.LeaseHeld && value.OutboxRecovered && value.InboxRecovered
	case StartupLoadBalancesOrdersFills:
		return value.LeaseHeld && value.ExchangeStateLoaded
	case StartupResolveUnknownOrders:
		return value.LeaseHeld && value.UnknownResolvedOrQuarantined
	case StartupReconcileJournalReservations:
		return value.LeaseHeld && value.JournalReconciled && value.ReservationsReconciled
	case StartupSynchronizeFiltersBookClock:
		return value.LeaseHeld && value.FiltersSynchronized && value.Eligibility.Eligible
	case StartupStartPrivateStream:
		return value.LeaseHeld && value.PrivateStreamHealthy
	case StartupEnterReadyPaused:
		return value.LeaseHeld && value.BuildValid && value.ConfigurationValid &&
			value.CredentialValid && value.AccountGenerationOK && value.OutboxRecovered &&
			value.InboxRecovered && value.ExchangeStateLoaded && value.UnknownResolvedOrQuarantined &&
			value.JournalReconciled && value.ReservationsReconciled && value.FiltersSynchronized &&
			value.Eligibility.Eligible && value.PrivateStreamHealthy && value.EvidenceHealthy
	default:
		return false
	}
}

// State returns the current fail-closed engine state.
func (gate *StartupGate) State() EngineState {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	return gate.state
}

// SandboxStartupSequence returns a defensive copy of the required stage order.
func SandboxStartupSequence() []StartupStage {
	result := make([]StartupStage, len(sandboxStartupStages))
	copy(result, sandboxStartupStages[:])
	return result
}

// EntrySafetySnapshot is the complete entry-admission decision boundary.
type EntrySafetySnapshot struct {
	AccountID                  AccountID   `json:"account_id"`
	AccountEpoch               uint64      `json:"account_epoch"`
	Exchange                   Exchange    `json:"exchange"`
	ObservedAt                 time.Time   `json:"observed_at"`
	State                      EngineState `json:"state"`
	ArmActive                  bool        `json:"arm_active"`
	GlobalIntegrationEnabled   bool        `json:"global_integration_enabled"`
	GlobalSubmissionEnabled    bool        `json:"global_submission_enabled"`
	ExchangeIntegrationEnabled bool        `json:"exchange_integration_enabled"`
	ExchangeSubmissionEnabled  bool        `json:"exchange_submission_enabled"`
	PublicEligible             bool        `json:"public_eligible"`
	PrivateStreamHealthy       bool        `json:"private_stream_healthy"`
	AccountStateFresh          bool        `json:"account_state_fresh"`
	ReconciliationClean        bool        `json:"reconciliation_clean"`
	LeaseHeld                  bool        `json:"lease_held"`
	EvidenceHealthy            bool        `json:"evidence_healthy"`
	OpenCapacityAvailable      bool        `json:"open_capacity_available"`
	DailyCapacityAvailable     bool        `json:"daily_capacity_available"`
}

// ErrEntryBlocked reports a fail-closed entry-admission rejection.
var ErrEntryBlocked = errors.New("sandbox_entry_blocked")

// CanSubmitEntry accepts only an armed engine with every safety gate healthy.
func (snapshot EntrySafetySnapshot) CanSubmitEntry() error {
	if snapshot.AccountID == "" || snapshot.AccountEpoch == 0 ||
		(snapshot.Exchange != ExchangeBinance && snapshot.Exchange != ExchangeBybit) ||
		snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.Location() != time.UTC ||
		snapshot.State != EngineArmed || !snapshot.ArmActive ||
		!snapshot.GlobalIntegrationEnabled || !snapshot.GlobalSubmissionEnabled ||
		!snapshot.ExchangeIntegrationEnabled || !snapshot.ExchangeSubmissionEnabled ||
		!snapshot.PublicEligible ||
		!snapshot.PrivateStreamHealthy || !snapshot.AccountStateFresh ||
		!snapshot.ReconciliationClean || !snapshot.LeaseHeld || !snapshot.EvidenceHealthy ||
		!snapshot.OpenCapacityAvailable || !snapshot.DailyCapacityAvailable {
		return ErrEntryBlocked
	}
	return nil
}

// ValidateFor binds one complete entry-admission decision to the exact account,
// epoch, exchange, and approval instant of a durable submission.
func (snapshot EntrySafetySnapshot) ValidateFor(
	submission Submission,
	exchange Exchange,
	approvedAt time.Time,
) error {
	if snapshot.AccountID != submission.AccountID ||
		snapshot.AccountEpoch != submission.AccountEpoch ||
		snapshot.Exchange != exchange ||
		!snapshot.ObservedAt.Equal(approvedAt) ||
		snapshot.CanSubmitEntry() != nil {
		return ErrEntryBlocked
	}
	return nil
}

// CanCancel deliberately ignores entry-only gates.
func (snapshot EntrySafetySnapshot) CanCancel() bool { return snapshot.LeaseHeld }
