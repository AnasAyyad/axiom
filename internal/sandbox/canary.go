package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
)

// CanaryIntent is the typed, explicitly requested PR2 sandbox canary input.
// V1C canaries are buy-only so the bounded runner never assumes ownership of a
// base asset that is not represented in the durable allocator.
type CanaryIntent struct {
	ID           string
	Exchange     Exchange
	AccountID    AccountID
	AccountEpoch uint64
	Instrument   domain.Instrument
	Side         domain.Side
	Quantity     domain.Quantity
	LimitPrice   domain.Price
	Style        OrderStyle
	RequestedAt  time.Time
}

// CanaryPlanIdentifiers are generated before the deterministic pipeline runs.
type CanaryPlanIdentifiers struct {
	PlanID        string
	OrderID       string
	ReservationID string
	ClientOrderID string
}

// CanaryApprovalContext contains the current arm and complete entry-admission
// facts. Callers cannot omit an enablement switch or synthesize eligibility.
type CanaryApprovalContext struct {
	SessionID                  SessionID
	Arm                        Arm
	ConfigurationID            string
	AssetApproved              bool
	GlobalIntegrationEnabled   bool
	GlobalSubmissionEnabled    bool
	ExchangeIntegrationEnabled bool
	ExchangeSubmissionEnabled  bool
	Eligibility                EligibilitySnapshot
	EntrySafety                EntrySafetySnapshot
	ApprovedAt                 time.Time
}

// BuildCanaryPlan runs one typed request through the V1C intent, allocator,
// central-risk, planner, and durable-dispatch approval contract.
func BuildCanaryPlan(
	intent CanaryIntent,
	identifiers CanaryPlanIdentifiers,
	approval CanaryApprovalContext,
) (ApprovedSandboxPlan, error) {
	notional, err := validateCanaryIntent(intent)
	if err != nil || validateCanaryApproval(intent, approval) != nil {
		return ApprovedSandboxPlan{}, contractError("canary_intent_rejected")
	}
	planID, orderID, strategyID, err := validateCanaryPlanIdentifiers(
		identifiers,
	)
	if err != nil {
		return ApprovedSandboxPlan{}, contractError("canary_planner_rejected")
	}
	submission, pipeline, reservation := buildCanaryPipeline(
		intent, identifiers, approval, notional,
		planID, orderID, strategyID,
	)
	plan := assembleCanaryPlan(
		intent, approval, planID, submission, pipeline, reservation,
	)
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan, nil
}

func validateCanaryPlanIdentifiers(
	identifiers CanaryPlanIdentifiers,
) (
	domain.ExecutionPlanID,
	domain.VirtualOrderID,
	domain.StrategyID,
	error,
) {
	planID, planErr := domain.NewExecutionPlanID(identifiers.PlanID)
	orderID, orderErr := domain.NewVirtualOrderID(identifiers.OrderID)
	strategyID, strategyErr := domain.NewStrategyID(StrategySandboxCanary)
	if planErr != nil || orderErr != nil || strategyErr != nil ||
		identifiers.ReservationID == "" ||
		identifiers.ClientOrderID == "" ||
		len(identifiers.ClientOrderID) > 64 {
		return domain.ExecutionPlanID{}, domain.VirtualOrderID{},
			domain.StrategyID{}, contractError("canary_planner_rejected")
	}
	return planID, orderID, strategyID, nil
}

func buildCanaryPipeline(
	intent CanaryIntent,
	identifiers CanaryPlanIdentifiers,
	approval CanaryApprovalContext,
	notional domain.Notional,
	planID domain.ExecutionPlanID,
	orderID domain.VirtualOrderID,
	strategyID domain.StrategyID,
) (Submission, ApprovalPipelineEvidence, DurableReservation) {
	hashes := buildCanaryPipelineHashes(intent, identifiers, approval, notional)
	submission := buildCanarySubmission(
		intent, identifiers, approval, notional, planID, orderID, strategyID,
		hashes.request, hashes.policy,
	)
	pipeline := ApprovalPipelineEvidence{
		IntentKind: ApprovalCanaryIntent, IntentHash: hashes.intent,
		AllocatorHash: hashes.allocator, RiskHash: hashes.risk,
		PlannerHash: hashes.planner, AssetApprovalHash: hashes.assetApproval,
		RiskApproved: true, AssetApproved: true,
		ObservedAt: approval.ApprovedAt,
	}
	reservation := DurableReservation{
		ID: identifiers.ReservationID, AccountID: intent.AccountID,
		AccountEpoch: intent.AccountEpoch, OrderID: orderID.String(),
		Asset: string(intent.Instrument.Quote), Quantity: notional.String(),
	}
	return submission, pipeline, reservation
}

type canaryPipelineHashes struct {
	intent, allocator, risk, planner, assetApproval, policy, request string
}

func buildCanaryPipelineHashes(
	intent CanaryIntent,
	identifiers CanaryPlanIdentifiers,
	approval CanaryApprovalContext,
	notional domain.Notional,
) canaryPipelineHashes {
	intentHash := hashCanaryValues(
		"intent", intent.ID, string(intent.Exchange), string(intent.AccountID),
		strconv.FormatUint(intent.AccountEpoch, 10), intent.Instrument.Symbol(),
		string(intent.Side), intent.Quantity.String(), intent.LimitPrice.String(),
		string(intent.Style), intent.RequestedAt.Format(time.RFC3339Nano),
	)
	reservationAsset := string(intent.Instrument.Quote)
	reservationQuantity := notional.String()
	allocatorHash := hashCanaryValues(
		"allocator", intentHash, reservationAsset, reservationQuantity,
		identifiers.ReservationID,
	)
	riskHash := hashCanaryValues(
		"risk", allocatorHash, "maximum_order_notional=10",
		"maximum_daily_notional=50", "maximum_open_per_account=1",
		"maximum_open_global=2", "all_entry_gates=true",
	)
	plannerHash := hashCanaryValues(
		"planner", riskHash, identifiers.PlanID, identifiers.OrderID,
		identifiers.ClientOrderID, string(intent.Style),
	)
	assetApprovalHash := hashCanaryValues(
		"asset_approval", approval.ConfigurationID,
		intent.Instrument.Symbol(), "approved=true",
	)
	policyHash := hashCanaryValues(
		"policy", approval.ConfigurationID, "spot_only=true",
		"canary_buy_only=true", "order_cap=10", "daily_cap=50",
	)
	requestHash := hashCanaryValues(
		"request", string(intent.Exchange), identifiers.ClientOrderID,
		intent.Instrument.Symbol(), string(intent.Side),
		intent.Quantity.String(), intent.LimitPrice.String(), string(intent.Style),
	)
	return canaryPipelineHashes{
		intent: intentHash, allocator: allocatorHash, risk: riskHash,
		planner: plannerHash, assetApproval: assetApprovalHash,
		policy: policyHash, request: requestHash,
	}
}

func buildCanarySubmission(
	intent CanaryIntent,
	identifiers CanaryPlanIdentifiers,
	approval CanaryApprovalContext,
	notional domain.Notional,
	planID domain.ExecutionPlanID,
	orderID domain.VirtualOrderID,
	strategyID domain.StrategyID,
	requestHash string,
	policyHash string,
) Submission {
	return Submission{
		PlanID: planID, OrderID: orderID,
		AccountID: intent.AccountID, AccountEpoch: intent.AccountEpoch,
		ClientOrderID: identifiers.ClientOrderID, StrategyID: strategyID,
		Instrument: intent.Instrument, Side: intent.Side,
		Quantity: intent.Quantity, LimitPrice: intent.LimitPrice,
		Notional: notional, Style: intent.Style, Action: IntentEntry,
		RequestHash: requestHash, PolicyHash: policyHash,
		ApprovedAt: approval.ApprovedAt,
	}
}

func assembleCanaryPlan(
	intent CanaryIntent,
	approval CanaryApprovalContext,
	planID domain.ExecutionPlanID,
	submission Submission,
	pipeline ApprovalPipelineEvidence,
	reservation DurableReservation,
) ApprovedSandboxPlan {
	return ApprovedSandboxPlan{
		ID: planID.String(), SessionID: approval.SessionID,
		Submissions:  []Submission{submission},
		Reservations: []DurableReservation{reservation},
		Arm:          approval.Arm,
		Eligibility: map[Exchange]EligibilitySnapshot{
			intent.Exchange: approval.Eligibility,
		},
		EntrySafety: map[AccountID]EntrySafetySnapshot{
			intent.AccountID: approval.EntrySafety,
		},
		Pipeline: pipeline, ApprovedAt: approval.ApprovedAt,
		ConfigurationID: approval.ConfigurationID,
	}
}

func validateCanaryIntent(intent CanaryIntent) (domain.Notional, error) {
	maximum, _ := domain.ParseNotional("10")
	zero, _ := domain.ParseNotional("0")
	notional, err := domain.CalculateNotional(
		intent.LimitPrice,
		intent.Quantity,
		18,
	)
	if err != nil || intent.ID == "" ||
		(intent.Exchange != ExchangeBinance && intent.Exchange != ExchangeBybit) ||
		intent.AccountID == "" || intent.AccountEpoch == 0 ||
		intent.Instrument.Quote != "USDT" ||
		intent.Side != domain.SideBuy ||
		!validOrderStyle(intent.Style) ||
		intent.RequestedAt.IsZero() ||
		intent.RequestedAt.Location() != time.UTC ||
		notional.Compare(zero) <= 0 || notional.Compare(maximum) > 0 {
		return domain.Notional{}, contractError("canary_intent_rejected")
	}
	return notional, nil
}

func validateCanaryApproval(
	intent CanaryIntent,
	approval CanaryApprovalContext,
) error {
	if approval.SessionID == "" || approval.ConfigurationID == "" ||
		!approval.AssetApproved ||
		!approval.GlobalIntegrationEnabled ||
		!approval.GlobalSubmissionEnabled ||
		!approval.ExchangeIntegrationEnabled ||
		!approval.ExchangeSubmissionEnabled ||
		approval.ApprovedAt.IsZero() ||
		approval.ApprovedAt.Location() != time.UTC ||
		!approval.Arm.Active(approval.ApprovedAt) ||
		approval.Arm.SessionID != approval.SessionID ||
		len(approval.Arm.AccountIDs) != 1 ||
		approval.Arm.AccountIDs[0] != intent.AccountID ||
		!approval.Eligibility.Eligible ||
		approval.Eligibility.Exchange != string(intent.Exchange) ||
		approval.Eligibility.Instrument != intent.Instrument.Symbol() ||
		approval.Eligibility.ObservedAt.After(approval.ApprovedAt) ||
		approval.ApprovedAt.Sub(approval.Eligibility.ObservedAt) >
			250*time.Millisecond ||
		approval.EntrySafety.ValidateFor(
			Submission{
				AccountID:    intent.AccountID,
				AccountEpoch: intent.AccountEpoch,
			},
			intent.Exchange,
			approval.ApprovedAt,
		) != nil {
		return contractError("canary_approval_rejected")
	}
	return nil
}

func hashCanaryValues(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}
