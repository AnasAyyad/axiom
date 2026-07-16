package risk

import (
	"time"

	"axiom/internal/domain"
)

// DefaultLimits returns every active conservative V1A threshold as fractions.
func DefaultLimits() Limits {
	return Limits{AccountDrawdown: percent("0.05"), DayLoss: percent("0.01"), RollingLoss: percent("0.01"),
		StrategyLoss: percent("0.03"), AssetExposure: percent("0.30"), CombinedExposure: percent("0.50"),
		ExchangeExposure: percent("0.60"), MinimumReserve: percent("0.15"), ReservedCapital: percent("0.85"),
		MaximumSpread: percent("0.01"), MaximumSlippage: percent("0.005"), MaximumOpenOrders: 8,
		MaximumBookAge: 250 * time.Millisecond, MaximumQueueLag: 250 * time.Millisecond,
		MaximumClockDrift: 100 * time.Millisecond, MinimumQuality: 90}
}

// DefaultGlobalPolicy starts PAUSED and never grants entry by construction.
func DefaultGlobalPolicy() Policy {
	return Policy{ID: "v1a-global-default", Version: 1, Scope: Scope{Kind: ScopeGlobal, ID: "platform"},
		State: StatePaused, Limits: DefaultLimits()}
}

func percent(value string) domain.Percent {
	parsed, _ := domain.ParsePercent(value)
	return parsed
}

func validPolicy(policy Policy) bool {
	return policy.ID != "" && policy.Version > 0 && policy.Scope.ID != "" && validScope(policy.Scope.Kind) &&
		validState(policy.State) && validLimits(policy.Limits)
}

func validLimits(limits Limits) bool {
	zero := percent("0")
	return limits.AccountDrawdown.Compare(zero) > 0 && limits.DayLoss.Compare(zero) > 0 &&
		limits.RollingLoss.Compare(zero) > 0 && limits.StrategyLoss.Compare(zero) > 0 &&
		limits.AssetExposure.Compare(zero) > 0 && limits.CombinedExposure.Compare(zero) > 0 &&
		limits.ExchangeExposure.Compare(zero) > 0 && limits.MinimumReserve.Compare(zero) > 0 &&
		limits.ReservedCapital.Compare(zero) > 0 && limits.MaximumSpread.Compare(zero) > 0 &&
		limits.MaximumSlippage.Compare(zero) > 0 && limits.MaximumOpenOrders > 0 &&
		limits.MaximumBookAge > 0 && limits.MaximumQueueLag > 0 && limits.MaximumClockDrift > 0 &&
		limits.MinimumQuality > 0 && limits.MinimumQuality <= 100
}

func validScope(scope ScopeKind) bool {
	return scope == ScopeGlobal || scope == ScopeAccount || scope == ScopeExchange || scope == ScopeStrategy || scope == ScopePortfolio ||
		scope == ScopeAsset || scope == ScopeInstrument
}

func validState(state State) bool {
	return state == StateNormal || state == StateCautious || state == StatePaused || state == StateLocked
}

func stateRank(state State) int {
	switch state {
	case StateNormal:
		return 0
	case StateCautious:
		return 1
	case StatePaused:
		return 2
	case StateLocked:
		return 3
	default:
		return 4
	}
}
