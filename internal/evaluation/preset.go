package evaluation

import "errors"

// Strategy identifies one reviewed V1 strategy family.
type Strategy string

// Supported strategy families are fixed by the balanced evaluation preset.
const (
	StrategyTrend      Strategy = "trend-following"
	StrategyMean       Strategy = "mean-reversion"
	StrategyTriangular Strategy = "triangular-arbitrage"
	StrategyCross      Strategy = "cross-exchange-arbitrage"
	StrategyInventory  Strategy = "inventory-rebalancing"
)

// Verdict is a strategy viability decision. It is separate from platform
// correctness: a trustworthy losing strategy may be rejected while the
// platform remains correct.
type Verdict string

// Candidate verdicts separate viability from platform correctness.
const (
	VerdictContinue Verdict = "CONTINUE"
	VerdictImprove  Verdict = "IMPROVE"
	VerdictReject   Verdict = "REJECT"
	VerdictBlocked  Verdict = "BLOCKED"
)

// CandidateConfiguration is one immutable member of the server-owned matrix.
// ConfigurationKey is semantic and resolves to a versioned configuration in
// storage; it is never accepted from the browser.
type CandidateConfiguration struct {
	Strategy         Strategy
	ConfigurationKey string
	OrderCapable     bool
}

// PlannedRun is one durable matrix member. RepeatOrdinal zero is the primary
// run; positive ordinals are deterministic repeats. CostStressBPS uses 10,000
// for baseline costs, 15,000 for 1.5x, and 20,000 for 2x.
type PlannedRun struct {
	ID               string
	Strategy         Strategy
	ConfigurationKey string
	Mode             string
	CapitalMicros    int64
	RepeatOrdinal    int16
	CostStressBPS    int32
}

// CapitalLevelsMicros are independent verification levels in exact USDT
// micro-units. The 10,000 USDT combined profile is separate from these runs.
var CapitalLevelsMicros = []int64{
	500_000_000,
	1_000_000_000,
	1_500_000_000,
	2_000_000_000,
}

// Combined evaluation capital limits are exact USDT micro-unit ceilings.
const (
	CombinedCapitalMicros  int64 = 10_000_000_000
	CombinedReserveMicros  int64 = 2_000_000_000
	CombinedStrategyMicros int64 = 2_000_000_000
)

// BalancedFullDefinition returns a fresh immutable definition of the complete
// 14-configuration matrix and combined-shadow capital policy.
func BalancedFullDefinition() []CandidateConfiguration {
	return []CandidateConfiguration{
		{StrategyTrend, "trend-balanced-01", true},
		{StrategyTrend, "trend-balanced-02", true},
		{StrategyTrend, "trend-balanced-03", true},
		{StrategyTrend, "trend-balanced-04", true},
		{StrategyMean, "mean-balanced-01", true},
		{StrategyMean, "mean-balanced-02", true},
		{StrategyMean, "mean-balanced-03", true},
		{StrategyMean, "mean-balanced-04", true},
		{StrategyTriangular, "triangular-balanced-01", true},
		{StrategyTriangular, "triangular-balanced-02", true},
		{StrategyCross, "cross-balanced-01", true},
		{StrategyCross, "cross-balanced-02", true},
		{StrategyInventory, "inventory-balanced-01", false},
		{StrategyInventory, "inventory-balanced-02", false},
	}
}

// BalancedFullRuns expands the primary 14 backtests and 14 replays with exact
// deterministic repeats, both cost stresses, and focused capacity levels for
// every order-capable family.
func BalancedFullRuns() []PlannedRun {
	definition := BalancedFullDefinition()
	values := make([]PlannedRun, 0, 136)
	firstOrderConfiguration := map[Strategy]string{}
	for _, candidate := range definition {
		if candidate.OrderCapable && firstOrderConfiguration[candidate.Strategy] == "" {
			firstOrderConfiguration[candidate.Strategy] = candidate.ConfigurationKey
		}
		capital := CombinedStrategyMicros
		if !candidate.OrderCapable {
			capital = CombinedCapitalMicros
		}
		for _, mode := range []string{"backtest", "replay"} {
			values = append(values,
				plannedRun(candidate, mode, capital, 0, 10_000, "primary"),
				plannedRun(candidate, mode, capital, 1, 10_000, "repeat"),
				plannedRun(candidate, mode, capital, 0, 15_000, "stress15"),
				plannedRun(candidate, mode, capital, 0, 20_000, "stress20"),
			)
		}
	}
	for _, strategy := range []Strategy{StrategyTrend, StrategyMean, StrategyTriangular, StrategyCross} {
		configurationKey := firstOrderConfiguration[strategy]
		candidate := CandidateConfiguration{Strategy: strategy, ConfigurationKey: configurationKey, OrderCapable: true}
		for _, capital := range CapitalLevelsMicros[:3] {
			for _, mode := range []string{"backtest", "replay"} {
				values = append(values, plannedRun(candidate, mode, capital, 0, 10_000, "capacity"))
			}
		}
	}
	return values
}

// BalancedFinalRuns returns the two final-window evaluations created only
// after one configuration has been durably locked for its strategy. Ordinal
// two is reserved for final-test evidence and cannot collide with primary or
// deterministic-repeat matrix members.
func BalancedFinalRuns(candidate CandidateConfiguration) []PlannedRun {
	capital := CombinedStrategyMicros
	if !candidate.OrderCapable {
		capital = CombinedCapitalMicros
	}
	return []PlannedRun{
		plannedRun(candidate, "replay", capital, 2, 10_000, "final"),
		plannedRun(candidate, "replay", capital, 2, 15_000, "final-stress15"),
	}
}

func plannedRun(candidate CandidateConfiguration, mode string, capital int64, repeat int16,
	stress int32, purpose string) PlannedRun {
	id := string(candidate.Strategy) + ":" + candidate.ConfigurationKey + ":" + mode + ":" +
		purpose + ":" + formatExactInteger(capital) + ":" + formatExactInteger(int64(repeat)) + ":" +
		formatExactInteger(int64(stress))
	return PlannedRun{ID: id, Strategy: candidate.Strategy, ConfigurationKey: candidate.ConfigurationKey,
		Mode: mode, CapitalMicros: capital, RepeatOrdinal: repeat, CostStressBPS: stress}
}

func formatExactInteger(value int64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}

// ValidateBalancedFullDefinition guards accidental matrix or capital drift.
func ValidateBalancedFullDefinition(values []CandidateConfiguration) error {
	counts := map[Strategy]int{}
	keys := map[string]struct{}{}
	for _, value := range values {
		if value.ConfigurationKey == "" {
			return errors.New("evaluation_configuration_key_missing")
		}
		if _, duplicate := keys[value.ConfigurationKey]; duplicate {
			return errors.New("evaluation_configuration_key_duplicate")
		}
		keys[value.ConfigurationKey] = struct{}{}
		counts[value.Strategy]++
		if value.Strategy == StrategyInventory && value.OrderCapable {
			return errors.New("evaluation_inventory_must_be_advisory")
		}
	}
	if len(values) != 14 || counts[StrategyTrend] != 4 || counts[StrategyMean] != 4 ||
		counts[StrategyTriangular] != 2 || counts[StrategyCross] != 2 || counts[StrategyInventory] != 2 {
		return errors.New("evaluation_matrix_incomplete")
	}
	if CombinedReserveMicros+4*CombinedStrategyMicros != CombinedCapitalMicros {
		return errors.New("evaluation_combined_capital_unbalanced")
	}
	return nil
}
