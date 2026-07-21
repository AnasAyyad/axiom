package risk

import (
	"time"

	"axiom/internal/domain"
)

// State is one persisted central risk posture.
type State string

// Supported risk states in increasing restriction order.
const (
	StateNormal   State = "NORMAL"
	StateCautious State = "CAUTIOUS"
	StatePaused   State = "PAUSED"
	StateLocked   State = "LOCKED"
)

// Intent identifies one closed risk permission category.
type Intent string

// Supported intent categories.
const (
	IntentEntry          Intent = "ENTRY"
	IntentExit           Intent = "EXIT"
	IntentCancel         Intent = "CANCEL"
	IntentRecovery       Intent = "RECOVERY"
	IntentReconciliation Intent = "RECONCILIATION"
)

// Action is one stable central-risk result.
type Action string

// Supported risk actions.
const (
	ActionApprove         Action = "approve"
	ActionReject          Action = "reject"
	ActionPauseStrategy   Action = "pause_strategy"
	ActionPauseInstrument Action = "pause_instrument"
	ActionPauseExchange   Action = "pause_exchange"
	ActionLockEngine      Action = "lock_engine"
	ActionQuarantine      Action = "quarantine"
)

// ScopeKind is one persisted policy hierarchy dimension.
type ScopeKind string

// Required six A9 policy scopes.
const (
	ScopeGlobal     ScopeKind = "global"
	ScopeAccount    ScopeKind = "exchange_account"
	ScopeExchange   ScopeKind = "exchange"
	ScopeStrategy   ScopeKind = "strategy"
	ScopePortfolio  ScopeKind = "portfolio"
	ScopeAsset      ScopeKind = "asset"
	ScopeInstrument ScopeKind = "instrument"
)

// CautiousControls prove all mandatory CAUTIOUS entry reductions upstream.
type CautiousControls struct {
	ReducedSize        bool
	StricterEdge       bool
	InstrumentEligible bool
}

// Scope identifies one concrete policy owner.
type Scope struct {
	Kind ScopeKind
	ID   string
}

// Limits contains every active conservative V1A threshold.
type Limits struct {
	AccountDrawdown   domain.Percent
	DayLoss           domain.Percent
	RollingLoss       domain.Percent
	StrategyLoss      domain.Percent
	AssetExposure     domain.Percent
	CombinedExposure  domain.Percent
	ExchangeExposure  domain.Percent
	MinimumReserve    domain.Percent
	ReservedCapital   domain.Percent
	MaximumSpread     domain.Percent
	MaximumSlippage   domain.Percent
	MaximumOpenOrders uint32
	MaximumBookAge    time.Duration
	MaximumQueueLag   time.Duration
	MaximumClockDrift time.Duration
	MinimumQuality    uint8
}

// Policy is one versioned scope contribution.
type Policy struct {
	ID      string
	Version uint64
	Scope   Scope
	State   State
	Limits  Limits
}

// HealthInputs contains mandatory hard circuit-breaker facts.
type HealthInputs struct {
	Gap                 *bool
	StaleData           *bool
	ReconciliationFault *bool
	AccountingFault     *bool
	UnknownOrder        *bool
	PersistenceFault    *bool
	DiskFault           *bool
	APIError            *bool
	LeaseLost           *bool
}

// Observations are exact current and projected risk inputs.
type Observations struct {
	AccountDrawdown   *domain.Percent
	UTCDayLoss        *domain.Percent
	Rolling24HourLoss *domain.Percent
	StrategyLoss      *domain.Percent
	AssetExposure     *domain.Percent
	CombinedExposure  *domain.Percent
	ExchangeExposure  *domain.Percent
	Reserve           *domain.Percent
	ReservedCapital   *domain.Percent
	Spread            *domain.Percent
	Slippage          *domain.Percent
	OpenOrders        *uint32
	BookAge           *time.Duration
	QueueLag          *time.Duration
	ClockDrift        *time.Duration
	QualityScore      *uint8
	Health            HealthInputs
}

// Request is one immutable risk evaluation input.
type Request struct {
	Intent               Intent
	RiskReducing         bool
	LockedPolicyApproved bool
	Cautious             CautiousControls
	Policies             []Policy
	Observations         Observations
	EvaluatedAt          time.Time
}

// Decision stores all contributing policy identities and stable output.
type Decision struct {
	Action          Action
	ReasonCode      string
	EffectiveState  State
	ContributingIDs []string
	PolicyVersions  []uint64
	EvaluatedAt     time.Time
}

// AuditEvent is one immutable state/evaluation evidence fact.
type AuditEvent struct {
	Type       string
	ReasonCode string
	Prior      State
	Next       State
	Actor      string
	At         time.Time
}

// AuditSink persists immutable risk transition evidence.
type AuditSink interface{ Append(AuditEvent) error }

// AlertSink emits a bounded stable circuit-breaker alert.
type AlertSink interface {
	Emit(string, Action, State) error
}
