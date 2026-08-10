package triangular

import (
	"time"

	"axiom/internal/risk"
)

// RiskEvaluator is the central risk boundary used by the triangular arbitrage strategy.
type RiskEvaluator interface {
	Evaluate(risk.Request) (risk.Decision, error)
}

// RiskInput contains the complete immutable central-risk snapshot for a cycle.
type RiskInput struct {
	Policies     []risk.Policy         `json:"policies"`
	Observations risk.Observations     `json:"observations"`
	EvaluatedAt  time.Time             `json:"evaluated_at"`
	Cautious     risk.CautiousControls `json:"cautious"`
}

// ApproveCandidate requires an explicit central-risk approval for the complete
// three-leg entry before any resource may be claimed or any leg dispatched.
func ApproveCandidate(
	engine RiskEvaluator,
	candidate Candidate,
	input RiskInput,
	nowOffsetNanos uint64,
) (risk.Decision, error) {
	if engine == nil || candidate.ID == "" || len(candidate.Legs) != 3 ||
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
