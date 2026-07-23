package crossarb

import (
	"time"

	"axiom/internal/risk"
)

// RiskEvaluator is the central fail-closed strategy-independent boundary.
type RiskEvaluator interface {
	Evaluate(risk.Request) (risk.Decision, error)
}

// RiskInput contains the complete central-risk snapshot for both legs.
type RiskInput struct {
	Policies     []risk.Policy
	Observations risk.Observations
	EvaluatedAt  time.Time
	Cautious     risk.CautiousControls
}

// ApproveCandidate requires one explicit central-risk approval before claims
// or simulated dispatch. It grants no exchange or production capability.
func ApproveCandidate(
	engine RiskEvaluator,
	candidate Candidate,
	input RiskInput,
	nowOffsetNanos uint64,
) (risk.Decision, error) {
	if engine == nil || candidate.ID == "" || candidate.BuyExchange == candidate.SellExchange ||
		nowOffsetNanos > candidate.ExpiresOffsetNanos {
		return risk.Decision{}, strategyError("risk_candidate_invalid")
	}
	decision, err := engine.Evaluate(risk.Request{
		Intent: risk.IntentEntry, Cautious: input.Cautious, Policies: input.Policies,
		Observations: input.Observations, EvaluatedAt: input.EvaluatedAt,
	})
	if err != nil {
		return risk.Decision{}, strategyError("risk_evaluation_failed")
	}
	if decision.Action != risk.ActionApprove {
		return decision, strategyError("risk_" + decision.ReasonCode)
	}
	return decision, nil
}
