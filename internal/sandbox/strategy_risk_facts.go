package sandbox

import (
	"context"
	"encoding/hex"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
)

// StrategyRiskFacts binds the active central-risk policy and its resolved
// reserve to the exact account snapshot used for one strategy evaluation.
// It deliberately contains no optimistic default: callers must obtain it from
// a durable policy and valuation projection.
type StrategyRiskFacts struct {
	AccountID       AccountID
	AccountEpoch    uint64
	SnapshotHash    string
	PolicyID        string
	PolicyVersion   uint64
	PolicyHash      string
	Policy          risk.Policy
	MinimumReserve  domain.Money
	MaximumReserved domain.Money
	ObservedAt      time.Time
}

// StrategyRiskFactsSource supplies snapshot-bound central-risk facts. The
// source is separate from the strategy executor so it cannot silently infer a
// healthy policy, reserve, or valuation from configuration alone.
type StrategyRiskFactsSource interface {
	StrategyRiskFacts(
		context.Context,
		StrategySessionWork,
		AccountSnapshot,
		time.Time,
	) (StrategyRiskFacts, error)
}

// ValidFor rejects stale, cross-account, or unversioned risk facts before
// they can influence a strategy input. A full central risk evaluation still
// happens after candidate construction.
func (facts StrategyRiskFacts) ValidFor(
	work StrategySessionWork,
	snapshot AccountSnapshot,
	now time.Time,
) error {
	zero, err := domain.ParseMoney("0")
	if err != nil || work.ValidAt(now) != nil || snapshot.Validate() != nil ||
		facts.AccountID != work.Account.ID || facts.AccountEpoch != work.Account.Epoch ||
		facts.SnapshotHash != snapshot.SnapshotHash || !strategyRiskHash(facts.SnapshotHash) ||
		facts.PolicyID == "" || facts.PolicyVersion == 0 || !strategyRiskHash(facts.PolicyHash) ||
		risk.ValidatePolicy(facts.Policy) != nil || facts.Policy.ID != facts.PolicyID ||
		facts.Policy.Version != facts.PolicyVersion || facts.Policy.Scope.Kind != risk.ScopeGlobal ||
		facts.Policy.Scope.ID != "platform" || facts.Policy.State != risk.StateNormal ||
		facts.MinimumReserve.Compare(zero) < 0 || facts.MaximumReserved.Compare(zero) < 0 || facts.ObservedAt.IsZero() ||
		facts.ObservedAt.Location() != time.UTC || facts.ObservedAt.After(now) ||
		now.Sub(facts.ObservedAt) > 250*time.Millisecond {
		return contractError("strategy_risk_facts_invalid")
	}
	return nil
}

func strategyRiskHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
