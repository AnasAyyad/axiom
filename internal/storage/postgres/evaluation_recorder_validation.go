package postgres

import (
	"fmt"
	"time"
)

func validateEvaluationRecorderObservation(session string, observedAt time.Time,
	instruments []EvaluationRecorderInstrumentObservation) error {
	if err := validateEvaluationRecorderObservationIdentity(session, observedAt); err != nil {
		return err
	}
	return validateEvaluationRecorderInstruments(instruments)
}

func validateEvaluationRecorderObservationIdentity(session string, observedAt time.Time) error {
	if session == "" || observedAt.IsZero() || observedAt.Location() != time.UTC {
		return fmt.Errorf("evaluation_recorder_observation_invalid")
	}
	return nil
}

func validateEvaluationRecorderInstruments(instruments []EvaluationRecorderInstrumentObservation) error {
	if len(instruments) != 6 {
		return fmt.Errorf("evaluation_recorder_observation_invalid")
	}
	seen := make(map[string]struct{}, 6)
	for _, item := range instruments {
		key := item.ExchangeID + ":" + item.Instrument
		if (item.ExchangeID != "binance" && item.ExchangeID != "bybit") ||
			(item.Instrument != "BTCUSDT" && item.Instrument != "ETHUSDT" && item.Instrument != "ETHBTC") ||
			item.LatestEventAt.IsZero() || item.LatestEventAt.Location() != time.UTC {
			return fmt.Errorf("evaluation_recorder_observation_invalid")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("evaluation_recorder_observation_duplicate")
		}
		seen[key] = struct{}{}
	}
	return nil
}
