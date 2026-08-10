package bootstrap

import (
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// sandboxStrategyEvaluationTrigger identifies the finalized signal candle set
// that may produce exactly one durable strategy evaluation. Order-book changes
// are intentionally excluded: they affect sizing and execution evidence, but
// they are not the configured Trend or Mean Reversion evaluation cadence.
func sandboxStrategyEvaluationTrigger(
	work sandbox.StrategySessionWork,
	market sandbox.StrategyMarketInput,
) (string, error) {
	if market.Instrument.Symbol() != work.Instrument {
		return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
	}
	switch work.Strategy {
	case sandbox.StrategyTrend:
		return sandboxStrategyCandleTrigger(work, market.Instrument, market.Candles["4h"])
	case sandbox.StrategyMeanReversion:
		primary, err := latestSandboxStrategyCandle(market.Instrument, "1h", market.Candles["1h"])
		if err != nil {
			return "", err
		}
		higher, err := latestSandboxStrategyCandle(market.Instrument, "4h", market.Candles["4h"])
		if err != nil {
			return "", err
		}
		return strategyInputHash(
			work.Strategy,
			work.Instrument,
			sandboxStrategyCandleIdentity(primary),
			sandboxStrategyCandleIdentity(higher),
		), nil
	default:
		return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
	}
}

// sandboxStrategyEvaluationTriggerFromCanonicalInput reconstructs the same
// trigger from durable decision-journal evidence after a restart. Parsing the
// strategy's real input contract avoids maintaining a weaker parallel schema.
func sandboxStrategyEvaluationTriggerFromCanonicalInput(
	work sandbox.StrategySessionWork,
	canonical json.RawMessage,
) (string, error) {
	if !json.Valid(canonical) {
		return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
	}
	switch work.Strategy {
	case sandbox.StrategyTrend:
		var input trend.Input
		if err := json.Unmarshal(canonical, &input); err != nil ||
			input.Ordinal == 0 || input.LogicalTime == 0 || input.Instrument.Symbol() != work.Instrument {
			return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
		}
		return sandboxStrategyCandleTrigger(work, input.Instrument, input.Candles)
	case sandbox.StrategyMeanReversion:
		var input meanreversion.Input
		if err := json.Unmarshal(canonical, &input); err != nil ||
			input.Ordinal == 0 || input.LogicalTime == 0 || input.Instrument.Symbol() != work.Instrument {
			return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
		}
		return sandboxStrategyEvaluationTrigger(work, sandbox.StrategyMarketInput{
			Instrument: input.Instrument,
			Candles: map[string][]exchangecontracts.Candle{
				"1h": input.PrimaryCandles,
				"4h": input.HigherCandles,
			},
		})
	default:
		return "", fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
	}
}

func sandboxStrategyCandleTrigger(
	work sandbox.StrategySessionWork,
	instrument domain.Instrument,
	candles []exchangecontracts.Candle,
) (string, error) {
	latest, err := latestSandboxStrategyCandle(instrument, "4h", candles)
	if err != nil {
		return "", err
	}
	return strategyInputHash(work.Strategy, work.Instrument, sandboxStrategyCandleIdentity(latest)), nil
}

func latestSandboxStrategyCandle(
	instrument domain.Instrument,
	interval string,
	candles []exchangecontracts.Candle,
) (exchangecontracts.Candle, error) {
	if instrument.Product != domain.ProductSpot || (interval != "1h" && interval != "4h") || len(candles) == 0 {
		return exchangecontracts.Candle{}, fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
	}
	var latest exchangecontracts.Candle
	for _, candle := range candles {
		if candle.Instrument != instrument || candle.Interval != interval || !candle.Closed ||
			candle.OpenTime.IsZero() || candle.CloseTime.IsZero() ||
			candle.OpenTime.Location() != time.UTC || candle.CloseTime.Location() != time.UTC ||
			!candle.OpenTime.Before(candle.CloseTime) || !projectorHash256(candle.RawPayloadHash) {
			return exchangecontracts.Candle{}, fmt.Errorf("sandbox_strategy_evaluation_trigger_invalid")
		}
		if latest.CloseTime.IsZero() || latest.CloseTime.Before(candle.CloseTime) {
			latest = candle
		}
	}
	return latest, nil
}

func sandboxStrategyCandleIdentity(candle exchangecontracts.Candle) string {
	return strategyInputHash(
		candle.Interval,
		candle.OpenTime.Format(time.RFC3339Nano),
		candle.CloseTime.Format(time.RFC3339Nano),
		candle.RawPayloadHash,
	)
}
