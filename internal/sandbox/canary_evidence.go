package sandbox

import "time"

// CanaryEvidenceStage is one immutable sandbox connectivity canary lifecycle fact.
type CanaryEvidenceStage string

// Immutable sandbox connectivity canary lifecycle stages.
const (
	// CanaryPlanApproved records durable approval through the full pipeline.
	CanaryPlanApproved CanaryEvidenceStage = "PLAN_APPROVED"
	// CanaryQuerySucceeded records an authoritative post-create query.
	CanaryQuerySucceeded CanaryEvidenceStage = "QUERY_SUCCEEDED"
	// CanaryCancelOrFillConfirmed records terminal cancellation or fill state.
	CanaryCancelOrFillConfirmed CanaryEvidenceStage = "CANCEL_OR_FILL_CONFIRMED"
	// CanaryReconciled records a clean authoritative reconciliation.
	CanaryReconciled CanaryEvidenceStage = "RECONCILED"
	// CanaryRestartVerified records the post-restart duplicate-submission proof.
	CanaryRestartVerified CanaryEvidenceStage = "RESTART_VERIFIED"
)

// CanarySessionCommand creates one still-paused session around a single
// current account epoch. It contains no order values or authentication factor.
type CanarySessionCommand struct {
	ID              SessionID
	Exchange        Exchange
	Instrument      string
	ConfigurationID string
	StrategySetHash string
	CreatedBy       string
	CreatedAt       time.Time
}

// CanarySession identifies the account epoch and startup cycle admitted into a
// new manually armable canary session.
type CanarySession struct {
	ID           SessionID
	AccountID    AccountID
	AccountEpoch uint64
	Exchange     Exchange
	StartupCycle uint64
	Revision     uint64
}

// CanaryEvidence appends one hash-only lifecycle fact.
type CanaryEvidence struct {
	CanaryID     string
	Exchange     Exchange
	AccountID    AccountID
	AccountEpoch uint64
	SessionID    SessionID
	PlanID       string
	Stage        CanaryEvidenceStage
	StartupCycle uint64
	FactHash     string
	ObservedAt   time.Time
}

// CanaryOrderStatus is the redacted durable order projection used for bounded
// runner polling.
type CanaryOrderStatus struct {
	CanaryID        string
	Exchange        Exchange
	AccountID       AccountID
	AccountEpoch    uint64
	SessionID       SessionID
	PlanID          string
	ConfigurationID string
	ClientOrderID   string
	OutboxState     OutboxState
	OrderState      string
	Attempt         uint32
	ApprovedAt      time.Time
}

// CanaryEvidenceRecord is one immutable export row.
type CanaryEvidenceRecord struct {
	ID           string              `json:"id"`
	CanaryID     string              `json:"canary_id"`
	Exchange     Exchange            `json:"exchange"`
	AccountID    AccountID           `json:"account_id"`
	AccountEpoch uint64              `json:"account_epoch"`
	SessionID    SessionID           `json:"sandbox_session_id"`
	PlanID       string              `json:"plan_id"`
	Stage        CanaryEvidenceStage `json:"stage"`
	StartupCycle uint64              `json:"startup_cycle"`
	EvidenceHash string              `json:"evidence_hash"`
	ObservedAt   time.Time           `json:"observed_at"`
}
