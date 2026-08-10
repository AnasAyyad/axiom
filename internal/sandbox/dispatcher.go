package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// OutboxState is one durable asynchronous submission state.
type OutboxState string

// Outbox states are closed and keep every nonterminal order capacity-bearing.
const (
	OutboxWaiting      OutboxState = "WAITING"
	OutboxPending      OutboxState = "PENDING"
	OutboxClaimed      OutboxState = "CLAIMED"
	OutboxAcknowledged OutboxState = "ACKNOWLEDGED"
	OutboxUnknown      OutboxState = "UNKNOWN"
	OutboxTerminal     OutboxState = "TERMINAL"
)

// KillBoundary is the closed crash-injection surface used by dispatcher recovery recovery tests.
type KillBoundary string

// Kill boundaries cover every durable and external dispatch transition.
const (
	KillBeforePlanCommit         KillBoundary = "before_plan_commit"
	KillAfterPlanCommit          KillBoundary = "after_plan_commit"
	KillBeforeNetworkAttempt     KillBoundary = "before_network_attempt"
	KillAfterNetworkAttempt      KillBoundary = "after_network_attempt"
	KillBeforeAcknowledgement    KillBoundary = "before_acknowledgement"
	KillAfterAcknowledgement     KillBoundary = "after_acknowledgement"
	KillBeforeInboxAppend        KillBoundary = "before_inbox_append"
	KillAfterInboxAppend         KillBoundary = "after_inbox_append"
	KillBeforeInboxCommit        KillBoundary = "before_inbox_commit"
	KillAfterInboxCommit         KillBoundary = "after_inbox_commit"
	KillBeforeReducerUpdate      KillBoundary = "before_reducer_update"
	KillAfterReducerUpdate       KillBoundary = "after_reducer_update"
	KillBeforeReductionCommit    KillBoundary = "before_reduction_commit"
	KillAfterReductionCommit     KillBoundary = "after_reduction_commit"
	KillBeforeFillPosting        KillBoundary = "before_fill_posting"
	KillAfterFillPosting         KillBoundary = "after_fill_posting"
	KillBeforeReservationRelease KillBoundary = "before_reservation_release"
	KillAfterReservationRelease  KillBoundary = "after_reservation_release"
	KillBeforeLeaseTransition    KillBoundary = "before_lease_transition"
	KillAfterLeaseTransition     KillBoundary = "after_lease_transition"
)

// KillPoint injects deterministic crash failures at named dispatcher recovery boundaries.
type KillPoint interface {
	Hit(context.Context, KillBoundary) error
}

// NoKillPoint disables deterministic crash injection.
type NoKillPoint struct{}

// Hit accepts every boundary without injecting a crash.
func (NoKillPoint) Hit(context.Context, KillBoundary) error { return nil }

// DurableReservation holds the exact asset required by one submission leg.
type DurableReservation struct {
	ID            string
	AccountID     AccountID
	AccountEpoch  uint64
	OrderID       string
	Asset         string
	Quantity      string
	State         ReservationState
	ReleasedAt    *time.Time
	ReleaseReason string
}

// ReservationState records whether reserved capacity is waiting on a prior
// fill, remains active, was consumed, was safely released, or is quarantined.
type ReservationState string

// Closed reservation states mirror the durable PostgreSQL contract.
const (
	ReservationWaiting     ReservationState = "WAITING"
	ReservationActive      ReservationState = "ACTIVE"
	ReservationConsumed    ReservationState = "CONSUMED"
	ReservationReleased    ReservationState = "RELEASED"
	ReservationQuarantined ReservationState = "QUARANTINED"
)

// ApprovedSandboxPlan atomically groups approved legs, reservations, and arm evidence.
type ApprovedSandboxPlan struct {
	ID                 string
	SessionID          SessionID
	Submissions        []Submission
	Reservations       []DurableReservation
	Arm                Arm
	Eligibility        map[Exchange]EligibilitySnapshot
	MarketEligibility  []EligibilitySnapshot
	EntrySafety        map[AccountID]EntrySafetySnapshot
	AccountSnapshots   map[AccountID]AccountSnapshotReference
	StrategyDecision   *StrategyDecisionEvidence
	Pipeline           ApprovalPipelineEvidence
	ApprovedAt         time.Time
	ExecutionExpiresAt *time.Time
	ApprovalHash       string
	ConfigurationID    string
}

// AccountSnapshotReference binds an automatic strategy plan to the exact
// recently recorded exchange-account view that proved its current inventory.
// It contains no balances, credentials, or private provider payload.
type AccountSnapshotReference struct {
	AccountID    AccountID
	AccountEpoch uint64
	SnapshotHash string
	ObservedAt   time.Time
}

// ValidateFor verifies an immutable snapshot reference for one exact account
// and decision instant. Database persistence verifies the hash exists.
func (reference AccountSnapshotReference) ValidateFor(
	account AccountID,
	epoch uint64,
	approvedAt time.Time,
) error {
	if reference.AccountID != account || reference.AccountEpoch != epoch ||
		!hash256(reference.SnapshotHash) || reference.ObservedAt.IsZero() ||
		reference.ObservedAt.Location() != time.UTC ||
		reference.ObservedAt.After(approvedAt) ||
		approvedAt.Sub(reference.ObservedAt) > 250*time.Millisecond {
		return contractError("account_snapshot_reference_invalid")
	}
	return nil
}

// ApprovalIntentKind distinguishes typed operator canaries from natural
// eligible-strategy intents while preserving one allocation/risk/planner path.
type ApprovalIntentKind string

// Closed sandbox runtime intent sources.
const (
	ApprovalCanaryIntent   ApprovalIntentKind = "CANARY"
	ApprovalStrategyIntent ApprovalIntentKind = "STRATEGY"
)

// ApprovalPipelineEvidence proves that a submission reached the durable
// dispatcher through intent, allocator, central risk, planner, and current
// asset approval. It contains hashes only, never private order values.
type ApprovalPipelineEvidence struct {
	IntentKind        ApprovalIntentKind
	IntentHash        string
	AllocatorHash     string
	RiskHash          string
	PlannerHash       string
	AssetApprovalHash string
	RiskApproved      bool
	AssetApproved     bool
	ObservedAt        time.Time
}

// HashFor returns the canonical pipeline approval bound to the exact plan,
// session, configuration, order identities, and redacted request hashes.
func (evidence ApprovalPipelineEvidence) HashFor(plan ApprovedSandboxPlan) string {
	values := evidence.approvalHashBase(plan)
	values = appendApprovalArmHash(values, plan.Arm)
	for _, submission := range plan.Submissions {
		values = appendSubmissionHash(values, submission)
	}
	for _, reservation := range plan.Reservations {
		values = appendReservationHash(values, reservation)
	}
	values = appendEligibilityHash(values, plan.Eligibility, plan.MarketEligibility)
	values = appendEntrySafetyHash(values, plan.EntrySafety)
	values = appendAccountSnapshotHash(values, plan.AccountSnapshots)
	values = appendStrategyDecisionHash(values, plan.StrategyDecision)
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func appendStrategyDecisionHash(
	values []string,
	evidence *StrategyDecisionEvidence,
) []string {
	if evidence == nil {
		return append(values, "strategy-decision:none")
	}
	return append(values, "strategy-decision", string(evidence.SessionID),
		string(evidence.AccountID), stringUint64(evidence.AccountEpoch), evidence.Strategy,
		evidence.Instrument, evidence.DecisionID, stringUint64(evidence.EventOrdinal),
		stringUint64(evidence.EventLogicalTime), evidence.InputHash, evidence.DecisionHash)
}

func (evidence ApprovalPipelineEvidence) approvalHashBase(
	plan ApprovedSandboxPlan,
) []string {
	return []string{
		string(evidence.IntentKind),
		evidence.IntentHash,
		evidence.AllocatorHash,
		evidence.RiskHash,
		evidence.PlannerHash,
		evidence.AssetApprovalHash,
		evidence.ObservedAt.UTC().Format(time.RFC3339Nano),
		plan.ID,
		string(plan.SessionID),
		plan.ConfigurationID,
		plan.ApprovedAt.UTC().Format(time.RFC3339Nano),
		planExecutionExpiry(plan),
	}
}

func planExecutionExpiry(plan ApprovedSandboxPlan) string {
	if plan.ExecutionExpiresAt == nil {
		return ""
	}
	return plan.ExecutionExpiresAt.UTC().Format(time.RFC3339Nano)
}

func appendApprovalArmHash(values []string, arm Arm) []string {
	values = append(values,
		arm.ID,
		string(arm.SessionID),
		arm.AuthorizationHash,
		arm.ActorUserID,
		arm.ActorSessionID,
		arm.ReasonHash,
		arm.CreatedAt.UTC().Format(time.RFC3339Nano),
		arm.ExpiresAt.UTC().Format(time.RFC3339Nano),
		stringUint64(arm.Revision),
	)
	armAccounts := make([]string, 0, len(arm.AccountIDs))
	for _, account := range arm.AccountIDs {
		armAccounts = append(armAccounts, string(account))
	}
	sort.Strings(armAccounts)
	values = append(values, armAccounts...)
	if arm.RevokedAt != nil {
		values = append(values, arm.RevokedAt.UTC().Format(time.RFC3339Nano))
	}
	return values
}

func appendSubmissionHash(values []string, submission Submission) []string {
	return append(
		values,
		submission.PlanID.String(),
		submission.OrderID.String(),
		string(submission.AccountID),
		stringUint64(submission.AccountEpoch),
		submission.ClientOrderID,
		submission.StrategyID.String(),
		submission.Instrument.Symbol(),
		string(submission.Side),
		submission.Quantity.String(),
		submission.LimitPrice.String(),
		submission.Notional.String(),
		string(submission.Style),
		string(submission.Action),
		submission.RequestHash,
		submission.PolicyHash,
		submission.ApprovedAt.UTC().Format(time.RFC3339Nano),
	)
}

func appendReservationHash(
	values []string,
	reservation DurableReservation,
) []string {
	releasedAt := ""
	if reservation.ReleasedAt != nil {
		releasedAt = reservation.ReleasedAt.UTC().Format(time.RFC3339Nano)
	}
	return append(
		values,
		reservation.ID,
		string(reservation.AccountID),
		stringUint64(reservation.AccountEpoch),
		reservation.OrderID,
		reservation.Asset,
		reservation.Quantity,
		string(reservation.State),
		releasedAt,
		reservation.ReleaseReason,
	)
}
