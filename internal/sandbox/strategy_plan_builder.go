package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"axiom/internal/domain"
)

// SingleVenueStrategyPlanBuilder converts the real shared-pipeline output for
// Trend Following or Mean Reversion into one durable Testnet/Demo entry or
// exit. Multi-leg triangular and cross-exchange strategies are rejected here
// until their atomic leg builders can preserve their separate reservations and
// recovery semantics.
type SingleVenueStrategyPlanBuilder struct {
	admission StrategySessionAdmission
	snapshot  AccountSnapshot
	inventory StrategyOwnedInventory
}

// NewSingleVenueStrategyPlanBuilder binds one exact decision-time admission to
// a plan builder. The admission is revalidated before every generated plan.
func NewSingleVenueStrategyPlanBuilder(
	admission StrategySessionAdmission,
	snapshot AccountSnapshot,
	inventory StrategyOwnedInventory,
) (*SingleVenueStrategyPlanBuilder, error) {
	if admission.Valid() != nil || !singleVenueStrategy(admission.Work.Strategy) ||
		!validStrategyPlanSnapshot(admission, snapshot) ||
		inventory.ValidFor(admission, strategyInstrumentBase(admission.Work.Instrument)) != nil {
		return nil, contractError("strategy_plan_builder_invalid")
	}
	return &SingleVenueStrategyPlanBuilder{admission: admission, snapshot: snapshot,
		inventory: inventory}, nil
}

func strategyInstrumentBase(instrument string) domain.AssetSymbol {
	if instrument == "BTCUSDT" {
		return "BTC"
	}
	if instrument == "ETHUSDT" {
		return "ETH"
	}
	return ""
}

func validStrategyPlanSnapshot(
	admission StrategySessionAdmission,
	snapshot AccountSnapshot,
) bool {
	return snapshot.Validate() == nil &&
		snapshot.AccountID == admission.Work.Account.ID &&
		snapshot.Epoch == admission.Work.Account.Epoch &&
		!snapshot.ObservedAt.After(admission.ApprovedAt) &&
		admission.ApprovedAt.Sub(snapshot.ObservedAt) <= 250*time.Millisecond
}

func singleVenueStrategy(strategy string) bool {
	return strategy == StrategyTrend || strategy == StrategyMeanReversion
}

// BuildStrategyPlan preserves the exact shared-pipeline plan identity and
// binds it to the current arm, eligibility, and entry-safety facts. It uses
// IOC for automatic one-leg entries so an unattended order cannot remain open
// beyond the evaluation that authorized it.
func (builder *SingleVenueStrategyPlanBuilder) BuildStrategyPlan(
	_ context.Context,
	material StrategyPipelineMaterial,
) (ApprovedSandboxPlan, error) {
	if builder == nil || builder.admission.Valid() != nil ||
		!validStrategyPipelineMaterial(material) {
		return ApprovedSandboxPlan{}, contractError("strategy_plan_builder_invalid")
	}
	decision, err := newStrategyDecisionEvidence(builder.admission, material)
	if err != nil {
		return ApprovedSandboxPlan{}, contractError("strategy_plan_builder_invalid")
	}
	submission, action, err := builder.singleVenueSubmission(material)
	if err != nil {
		return ApprovedSandboxPlan{}, err
	}
	pipeline := builder.singleVenuePipelineEvidence(material)
	plan := builder.singleVenuePlan(material, decision, submission, action, pipeline)
	plan.ApprovalHash = pipeline.HashFor(plan)
	return plan, nil
}

func (builder *SingleVenueStrategyPlanBuilder) singleVenueSubmission(
	material StrategyPipelineMaterial,
) (Submission, IntentAction, error) {
	leg := material.Plan.Legs[0]
	if leg.Instrument.Symbol() != builder.admission.Work.Instrument ||
		leg.Instrument.Quote != "USDT" {
		return Submission{}, "", contractError("strategy_plan_builder_instrument_invalid")
	}
	notional, err := domain.CalculateNotional(leg.LimitPrice, leg.Quantity, 18)
	if err != nil {
		return Submission{}, "", contractError("strategy_plan_builder_notional_invalid")
	}
	strategyID, err := domain.NewStrategyID(builder.admission.Work.Strategy)
	if err != nil {
		return Submission{}, "", contractError("strategy_plan_builder_strategy_invalid")
	}
	action := IntentEntry
	if leg.Side == domain.SideSell {
		if !builder.ownsBase(leg.Instrument.Base, leg.Quantity) {
			return Submission{}, "", contractError("strategy_plan_builder_inventory_unavailable")
		}
		action = IntentExit
	}
	requestHash := hashStrategyPipelineValues("request", material.Plan.ID.String(),
		leg.OrderID.String(), leg.ClientOrderID, leg.Instrument.Symbol(),
		string(leg.Side), leg.Quantity.String(), leg.LimitPrice.String(),
		string(OrderStyleLimitIOC), string(action))
	submission := Submission{PlanID: material.Plan.ID, OrderID: leg.OrderID,
		AccountID:     builder.admission.Work.Account.ID,
		AccountEpoch:  builder.admission.Work.Account.Epoch,
		ClientOrderID: leg.ClientOrderID, StrategyID: strategyID,
		Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.Quantity,
		LimitPrice: leg.LimitPrice, Notional: notional, Style: OrderStyleLimitIOC,
		Action: action, RequestHash: requestHash, PolicyHash: material.Approved.PolicyHash,
		ApprovedAt: builder.admission.ApprovedAt}
	return submission, action, nil
}

func (builder *SingleVenueStrategyPlanBuilder) singleVenuePipelineEvidence(
	material StrategyPipelineMaterial,
) ApprovalPipelineEvidence {
	leg := material.Plan.Legs[0]
	return ApprovalPipelineEvidence{
		IntentKind: ApprovalStrategyIntent,
		IntentHash: hashStrategyPipelineValues("intent", string(material.Candidate.Payload),
			material.Approved.DecisionID.String(), material.Approved.ApprovalHash),
		AllocatorHash: hashStrategyPipelineValues("allocator", string(material.Allocated.Payload)),
		RiskHash: hashStrategyPipelineValues("risk", material.Approved.DecisionID.String(),
			material.Approved.ApprovalHash, material.Approved.PolicyHash),
		PlannerHash: hashStrategyPipelineValues("planner", material.Plan.ID.String(),
			leg.OrderID.String(), leg.ClientOrderID),
		AssetApprovalHash: hashStrategyPipelineValues("asset-approval",
			builder.admission.Work.ConfigurationID, builder.admission.Work.ConfigurationHash,
			builder.snapshot.SnapshotHash,
			builder.inventory.EvidenceHash,
			leg.Instrument.Symbol(),
			string(leg.Instrument.Base), string(leg.Instrument.Quote)),
		RiskApproved: true, AssetApproved: true,
		ObservedAt: builder.admission.ApprovedAt,
	}
}

func (builder *SingleVenueStrategyPlanBuilder) singleVenuePlan(material StrategyPipelineMaterial,
	decision StrategyDecisionEvidence, submission Submission, action IntentAction,
	pipeline ApprovalPipelineEvidence,
) ApprovedSandboxPlan {
	entrySafety := map[AccountID]EntrySafetySnapshot{}
	if action == IntentEntry {
		entrySafety[builder.admission.Work.Account.ID] = builder.admission.Safety
	}
	plan := ApprovedSandboxPlan{ID: material.Plan.ID.String(),
		SessionID: builder.admission.Work.SessionID, Submissions: []Submission{submission},
		Reservations: []DurableReservation{strategyReservation(submission)}, Arm: builder.admission.Arm,
		Eligibility: map[Exchange]EligibilitySnapshot{
			builder.admission.Work.Account.Exchange: builder.admission.Eligibility,
		},
		EntrySafety: entrySafety,
		AccountSnapshots: map[AccountID]AccountSnapshotReference{
			builder.admission.Work.Account.ID: {
				AccountID: builder.snapshot.AccountID, AccountEpoch: builder.snapshot.Epoch,
				SnapshotHash: builder.snapshot.SnapshotHash, ObservedAt: builder.snapshot.ObservedAt,
			},
		},
		StrategyDecision: &decision,
		Pipeline:         pipeline, ApprovedAt: builder.admission.ApprovedAt,
		ConfigurationID: builder.admission.Work.ConfigurationID}
	return plan
}

func (builder *SingleVenueStrategyPlanBuilder) ownsBase(
	asset domain.AssetSymbol,
	quantity domain.Quantity,
) bool {
	required, err := domain.ParseBalance(quantity.String())
	if err != nil || builder.inventory.ValidFor(builder.admission, asset) != nil ||
		builder.inventory.Available.Compare(required) < 0 {
		return false
	}
	for _, balance := range builder.snapshot.Balances {
		if balance.Asset == asset {
			return balance.Available.Compare(required) >= 0
		}
	}
	return false
}

func validStrategyPipelineMaterial(material StrategyPipelineMaterial) bool {
	if material.Event.Ordinal == 0 || material.Event.LogicalTime == 0 ||
		!json.Valid(material.Event.Canonical) ||
		!json.Valid(material.DecisionEvidence) ||
		material.Candidate.Ordinal != material.Event.Ordinal ||
		!json.Valid(material.Candidate.Payload) ||
		material.Allocated.Ordinal != material.Event.Ordinal ||
		!json.Valid(material.Allocated.Payload) ||
		material.Approved.DecisionID.String() == "" ||
		!hash256(material.Approved.ApprovalHash) ||
		!hash256(material.Approved.PolicyHash) ||
		material.Plan.ID.String() == "" || material.Plan.Intent != material.Approved ||
		material.Plan.Namespace == "" || len(material.Plan.Legs) != 1 {
		return false
	}
	leg := material.Plan.Legs[0]
	return leg.OrderID.String() != "" && leg.ClientOrderID != "" &&
		(leg.Side == domain.SideBuy || leg.Side == domain.SideSell)
}

func strategyReservation(submission Submission) DurableReservation {
	asset, quantity := string(submission.Instrument.Quote), submission.Notional.String()
	if submission.Side == domain.SideSell {
		asset, quantity = string(submission.Instrument.Base), submission.Quantity.String()
	}
	return DurableReservation{ID: "strategy-reservation-" + hashStrategyPipelineValues(
		"reservation", submission.PlanID.String(), submission.OrderID.String(),
		submission.ClientOrderID)[:24], AccountID: submission.AccountID,
		AccountEpoch: submission.AccountEpoch, OrderID: submission.OrderID.String(),
		Asset: asset, Quantity: quantity}
}

func hashStrategyPipelineValues(label string, values ...string) string {
	parts := []string{label}
	parts = append(parts, values...)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
