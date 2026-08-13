package sandbox

import (
	"fmt"
	"time"

	"axiom/internal/risk"
)

// StrategyRiskInputs is one immutable risk.ObservationProvider assembled
// before a strategy enters the in-process evaluation/allocation/risk/planning
// hot path. It cannot fetch, default, or mutate a risk fact during approval.
type StrategyRiskInputs struct {
	observation StrategyRiskObservation
	policies    []risk.Policy
	evaluatedAt time.Time
}

// NewStrategyRiskInputs binds a complete durable observation and its exact
// immutable policy to one evaluation instant.
func NewStrategyRiskInputs(
	work StrategySessionWork,
	snapshot AccountSnapshot,
	market StrategyMarketInput,
	facts StrategyRiskFacts,
	observation StrategyRiskObservation,
	now time.Time,
) (*StrategyRiskInputs, error) {
	if observation.ValidFor(work, snapshot, market, facts, now) != nil ||
		risk.ValidatePolicy(facts.Policy) != nil || facts.Policy.ID != observation.PolicyID ||
		facts.Policy.Version != observation.PolicyVersion || facts.Policy.State != risk.StateNormal {
		return nil, fmt.Errorf("sandbox_strategy_risk_inputs_invalid")
	}
	return &StrategyRiskInputs{observation: observation,
		policies: []risk.Policy{facts.Policy}, evaluatedAt: now}, nil
}

// Current returns defensive policy copies of the already-bound inputs. There
// is no storage or network access between allocation and central risk.
func (inputs *StrategyRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	if inputs == nil || len(inputs.policies) != 1 || inputs.evaluatedAt.IsZero() {
		return risk.Observations{}, nil, time.Time{}, fmt.Errorf("sandbox_strategy_risk_inputs_unavailable")
	}
	return inputs.observation.RiskObservations(), append([]risk.Policy(nil), inputs.policies...), inputs.evaluatedAt, nil
}

var _ risk.ObservationProvider = (*StrategyRiskInputs)(nil)
