package evaluation

import (
	"encoding/json"
	"fmt"
	"strconv"

	"axiom/internal/config"
)

// BalancedRunConfiguration derives one immutable credential-free matrix
// configuration without mutating the normative 500 USDT default profile.
func BalancedRunConfiguration(base config.Configuration, configurationKey string,
	capitalMicros int64) (config.Configuration, error) {
	if !evaluationCapitalAllowed(capitalMicros) {
		return config.Configuration{}, fmt.Errorf("evaluation_capital_invalid")
	}
	payload, err := json.Marshal(base)
	if err != nil {
		return config.Configuration{}, err
	}
	var value config.Configuration
	if json.Unmarshal(payload, &value) != nil {
		return config.Configuration{}, fmt.Errorf("evaluation_configuration_clone_failed")
	}
	value.Portfolio.StartingCapital.Value = strconv.FormatInt(capitalMicros/1_000_000, 10)
	for index, candidate := range BalancedFullDefinition() {
		if candidate.ConfigurationKey == configurationKey {
			value.Revision = uint64(index + 2)
			break
		}
	}
	parameter, parameterValue, err := balancedParameter(configurationKey)
	if err != nil {
		return config.Configuration{}, err
	}
	if err = setBalancedParameter(&value, parameter, parameterValue); err != nil {
		return config.Configuration{}, err
	}
	if err = config.Validate(value); err != nil {
		return config.Configuration{}, fmt.Errorf("evaluation_configuration_invalid: %w", err)
	}
	return value, nil
}

// BalancedCombinedConfiguration is the versioned 10,000 USDT evaluation
// profile. It does not alter config.DefaultConfiguration or its 500 USDT
// normative starting capital.
func BalancedCombinedConfiguration(base config.Configuration) (config.Configuration, error) {
	value, err := BalancedRunConfiguration(base, "trend-balanced-01", CombinedCapitalMicros)
	if err != nil {
		return config.Configuration{}, err
	}
	value.Portfolio.StartingCapital.Value = "10000"
	if err = config.Validate(value); err != nil {
		return config.Configuration{}, err
	}
	return value, nil
}

func evaluationCapitalAllowed(value int64) bool {
	switch value {
	case 500_000_000, 1_000_000_000, 1_500_000_000, 2_000_000_000, 10_000_000_000:
		return true
	default:
		return false
	}
}

func balancedParameter(key string) (string, string, error) {
	values := map[string][2]string{
		"trend-balanced-01":      {"trend.breakout_lookback", "20"},
		"trend-balanced-02":      {"trend.breakout_lookback", "30"},
		"trend-balanced-03":      {"trend.breakout_lookback", "40"},
		"trend-balanced-04":      {"trend.breakout_lookback", "55"},
		"mean-balanced-01":       {"mean_reversion.entry_zscore", "-1.5"},
		"mean-balanced-02":       {"mean_reversion.entry_zscore", "-2"},
		"mean-balanced-03":       {"mean_reversion.entry_zscore", "-2.5"},
		"mean-balanced-04":       {"mean_reversion.entry_zscore", "-3"},
		"triangular-balanced-01": {"triangular.latency_deterioration", "0.0005"},
		"triangular-balanced-02": {"triangular.latency_deterioration", "0.001"},
		"cross-balanced-01":      {"cross_exchange.maximum_book_age", "250"},
		"cross-balanced-02":      {"cross_exchange.maximum_book_age", "150"},
		"inventory-balanced-01":  {"rebalancing.minimum_confidence", "0.80"},
		"inventory-balanced-02":  {"rebalancing.minimum_confidence", "0.90"},
	}
	value, ok := values[key]
	if !ok {
		return "", "", fmt.Errorf("evaluation_configuration_key_invalid")
	}
	return value[0], value[1], nil
}

func setBalancedParameter(value *config.Configuration, id, setting string) error {
	groups := [][]config.StrategyParameter{value.Trend.Parameters, value.MeanReversion.Parameters,
		value.Triangular.Parameters, value.CrossExchange.Parameters, value.Rebalancing.Parameters}
	for _, parameters := range groups {
		for index := range parameters {
			if parameters[index].ID == id {
				parameters[index].Value = setting
				return nil
			}
		}
	}
	return fmt.Errorf("evaluation_configuration_parameter_missing")
}
