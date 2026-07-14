package marketdata

import (
	"sync"

	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
)

// CandleStore owns one bounded chronological completed-candle series.
type CandleStore struct {
	mutex      sync.RWMutex
	exchange   string
	instrument domain.Instrument
	interval   string
	limit      int
	version    uint64
	latest     Observation
	candles    []exchangecontracts.Candle
}

// NewCandleStore constructs a completed-candle store for one market interval.
func NewCandleStore(exchange string, instrument domain.Instrument, interval string, limit int) (*CandleStore, error) {
	validated, err := domain.NewSpotInstrument(instrument.Base, instrument.Quote)
	if err != nil || validated != instrument || exchange == "" || interval == "" || limit <= 0 {
		return nil, marketError("candle_configuration_invalid")
	}
	return &CandleStore{exchange: exchange, instrument: instrument, interval: interval, limit: limit}, nil
}

// Add accepts only a new closed candle or an identical duplicate.
func (store *CandleStore) Add(candle exchangecontracts.Candle, observation Observation) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if observation.Validate() != nil || candle.Exchange != exchangecontracts.ExchangeID(store.exchange) ||
		candle.Instrument != store.instrument || candle.Interval != store.interval || !candle.Closed ||
		candle.OpenTime.IsZero() || !candle.CloseTime.After(candle.OpenTime) {
		return marketError("candle_rejected")
	}
	count := len(store.candles)
	if count > 0 {
		prior := store.candles[count-1]
		if candle.OpenTime.Before(prior.OpenTime) {
			return marketError("candle_regressed")
		}
		if candle.OpenTime.Equal(prior.OpenTime) {
			if candle.RawPayloadHash == prior.RawPayloadHash {
				return nil
			}
			return marketError("candle_conflict")
		}
	}
	store.candles = append(store.candles, candle)
	if len(store.candles) > store.limit {
		store.candles = append([]exchangecontracts.Candle(nil), store.candles[len(store.candles)-store.limit:]...)
	}
	store.version++
	store.latest = observation
	return nil
}

// View returns an immutable chronological copy.
func (store *CandleStore) View() CandleView {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	return CandleView{record: candleViewRecord{Exchange: store.exchange, Instrument: store.instrument,
		Interval: store.interval, Version: store.version, Observation: store.latest,
		Candles: append([]exchangecontracts.Candle(nil), store.candles...)}}
}
