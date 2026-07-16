package trend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

// CandidateSource resolves the exact candidate protected by central approval.
type CandidateSource interface {
	Candidate(domain.DecisionID) (Candidate, bool)
}

// Planner converts only a central-risk-approved Trend candidate into one leg.
type Planner struct {
	mode      string
	namespace string
	source    CandidateSource
}

// NewPlanner constructs the A10 execution-planner adapter.
func NewPlanner(mode, namespace string, source CandidateSource) (*Planner, error) {
	if (mode != "backtest" && mode != "replay" && mode != "paper" && mode != "shadow") || namespace == "" || source == nil {
		return nil, trendError(ReasonInvalidConfiguration)
	}
	return &Planner{mode: mode, namespace: namespace, source: source}, nil
}

// Plan preserves exact strategy quantity/price and adds deterministic identities.
func (planner *Planner) Plan(_ context.Context, approved execution.ApprovedIntent) (execution.SimulatedPlan, error) {
	if approved.DecisionID.String() == "" || approved.ApprovalHash == "" || approved.PolicyHash == "" {
		return execution.SimulatedPlan{}, trendError(ReasonRiskClipped)
	}
	candidate, ok := planner.source.Candidate(approved.DecisionID)
	if !ok || candidate.DecisionID != approved.DecisionID {
		return execution.SimulatedPlan{}, trendError(ReasonRiskClipped)
	}
	strategyID, _ := domain.NewStrategyID("trend")
	clientID, err := execution.GenerateClientOrderID(execution.ClientOrderIdentity{Mode: planner.mode,
		StrategyID: strategyID, DecisionID: approved.DecisionID, Leg: 0, Attempt: 1})
	if err != nil {
		return execution.SimulatedPlan{}, err
	}
	planID, orderID, err := executionIDs(approved.DecisionID.String())
	if err != nil {
		return execution.SimulatedPlan{}, err
	}
	leg := execution.PlannedLeg{Index: 0, OrderID: orderID, ClientOrderID: clientID,
		Instrument: candidate.Instrument, Side: candidate.Side, Quantity: candidate.Quantity,
		LimitPrice: candidate.LimitPrice, ExpiresAt: candidate.ExpiresAt, Maker: false}
	return execution.SimulatedPlan{ID: planID, Intent: approved, Namespace: planner.namespace,
		DecisionLogicalTime: candidate.DecisionLogicalTime, Legs: []execution.PlannedLeg{leg}}, nil
}

func executionIDs(decisionID string) (domain.ExecutionPlanID, domain.VirtualOrderID, error) {
	digest := sha256.Sum256([]byte(decisionID))
	suffix := hex.EncodeToString(digest[:10])
	planID, err := domain.NewExecutionPlanID("trend-plan-" + suffix)
	if err != nil {
		return domain.ExecutionPlanID{}, domain.VirtualOrderID{}, err
	}
	orderID, err := domain.NewVirtualOrderID("trend-order-" + suffix)
	return planID, orderID, err
}

var _ execution.ExecutionPlanner = (*Planner)(nil)
