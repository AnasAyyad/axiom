package trend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"axiom/internal/config"
)

// Configuration is the parsed immutable rule graph used by one evaluator.
type Configuration struct {
	Version                string
	Hash                   string
	EMAConfirmation        int
	EMARegime              int
	BreakoutLookback       int
	ATRPeriod              int
	InitialStopMultiplier  decimal
	TrailingStopMultiplier decimal
	CooldownCandles        uint64
	RiskBudget             decimal
	MaximumNotional        decimal
	MaximumSlippage        decimal
	CandidateLifetime      time.Duration
	OrderValidity          time.Duration
	MaximumBookAge         time.Duration
	EvaluationWindow       time.Duration
	FinalizationDelay      time.Duration
	MaximumPositions       uint32
}

// NewConfiguration validates, hashes, and parses one locked Trend graph.
func NewConfiguration(source config.TrendConfiguration) (Configuration, error) {
	values, hash, err := configurationValues(source)
	if err != nil {
		return Configuration{}, err
	}
	parsed := Configuration{Version: source.StrategyVersion, Hash: hash}
	if err = parsed.parsePeriods(values); err != nil {
		return Configuration{}, err
	}
	if err = parsed.parseSizing(values); err != nil {
		return Configuration{}, err
	}
	if err = parsed.parseTiming(values); err != nil {
		return Configuration{}, err
	}
	return parsed, nil
}

func configurationValues(source config.TrendConfiguration) (map[string]string, string, error) {
	if source.StrategyVersion == "" || source.Timeframe != "4h" || len(source.Parameters) != 16 {
		return nil, "", trendError(ReasonInvalidConfiguration)
	}
	canonical, err := json.Marshal(source)
	if err != nil {
		return nil, "", trendError(ReasonInvalidConfiguration)
	}
	digest := sha256.Sum256(canonical)
	values := make(map[string]string, len(source.Parameters))
	for _, parameter := range source.Parameters {
		if parameter.ID == "" || parameter.Value == "" {
			return nil, "", trendError(ReasonInvalidConfiguration)
		}
		if _, duplicate := values[parameter.ID]; duplicate {
			return nil, "", trendError(ReasonInvalidConfiguration)
		}
		values[parameter.ID] = parameter.Value
	}
	return values, hex.EncodeToString(digest[:]), nil
}

func (parsed *Configuration) parsePeriods(values map[string]string) error {
	var err error
	if parsed.EMAConfirmation, err = integer(values, "trend.ema_confirmation_period"); err != nil {
		return err
	}
	if parsed.EMARegime, err = integer(values, "trend.ema_regime_period"); err != nil {
		return err
	}
	if parsed.BreakoutLookback, err = integer(values, "trend.breakout_lookback"); err != nil {
		return err
	}
	if parsed.ATRPeriod, err = integer(values, "trend.atr_period"); err != nil {
		return err
	}
	return nil
}

func (parsed *Configuration) parseSizing(values map[string]string) error {
	var err error
	if parsed.InitialStopMultiplier, err = numeric(values, "trend.initial_stop_atr_multiplier"); err != nil {
		return err
	}
	if parsed.TrailingStopMultiplier, err = numeric(values, "trend.trailing_stop_atr_multiplier"); err != nil {
		return err
	}
	if parsed.RiskBudget, err = numeric(values, "trend.trade_risk_budget"); err != nil {
		return err
	}
	if parsed.MaximumNotional, err = numeric(values, "trend.maximum_notional"); err != nil {
		return err
	}
	if parsed.MaximumSlippage, err = numeric(values, "trend.maximum_simulated_slippage"); err != nil {
		return err
	}
	if parsed.CooldownCandles, err = unsigned(values, "trend.protective_loss_cooldown"); err != nil {
		return err
	}
	positions, positionsErr := unsigned(values, "trend.maximum_positions")
	if positionsErr != nil || positions != 1 {
		return trendError(ReasonInvalidConfiguration)
	}
	parsed.MaximumPositions = uint32(positions)
	return nil
}

func (parsed *Configuration) parseTiming(values map[string]string) error {
	var err error
	if parsed.CandidateLifetime, err = duration(values, "trend.candidate_lifetime", time.Second); err != nil {
		return err
	}
	if parsed.OrderValidity, err = duration(values, "trend.marketable_limit_validity", time.Second); err != nil {
		return err
	}
	if parsed.MaximumBookAge, err = duration(values, "trend.arrival_book_max_age", time.Millisecond); err != nil {
		return err
	}
	if parsed.EvaluationWindow, err = duration(values, "trend.signal_evaluation_window", time.Second); err != nil {
		return err
	}
	if parsed.FinalizationDelay, err = duration(values, "trend.candle_finalization_delay", time.Second); err != nil {
		return err
	}
	return nil
}

func integer(values map[string]string, key string) (int, error) {
	value, err := strconv.Atoi(values[key])
	if err != nil || value <= 0 {
		return 0, trendError(ReasonInvalidConfiguration)
	}
	return value, nil
}

func unsigned(values map[string]string, key string) (uint64, error) {
	value, err := strconv.ParseUint(values[key], 10, 64)
	if err != nil {
		return 0, trendError(ReasonInvalidConfiguration)
	}
	return value, nil
}

func numeric(values map[string]string, key string) (decimal, error) {
	value, ok := values[key]
	if !ok {
		return decimal{}, trendError(ReasonInvalidConfiguration)
	}
	return parseDecimal(value)
}

func duration(values map[string]string, key string, unit time.Duration) (time.Duration, error) {
	value, err := unsigned(values, key)
	if err != nil || value == 0 || value > uint64((1<<63-1)/unit) {
		return 0, trendError(ReasonInvalidConfiguration)
	}
	return time.Duration(value) * unit, nil
}
