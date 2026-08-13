package runs

import (
	"sort"
	"strings"
)

// Mode describes an allowed V1 execution environment. Live is deliberately
// absent: it is not a catalogue choice and cannot be introduced by a request.
type Mode string

// Supported run modes exposed by the owner catalogue.
const (
	ModeDemonstration Mode = "demonstration"
	ModeBacktest      Mode = "backtest"
	ModeReplay        Mode = "replay"
	ModeShadow        Mode = "shadow"
	ModeSandbox       Mode = "sandbox"
	ModeTestnet       Mode = "testnet"
	ModeDemo          Mode = "demo"
)

// Exchange identifies the only V1 spot venues.
type Exchange string

// Supported public and sandbox exchanges exposed by the owner catalogue.
const (
	ExchangeBinance Exchange = "binance"
	ExchangeBybit   Exchange = "bybit"
)

// Strategy describes one production strategy package in owner language.
type Strategy struct {
	ID, Name, Explanation, Version string
	Modes                          []Mode
	Exchanges                      []Exchange
	ModeExchanges                  map[Mode][]Exchange
	Instruments                    []string
	Cadence, Warmup                string
	OrderCapable                   bool
	// IndependentExchanges emits one single-venue choice per supported exchange.
	// ExactExchangeSet requires the whole listed venue set (for a paired saga).
	// They are mutually exclusive so the catalogue cannot advertise a partial
	// multi-venue run as if it were valid.
	IndependentExchanges bool
	ExactExchangeSet     bool
}

// Selection is the stable semantic input to a unified run request.
type Selection struct {
	StrategyID string
	Mode       Mode
	Exchanges  []Exchange
	Instrument string
}

// Blocker explains an unsupported selection before a run is created.
type Blocker struct {
	Code, Summary, Detail, SuggestedAction string
}

// Combination is one valid server-advertised choice. Dataset, configuration,
// portfolio, and model identities are resolved by the runtime after selection.
type Combination struct {
	StrategyID, StrategyVersion, StrategyName, Instrument string
	Mode                                                  Mode
	Exchanges                                             []Exchange
	Cadence, Warmup                                       string
	OrderCapable                                          bool
}

// Registry is immutable after construction, making its advertised catalogue
// deterministic and safe to share between REST projections and run workers.
type Registry struct{ strategies map[string]Strategy }

// NewRegistry accepts only complete semantic strategies. It rejects an
// accidental live mode or any strategy that lacks a route through the shared
// product pipeline before it can reach an owner-facing catalogue.
func NewRegistry(strategies []Strategy) (*Registry, error) {
	registry := &Registry{strategies: make(map[string]Strategy, len(strategies))}
	for _, strategy := range strategies {
		if !validStrategy(strategy) {
			return nil, errInvalidStrategy
		}
		if _, exists := registry.strategies[strategy.ID]; exists {
			return nil, errInvalidStrategy
		}
		strategy.Modes = sortedModes(strategy.Modes)
		strategy.Exchanges = sortedExchanges(strategy.Exchanges)
		strategy.Instruments = sortedStrings(strategy.Instruments)
		registry.strategies[strategy.ID] = strategy
	}
	if len(registry.strategies) == 0 {
		return nil, errInvalidStrategy
	}
	return registry, nil
}

// Catalogue returns valid choices for a partial selection and one plain
// blocker for a fully invalid choice. It never silently substitutes an input.
func (registry *Registry) Catalogue(selection Selection) ([]Combination, *Blocker) {
	if registry == nil {
		return nil, unavailableBlocker()
	}
	if selection.Mode == "live" {
		return nil, &Blocker{Code: "LIVE_MODE_FORBIDDEN", Summary: "Real-money trading is not available.",
			Detail: "V1 never creates a production-private order request.", SuggestedAction: "Choose a demonstration, research, shadow, Testnet, or Demo run."}
	}
	if selection.StrategyID != "" {
		strategy, ok := registry.strategies[selection.StrategyID]
		if !ok {
			return nil, &Blocker{Code: "STRATEGY_UNKNOWN", Summary: "That strategy is not available.",
				Detail: "The requested semantic strategy ID is not registered by this build.", SuggestedAction: "Choose a strategy shown in the catalogue."}
		}
		combinations := filterStrategy(strategy, selection)
		if len(combinations) == 0 {
			return nil, selectionBlocker(strategy, selection)
		}
		return combinations, nil
	}
	var combinations []Combination
	for _, strategy := range registry.strategies {
		combinations = append(combinations, filterStrategy(strategy, selection)...)
	}
	sort.Slice(combinations, func(left, right int) bool {
		if combinations[left].StrategyID != combinations[right].StrategyID {
			return combinations[left].StrategyID < combinations[right].StrategyID
		}
		return combinations[left].Mode < combinations[right].Mode
	})
	if len(combinations) == 0 {
		return nil, &Blocker{Code: "NO_COMPATIBLE_RUN", Summary: "No run matches these choices.",
			Detail: "The selected mode, exchange, or instrument is not a supported combination.", SuggestedAction: "Choose a catalogue option without a blocker."}
	}
	return combinations, nil
}

func filterStrategy(strategy Strategy, selection Selection) []Combination {
	var combinations []Combination
	for _, mode := range strategy.Modes {
		if selection.Mode != "" && selection.Mode != mode {
			continue
		}
		if selection.Instrument != "" && !contains(strategy.Instruments, selection.Instrument) {
			continue
		}
		instrument := selection.Instrument
		if instrument == "" {
			instrument = strategy.Instruments[0]
		}
		exchanges := exchangesForMode(strategy, mode)
		if len(selection.Exchanges) > 0 {
			if !allSupported(exchanges, selection.Exchanges) ||
				(strategy.IndependentExchanges && len(selection.Exchanges) != 1) ||
				(strategy.ExactExchangeSet && !sameExchanges(exchanges, selection.Exchanges)) {
				continue
			}
			exchanges = sortedExchanges(selection.Exchanges)
		}
		if len(selection.Exchanges) == 0 && strategy.IndependentExchanges {
			for _, exchange := range exchanges {
				combinations = append(combinations, Combination{StrategyID: strategy.ID, StrategyVersion: strategy.Version,
					StrategyName: strategy.Name, Mode: mode, Exchanges: []Exchange{exchange}, Instrument: instrument,
					Cadence: strategy.Cadence, Warmup: strategy.Warmup, OrderCapable: strategy.OrderCapable})
			}
			continue
		}
		combinations = append(combinations, Combination{StrategyID: strategy.ID, StrategyVersion: strategy.Version,
			StrategyName: strategy.Name, Mode: mode, Exchanges: append([]Exchange(nil), exchanges...), Instrument: instrument,
			Cadence: strategy.Cadence, Warmup: strategy.Warmup, OrderCapable: strategy.OrderCapable})
	}
	return combinations
}

func selectionBlocker(strategy Strategy, selection Selection) *Blocker {
	if selection.Mode != "" && !containsMode(strategy.Modes, selection.Mode) {
		return &Blocker{Code: "MODE_UNSUPPORTED", Summary: "This strategy cannot run in that mode.",
			Detail: strategy.Name + " has no approved " + string(selection.Mode) + " workflow.", SuggestedAction: "Choose one of this strategy's available modes."}
	}
	if selection.Instrument != "" && !contains(strategy.Instruments, selection.Instrument) {
		return &Blocker{Code: "INSTRUMENT_UNSUPPORTED", Summary: "This strategy does not support that instrument.",
			Detail: "The selected instrument is outside the strategy's reviewed scope.", SuggestedAction: "Choose a supported spot instrument."}
	}
	return &Blocker{Code: "EXCHANGE_UNSUPPORTED", Summary: "This strategy does not support that exchange selection.",
		Detail: "The selected venue combination is outside the strategy's reviewed scope.", SuggestedAction: "Choose an exchange combination shown by the catalogue."}
}

func validStrategy(strategy Strategy) bool {
	if !semanticID(strategy.ID) || strings.TrimSpace(strategy.Name) == "" || strings.TrimSpace(strategy.Explanation) == "" ||
		!strings.Contains(strategy.Version, "@") || len(strategy.Modes) == 0 || len(strategy.Exchanges) == 0 ||
		len(strategy.Instruments) == 0 || strings.TrimSpace(strategy.Cadence) == "" || strings.TrimSpace(strategy.Warmup) == "" ||
		(strategy.IndependentExchanges && strategy.ExactExchangeSet) {
		return false
	}
	for _, mode := range strategy.Modes {
		if !validMode(mode) {
			return false
		}
	}
	for _, exchange := range strategy.Exchanges {
		if exchange != ExchangeBinance && exchange != ExchangeBybit {
			return false
		}
	}
	for mode, exchanges := range strategy.ModeExchanges {
		if !containsMode(strategy.Modes, mode) || len(exchanges) == 0 || !allSupported(strategy.Exchanges, exchanges) {
			return false
		}
	}
	return true
}

func exchangesForMode(strategy Strategy, mode Mode) []Exchange {
	if exchanges, ok := strategy.ModeExchanges[mode]; ok {
		return exchanges
	}
	return strategy.Exchanges
}

func validMode(mode Mode) bool {
	return mode == ModeDemonstration || mode == ModeBacktest || mode == ModeReplay ||
		mode == ModeShadow || mode == ModeSandbox || mode == ModeTestnet || mode == ModeDemo
}

func semanticID(value string) bool {
	if len(value) < 3 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-') {
			return false
		}
	}
	return true
}

func contains(values []string, value string) bool { return containsString(values, value) }
func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func containsMode(values []Mode, value Mode) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func allSupported(supported, selected []Exchange) bool {
	for _, exchange := range selected {
		if !containsExchange(supported, exchange) {
			return false
		}
	}
	return true
}
func containsExchange(values []Exchange, value Exchange) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
func sameExchanges(left, right []Exchange) bool {
	if len(left) != len(right) {
		return false
	}
	for _, exchange := range left {
		if !containsExchange(right, exchange) {
			return false
		}
	}
	return true
}
func sortedStrings(values []string) []string {
	value := append([]string(nil), values...)
	sort.Strings(value)
	return value
}
func sortedModes(values []Mode) []Mode {
	value := append([]Mode(nil), values...)
	sort.Slice(value, func(left, right int) bool { return value[left] < value[right] })
	return value
}
func sortedExchanges(values []Exchange) []Exchange {
	value := append([]Exchange(nil), values...)
	sort.Slice(value, func(left, right int) bool { return value[left] < value[right] })
	return value
}
