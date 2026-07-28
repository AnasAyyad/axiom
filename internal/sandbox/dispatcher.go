package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

// OutboxState is one durable asynchronous submission state.
type OutboxState string

// Outbox states are closed and keep every nonterminal order capacity-bearing.
const (
	OutboxPending      OutboxState = "PENDING"
	OutboxClaimed      OutboxState = "CLAIMED"
	OutboxAcknowledged OutboxState = "ACKNOWLEDGED"
	OutboxUnknown      OutboxState = "UNKNOWN"
	OutboxTerminal     OutboxState = "TERMINAL"
)

// KillBoundary is the closed crash-injection surface used by C3 recovery tests.
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

// KillPoint injects deterministic crash failures at named C3 boundaries.
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

// ReservationState records whether reserved capacity remains active, was
// consumed by a fill, was safely released without a fill, or is quarantined.
type ReservationState string

// Closed reservation states mirror the durable PostgreSQL contract.
const (
	ReservationActive      ReservationState = "ACTIVE"
	ReservationConsumed    ReservationState = "CONSUMED"
	ReservationReleased    ReservationState = "RELEASED"
	ReservationQuarantined ReservationState = "QUARANTINED"
)

// ApprovedSandboxPlan atomically groups approved legs, reservations, and arm evidence.
type ApprovedSandboxPlan struct {
	ID              string
	SessionID       SessionID
	Submissions     []Submission
	Reservations    []DurableReservation
	Arm             Arm
	Eligibility     map[Exchange]EligibilitySnapshot
	EntrySafety     map[AccountID]EntrySafetySnapshot
	Pipeline        ApprovalPipelineEvidence
	ApprovedAt      time.Time
	ApprovalHash    string
	ConfigurationID string
}

// ApprovalIntentKind distinguishes typed operator canaries from natural
// eligible-strategy intents while preserving one allocation/risk/planner path.
type ApprovalIntentKind string

// Closed V1C intent sources.
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
	values = appendEligibilityHash(values, plan.Eligibility)
	values = appendEntrySafetyHash(values, plan.EntrySafety)
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
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
	}
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

func appendEligibilityHash(
	values []string,
	eligibility map[Exchange]EligibilitySnapshot,
) []string {
	exchanges := make([]string, 0, len(eligibility))
	for exchange := range eligibility {
		exchanges = append(exchanges, string(exchange))
	}
	sort.Strings(exchanges)
	for _, exchange := range exchanges {
		encoded, _ := json.Marshal(eligibility[Exchange(exchange)])
		values = append(values, exchange, string(encoded))
	}
	return values
}

func appendEntrySafetyHash(
	values []string,
	entrySafety map[AccountID]EntrySafetySnapshot,
) []string {
	accounts := make([]string, 0, len(entrySafety))
	for account := range entrySafety {
		accounts = append(accounts, string(account))
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		encoded, _ := json.Marshal(entrySafety[AccountID(account)])
		values = append(values, account, string(encoded))
	}
	return values
}

func stringUint64(value uint64) string {
	return strconv.FormatUint(value, 10)
}

// ValidateFor checks the complete approval path and source/strategy binding.
func (evidence ApprovalPipelineEvidence) ValidateFor(
	plan ApprovedSandboxPlan,
) error {
	if (evidence.IntentKind != ApprovalCanaryIntent &&
		evidence.IntentKind != ApprovalStrategyIntent) ||
		!recoveryHash(evidence.IntentHash) ||
		!recoveryHash(evidence.AllocatorHash) ||
		!recoveryHash(evidence.RiskHash) ||
		!recoveryHash(evidence.PlannerHash) ||
		!recoveryHash(evidence.AssetApprovalHash) ||
		!evidence.RiskApproved || !evidence.AssetApproved ||
		evidence.ObservedAt.IsZero() ||
		evidence.ObservedAt.Location() != time.UTC ||
		!evidence.ObservedAt.Equal(plan.ApprovedAt) ||
		evidence.HashFor(plan) != plan.ApprovalHash {
		return contractError("approval_pipeline_invalid")
	}
	for _, submission := range plan.Submissions {
		isCanary := submission.StrategyID.Value() == StrategySandboxCanary
		if (evidence.IntentKind == ApprovalCanaryIntent) != isCanary {
			return contractError("approval_pipeline_invalid")
		}
	}
	return nil
}

// RequiresEntryArm verifies that a plan has one coherent action class. Entry
// plans require the full current arm/safety gate; exit and bounded recovery
// plans remain available while entry is paused. Cancellation uses Cancel.
func RequiresEntryArm(plan ApprovedSandboxPlan) (bool, error) {
	if len(plan.Submissions) == 0 {
		return false, contractError("sandbox_plan_action_invalid")
	}
	action := plan.Submissions[0].Action
	for _, submission := range plan.Submissions {
		if submission.Action != action {
			return false, contractError("sandbox_plan_action_invalid")
		}
	}
	switch action {
	case IntentEntry:
		return true, nil
	case IntentExit, IntentRecovery:
		return false, nil
	default:
		return false, contractError("sandbox_plan_action_invalid")
	}
}

// ValidatePlanSaga freezes the existing canonical execution saga as the
// authoritative plan topology before any V1C persistence.
func ValidatePlanSaga(plan ApprovedSandboxPlan) (execution.DispatchPolicy, error) {
	if len(plan.Submissions) == 0 ||
		len(plan.Submissions) != len(plan.Reservations) ||
		plan.Submissions[0].PlanID.String() != plan.ID {
		return "", contractError("sandbox_saga_invalid")
	}
	policy := execution.DispatchSequential
	if len(plan.Submissions) == 2 {
		policy = execution.DispatchConcurrent
	}
	legs := make([]execution.SagaLeg, 0, len(plan.Submissions))
	reservations := make([]domain.ReservationID, 0, len(plan.Reservations))
	for index, submission := range plan.Submissions {
		if submission.PlanID.String() != plan.ID {
			return "", contractError("sandbox_saga_invalid")
		}
		legs = append(legs, execution.SagaLeg{
			Index: uint32(index), OrderID: submission.OrderID,
			State: execution.OrderCreated,
		})
		reservationID, err := domain.NewReservationID(plan.Reservations[index].ID)
		if err != nil {
			return "", contractError("sandbox_saga_invalid")
		}
		reservations = append(reservations, reservationID)
	}
	if _, err := execution.NewSaga(
		plan.Submissions[0].PlanID,
		policy,
		legs,
		reservations,
	); err != nil {
		return "", contractError("sandbox_saga_invalid")
	}
	return policy, nil
}

// SubmissionLimits is the fixed, non-configurable V1C capacity policy.
type SubmissionLimits struct {
	MaximumOrderNotional  string
	MaximumDailyNotional  string
	MaximumOpenPerAccount int
	MaximumOpenGlobal     int
}

// SubmissionOutbox is one fenced asynchronous delivery record.
type SubmissionOutbox struct {
	ID             string
	Submission     Submission
	State          OutboxState
	ClaimOwner     string
	FencingToken   uint64
	ClaimExpiresAt time.Time
	Attempt        uint32
	UpdatedAt      time.Time
}

// DispatcherRepository owns atomic approval, claims, inbox, and reduction.
type DispatcherRepository interface {
	ApprovePlan(context.Context, ApprovedSandboxPlan, SubmissionLimits, KillPoint) error
	ClaimOutbox(context.Context, AccountID, uint64, string, uint64, time.Time, time.Duration, int, KillPoint) ([]SubmissionOutbox, error)
	MarkSubmitting(context.Context, string, uint64, time.Time, KillPoint) error
	MarkUnknown(context.Context, string, uint64, time.Time, KillPoint) error
	MarkCancelPending(context.Context, AccountID, uint64, string, string, uint64, time.Time, KillPoint) (string, error)
	MarkCancelUnknown(context.Context, string, uint64, time.Time, KillPoint) error
	AppendPrivateEvent(context.Context, string, uint64, PrivateEvent, KillPoint) error
}
