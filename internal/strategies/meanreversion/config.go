package meanreversion

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"axiom/internal/config"
)

// Configuration is the parsed immutable B3 baseline rule graph.
type Configuration struct {
	Version                  string
	Hash                     string
	PrimaryTimeframe         string
	HigherTimeframe          string
	ZScorePeriod             int
	ADXPeriod                int
	ADXThreshold             decimal
	EMARegimePeriod          int
	EMADeclineLookback       int
	EMADeclineThreshold      decimal
	EntryZScore              decimal
	NormalExitZScore         decimal
	ProtectiveExitZScore     decimal
	ATRPeriod                int
	ProtectiveStopMultiplier decimal
	MaximumHoldingCandles    uint64
	CooldownCandles          uint64
	RiskBudget               decimal
	MaximumPositions         uint32
	CandidateLifetime        time.Duration
	OrderValidity            time.Duration
	FinalizationDelay        time.Duration
	EvaluationWindow         time.Duration
	MaximumBookAge           time.Duration
	MaximumSpread            decimal
	MaximumSlippage          decimal
	MaximumGapAllowance      decimal
	MaximumNotional          decimal
	MaximumAllocation        decimal
}

// NewConfiguration hashes and parses one locked mean-reversion graph.
func NewConfiguration(source config.MeanReversionConfiguration) (Configuration, error) {
	if err := config.ValidateMeanReversionConfiguration(source); err != nil {
		return Configuration{}, strategyError(ReasonInvalidConfiguration)
	}
	values, hash, err := configurationValues(source)
	if err != nil {
		return Configuration{}, err
	}
	parsed := Configuration{Version: source.StrategyVersion, Hash: hash,
		PrimaryTimeframe: source.PrimaryTimeframe, HigherTimeframe: source.HigherTimeframe}
	if err = parsed.parsePeriods(values); err != nil {
		return Configuration{}, err
	}
	if err = parsed.parseThresholds(values); err != nil {
		return Configuration{}, err
	}
	if err = parsed.parseRisk(values); err != nil {
		return Configuration{}, err
	}
	if err = parsed.parseTiming(values); err != nil {
		return Configuration{}, err
	}
	if parsed.ProtectiveExitZScore.compare(parsed.EntryZScore) >= 0 ||
		parsed.EntryZScore.compare(parsed.NormalExitZScore) >= 0 || parsed.MaximumPositions != 1 {
		return Configuration{}, strategyError(ReasonInvalidConfiguration)
	}
	return parsed, nil
}

func configurationValues(source config.MeanReversionConfiguration) (map[string]string, string, error) {
	if source.StrategyVersion != "mean-reversion.v1b.1" || source.PrimaryTimeframe != "1h" ||
		source.HigherTimeframe != "4h" || len(source.Parameters) != config.MeanReversionParameterCount {
		return nil, "", strategyError(ReasonInvalidConfiguration)
	}
	canonical, err := json.Marshal(source)
	if err != nil {
		return nil, "", strategyError(ReasonInvalidConfiguration)
	}
	digest := sha256.Sum256(canonical)
	values := make(map[string]string, len(source.Parameters))
	for _, parameter := range source.Parameters {
		if parameter.ID == "" || parameter.Value == "" || parameter.AlgorithmVersion == "" {
			return nil, "", strategyError(ReasonInvalidConfiguration)
		}
		if _, duplicate := values[parameter.ID]; duplicate {
			return nil, "", strategyError(ReasonInvalidConfiguration)
		}
		values[parameter.ID] = parameter.Value
	}
	return values, hex.EncodeToString(digest[:]), nil
}

func (parsed *Configuration) parsePeriods(values map[string]string) error {
	var err error
	if primary, parseErr := integer(values, "mean_reversion.primary_timeframe_hours"); parseErr != nil || primary != 1 {
		return strategyError(ReasonInvalidConfiguration)
	}
	if higher, parseErr := integer(values, "mean_reversion.higher_timeframe_hours"); parseErr != nil || higher != 4 {
		return strategyError(ReasonInvalidConfiguration)
	}
	if parsed.ZScorePeriod, err = integer(values, "mean_reversion.zscore_period"); err != nil {
		return err
	}
	if parsed.ADXPeriod, err = integer(values, "mean_reversion.adx_period"); err != nil {
		return err
	}
	if parsed.EMARegimePeriod, err = integer(values, "mean_reversion.ema_regime_period"); err != nil {
		return err
	}
	if parsed.EMADeclineLookback, err = integer(values, "mean_reversion.ema_decline_lookback"); err != nil {
		return err
	}
	if parsed.ATRPeriod, err = integer(values, "mean_reversion.atr_period"); err != nil {
		return err
	}
	return nil
}

func (parsed *Configuration) parseThresholds(values map[string]string) error {
	var err error
	for target, key := range map[*decimal]string{
		&parsed.ADXThreshold: "mean_reversion.adx_threshold", &parsed.EMADeclineThreshold: "mean_reversion.ema_decline_threshold",
		&parsed.EntryZScore: "mean_reversion.entry_zscore", &parsed.NormalExitZScore: "mean_reversion.normal_exit_zscore",
		&parsed.ProtectiveExitZScore: "mean_reversion.protective_exit_zscore", &parsed.ProtectiveStopMultiplier: "mean_reversion.protective_stop_atr_multiplier",
		&parsed.MaximumSpread: "mean_reversion.maximum_spread", &parsed.MaximumSlippage: "mean_reversion.maximum_simulated_slippage",
		&parsed.MaximumGapAllowance: "mean_reversion.maximum_gap_allowance",
	} {
		if *target, err = numeric(values, key); err != nil {
			return err
		}
	}
	return nil
}

func (parsed *Configuration) parseRisk(values map[string]string) error {
	var err error
	if parsed.MaximumHoldingCandles, err = unsigned(values, "mean_reversion.maximum_holding_period"); err != nil {
		return err
	}
	if parsed.CooldownCandles, err = unsigned(values, "mean_reversion.protective_loss_cooldown"); err != nil {
		return err
	}
	if parsed.RiskBudget, err = numeric(values, "mean_reversion.trade_risk_budget"); err != nil {
		return err
	}
	positions, err := unsigned(values, "mean_reversion.maximum_positions")
	if err != nil || positions != 1 {
		return strategyError(ReasonInvalidConfiguration)
	}
	parsed.MaximumPositions = uint32(positions)
	if parsed.MaximumNotional, err = numeric(values, "mean_reversion.maximum_notional"); err != nil {
		return err
	}
	parsed.MaximumAllocation, err = numeric(values, "mean_reversion.maximum_allocation")
	return err
}

func (parsed *Configuration) parseTiming(values map[string]string) error {
	var err error
	if parsed.CandidateLifetime, err = duration(values, "mean_reversion.candidate_lifetime", time.Second); err != nil {
		return err
	}
	if parsed.OrderValidity, err = duration(values, "mean_reversion.marketable_limit_validity", time.Second); err != nil {
		return err
	}
	if parsed.FinalizationDelay, err = duration(values, "mean_reversion.candle_finalization_delay", time.Second); err != nil {
		return err
	}
	if parsed.EvaluationWindow, err = duration(values, "mean_reversion.signal_evaluation_window", time.Second); err != nil {
		return err
	}
	parsed.MaximumBookAge, err = duration(values, "mean_reversion.arrival_book_max_age", time.Millisecond)
	return err
}

func integer(values map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(values[key])
	if err != nil || value <= 0 {
		return 0, strategyError(ReasonInvalidConfiguration)
	}
	return value, nil
}

func unsigned(values map[string]string, key string) (uint64, error) {
	value, err := strconv.ParseUint(values[key], 10, 64)
	if err != nil {
		return 0, strategyError(ReasonInvalidConfiguration)
	}
	return value, nil
}

func numeric(values map[string]string, key string) (decimal, error) {
	value, ok := values[key]
	if !ok {
		return decimal{}, strategyError(ReasonInvalidConfiguration)
	}
	return parseDecimal(value)
}

func duration(values map[string]string, key string, unit time.Duration) (time.Duration, error) {
	value, err := unsigned(values, key)
	if err != nil || value == 0 || value > uint64((1<<63-1)/unit) {
		return 0, strategyError(ReasonInvalidConfiguration)
	}
	return time.Duration(value) * unit, nil
}
