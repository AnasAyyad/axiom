package sandbox

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

func appendEligibilityHash(
	values []string,
	eligibility map[Exchange]EligibilitySnapshot,
	marketEligibility []EligibilitySnapshot,
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
	markets := append([]EligibilitySnapshot(nil), marketEligibility...)
	sort.Slice(markets, func(left, right int) bool {
		if markets[left].Exchange != markets[right].Exchange {
			return markets[left].Exchange < markets[right].Exchange
		}
		if markets[left].Instrument != markets[right].Instrument {
			return markets[left].Instrument < markets[right].Instrument
		}
		return markets[left].ObservedAt.Before(markets[right].ObservedAt)
	})
	for _, snapshot := range markets {
		encoded, _ := json.Marshal(snapshot)
		values = append(values, "market", snapshot.Exchange, snapshot.Instrument, string(encoded))
	}
	return values
}

// EligibilityForSubmission returns the one immutable public-data eligibility
// fact for an exact venue and market. Multi-market plans must use the slice so
// a triangular cycle cannot reuse one healthy book as proof for its other two
// instruments. Existing one-market plans retain the legacy keyed form until
// the semantic schema migration is complete.
func EligibilityForSubmission(
	plan ApprovedSandboxPlan,
	submission Submission,
	exchange Exchange,
) (EligibilitySnapshot, bool) {
	if len(plan.MarketEligibility) == 0 {
		snapshot, exists := plan.Eligibility[exchange]
		return snapshot, exists
	}
	var result EligibilitySnapshot
	matches := 0
	for _, snapshot := range plan.MarketEligibility {
		if snapshot.Exchange == string(exchange) &&
			snapshot.Instrument == submission.Instrument.Symbol() {
			result = snapshot
			matches++
		}
	}
	return result, matches == 1
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

func appendAccountSnapshotHash(
	values []string,
	snapshots map[AccountID]AccountSnapshotReference,
) []string {
	accounts := make([]string, 0, len(snapshots))
	for account := range snapshots {
		accounts = append(accounts, string(account))
	}
	sort.Strings(accounts)
	for _, account := range accounts {
		reference := snapshots[AccountID(account)]
		values = append(values, account, string(reference.AccountID),
			stringUint64(reference.AccountEpoch), reference.SnapshotHash,
			reference.ObservedAt.UTC().Format(time.RFC3339Nano))
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
	if evidence.IntentKind == ApprovalStrategyIntent {
		// Older manually constructed strategy plans share this intent kind but
		// predate automatic strategy sessions. When present, evidence is always
		// bound into the approval; PostgreSQL determines whether this plan is an
		// automatic session and therefore requires it.
		if plan.StrategyDecision != nil && plan.StrategyDecision.ValidForPlan(plan) != nil {
			return contractError("approval_pipeline_invalid")
		}
	} else if plan.StrategyDecision != nil {
		return contractError("approval_pipeline_invalid")
	}
	if evidence.IntentKind == ApprovalStrategyIntent &&
		plan.Submissions[0].Action != IntentRecovery {
		for _, submission := range plan.Submissions {
			reference, exists := plan.AccountSnapshots[submission.AccountID]
			if !exists || reference.ValidateFor(
				submission.AccountID, submission.AccountEpoch, plan.ApprovedAt,
			) != nil {
				return contractError("approval_pipeline_invalid")
			}
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
// authoritative plan topology before any sandbox runtime persistence.
func ValidatePlanSaga(plan ApprovedSandboxPlan) (execution.DispatchPolicy, error) {
	policy, strategy, err := validatedSagaPolicy(plan)
	if err != nil {
		return "", err
	}
	legs, reservations, err := sandboxSagaTopology(plan, strategy)
	if err != nil {
		return "", err
	}
	if _, err = execution.NewSaga(plan.Submissions[0].PlanID, policy, legs, reservations); err != nil {
		return "", contractError("sandbox_saga_invalid")
	}
	return policy, nil
}

func validatedSagaPolicy(plan ApprovedSandboxPlan) (execution.DispatchPolicy, string, error) {
	if len(plan.Submissions) == 0 ||
		len(plan.Submissions) != len(plan.Reservations) ||
		plan.Submissions[0].PlanID.String() != plan.ID {
		return "", "", contractError("sandbox_saga_invalid")
	}
	strategy := plan.Submissions[0].StrategyID.Value()
	policy := execution.DispatchSequential
	switch strategy {
	case StrategyTrend, StrategyMeanReversion, StrategySandboxCanary:
		if len(plan.Submissions) != 1 || plan.ExecutionExpiresAt != nil {
			return "", "", contractError("sandbox_saga_invalid")
		}
	case StrategyTriangular:
		if len(plan.Submissions) != 3 || !validArbitrageExecutionExpiry(plan) {
			return "", "", contractError("sandbox_saga_invalid")
		}
	case StrategyCrossExchangeArbitrage:
		if len(plan.Submissions) != 2 || !validArbitrageExecutionExpiry(plan) {
			return "", "", contractError("sandbox_saga_invalid")
		}
		policy = execution.DispatchConcurrent
	default:
		return "", "", contractError("sandbox_saga_invalid")
	}
	return policy, strategy, nil
}

func sandboxSagaTopology(plan ApprovedSandboxPlan, strategy string) ([]execution.SagaLeg, []domain.ReservationID, error) {
	legs := make([]execution.SagaLeg, 0, len(plan.Submissions))
	reservations := make([]domain.ReservationID, 0, len(plan.Reservations))
	for index, submission := range plan.Submissions {
		if submission.PlanID.String() != plan.ID || submission.StrategyID.Value() != strategy {
			return nil, nil, contractError("sandbox_saga_invalid")
		}
		leg := execution.SagaLeg{
			Index: uint32(index), OrderID: submission.OrderID,
			State: execution.OrderCreated,
		}
		if strategy == StrategyTriangular && index > 0 {
			dependency := uint32(index - 1)
			leg.DependsOn = &dependency
		}
		legs = append(legs, leg)
		reservationID, err := domain.NewReservationID(plan.Reservations[index].ID)
		if err != nil {
			return nil, nil, contractError("sandbox_saga_invalid")
		}
		reservations = append(reservations, reservationID)
	}
	return legs, reservations, nil
}

func validArbitrageExecutionExpiry(plan ApprovedSandboxPlan) bool {
	return plan.ExecutionExpiresAt != nil &&
		!plan.ExecutionExpiresAt.Before(plan.ApprovedAt) &&
		plan.ExecutionExpiresAt.After(plan.ApprovedAt) &&
		plan.ExecutionExpiresAt.Location() == time.UTC &&
		plan.ExecutionExpiresAt.Sub(plan.ApprovedAt) <= 250*time.Millisecond
}

// SubmissionLimits is the fixed, non-configurable sandbox runtime capacity policy.
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
	LegIndex       uint32
	DependsOn      *uint32
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
