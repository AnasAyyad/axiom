package bootstrap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
	"axiom/internal/strategies/trend"
)

type ownerConsoleInputAwareProcessor struct {
	inputs   *ownerConsoleDecisionInputContext
	delegate backtest.Processor
}

// Process updates the per-event evidence providers before running the delegate.
func (processor *ownerConsoleInputAwareProcessor) Process(ctx context.Context, event replay.Event) (backtest.EventResult, error) {
	var input trend.Input
	if json.Unmarshal(event.Canonical, &input) != nil || processor.inputs.Set(input) != nil {
		return backtest.EventResult{}, fmt.Errorf("owner_console_decision_input_invalid")
	}
	return processor.delegate.Process(ctx, event)
}

// Metrics returns the delegate's canonical result metrics.
func (processor *ownerConsoleInputAwareProcessor) Metrics() backtest.Metrics {
	return processor.delegate.Metrics()
}

type ownerConsoleDecisionInputContext struct {
	mutex sync.RWMutex
	input trend.Input
	set   bool
}

// Set replaces the exact immutable input visible to downstream stage adapters.
func (inputs *ownerConsoleDecisionInputContext) Set(input trend.Input) error {
	if input.Ordinal == 0 || input.LogicalTime == 0 || input.Instrument.Product != domain.ProductSpot ||
		input.Sizing.InstrumentMetadata.Instrument != input.Instrument || input.Evidence.MarketViewRevision == 0 {
		return fmt.Errorf("owner_console_decision_input_invalid")
	}
	inputs.mutex.Lock()
	inputs.input, inputs.set = input, true
	inputs.mutex.Unlock()
	return nil
}

func (inputs *ownerConsoleDecisionInputContext) current() (trend.Input, error) {
	inputs.mutex.RLock()
	defer inputs.mutex.RUnlock()
	if !inputs.set {
		return trend.Input{}, fmt.Errorf("owner_console_decision_input_unavailable")
	}
	return inputs.input, nil
}

// Current returns the conservative risk projection for the current decision input.
func (inputs *ownerConsoleDecisionInputContext) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	input, err := inputs.current()
	if err != nil || input.Now.IsZero() || input.Now.Location() != time.UTC {
		return risk.Observations{}, nil, time.Time{}, fmt.Errorf("owner_console_risk_input_unavailable")
	}
	zero := ownerConsolePercent("0")
	one := ownerConsolePercent("1")
	openOrders, quality := uint32(0), uint8(100)
	queueLag, clockDrift := time.Duration(0), time.Duration(0)
	problem := !input.MarketHealthy
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	observations := risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero, Rolling24HourLoss: &zero,
		StrategyLoss: &zero, AssetExposure: &zero, CombinedExposure: &zero, ExchangeExposure: &zero,
		Reserve: &one, ReservedCapital: &zero, Spread: &zero, Slippage: &zero, OpenOrders: &openOrders,
		BookAge: &input.BookAge, QueueLag: &queueLag, ClockDrift: &clockDrift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}
	return observations, []risk.Policy{policy}, input.Now, nil
}

func ownerConsolePercent(value string) domain.Percent {
	parsed, _ := domain.ParsePercent(value)
	return parsed
}

type ownerConsoleDynamicBroker struct {
	claim      backtest.JobClaim
	inputs     *ownerConsoleDecisionInputContext
	guard      simulation.BoundaryGuard
	randomness *runtimecore.Randomness
	liquidity  *simulation.LiquidityLedger
}

func newOwnerConsoleDynamicBroker(claim backtest.JobClaim, inputs *ownerConsoleDecisionInputContext,
	guard simulation.BoundaryGuard) (*ownerConsoleDynamicBroker, error) {
	seed, err := hex.DecodeString(claim.Manifest.Seed)
	if err != nil {
		return nil, fmt.Errorf("owner_console_worker_seed_invalid")
	}
	randomness, err := runtimecore.NewRandomness(seed)
	if err != nil {
		return nil, err
	}
	return &ownerConsoleDynamicBroker{claim: claim, inputs: inputs, guard: guard, randomness: randomness,
		liquidity: simulation.NewLiquidityLedger()}, nil
}

// Submit builds the exact per-input simulation models and performs no network I/O.
func (broker *ownerConsoleDynamicBroker) Submit(ctx context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	input, err := broker.inputs.current()
	if err != nil || input.Evidence.FeeModelID != broker.claim.Configuration.Models.Fee ||
		input.Evidence.LatencyModelID != broker.claim.Configuration.Models.Latency {
		return nil, fmt.Errorf("owner_console_simulation_model_mismatch")
	}
	models, err := ownerConsoleBrokerModels(input, broker.claim.Manifest.Models)
	if err != nil {
		return nil, err
	}
	exchange := broker.claim.ExchangeID
	if exchange == "" {
		// Historical Trend manifests predate an explicit venue field and their
		// recorded input contract is Binance-scoped.
		exchange = "binance"
	}
	simulated, err := simulation.NewBroker(broker.randomness,
		ownerConsoleInputTimeline{input: input, exchange: exchange},
		ownerConsoleInputMetadata{input: input}, broker.guard, broker.liquidity, models)
	if err != nil {
		return nil, err
	}
	events, err := simulated.Submit(ctx, plan)
	if err != nil {
		return nil, err
	}
	for index := range events {
		events[index].OccurredAt = input.Now.Add(time.Duration(index) * time.Nanosecond)
	}
	return events, nil
}

// Cancel fails closed because this bounded broker leaves no nonterminal orders.
func (broker *ownerConsoleDynamicBroker) Cancel(context.Context, domain.VirtualOrderID, string) ([]execution.OrderEvent, error) {
	return nil, fmt.Errorf("owner_console_simulation_order_not_active")
}

func ownerConsoleBrokerModels(input trend.Input, namespace backtest.ModelNamespace) (simulation.BrokerModels, error) {
	if namespace.FillDomain == "" || input.Evidence.FillModelID != namespace.FillDomain {
		return simulation.BrokerModels{}, fmt.Errorf("owner_console_fill_model_mismatch")
	}
	zeroRate, _ := domain.ParseRate("0")
	zeroPercent, _ := domain.ParsePercent("0")
	onePercent, _ := domain.ParsePercent("1")
	latency := time.Duration(0)
	if input.Evidence.LatencyModelID != "fixed-zero-v1" {
		return simulation.BrokerModels{}, fmt.Errorf("owner_console_latency_model_unsupported")
	}
	return simulation.BrokerModels{
		Fee: simulation.FeeModel{Version: input.Evidence.FeeModelID, TakerRate: input.Sizing.EntryFeeRate,
			MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18},
		Price: simulation.PriceModel{Version: "recorded-first-executable-v1", Spread: zeroPercent,
			Slippage: zeroPercent, Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: input.Evidence.LatencyModelID, Samples: []time.Duration{latency}},
		Fill:    simulation.FillModel{Version: input.Evidence.FillModelID, PartialRatio: onePercent, QuantityScale: 18},
	}, nil
}

type ownerConsoleInputTimeline struct {
	input    trend.Input
	exchange string
}

// AtOrAfter returns only the recorded first executable observation.
func (timeline ownerConsoleInputTimeline) AtOrAfter(instrument domain.Instrument, logical uint64) (simulation.BookState, bool, error) {
	if instrument != timeline.input.Instrument || timeline.input.Sizing.FirstExecutablePrice.String() == "0" {
		return simulation.BookState{}, false, nil
	}
	quantity, _ := domain.ParseQuantity("1000000000")
	level := exchangecontracts.PriceLevel{Price: timeline.input.Sizing.FirstExecutablePrice, Quantity: quantity}
	if logical <= timeline.input.LogicalTime {
		logical = timeline.input.LogicalTime + 1
	}
	if timeline.exchange != "binance" && timeline.exchange != "bybit" {
		return simulation.BookState{}, false, fmt.Errorf("owner_console_shadow_exchange_invalid")
	}
	return simulation.BookState{Exchange: timeline.exchange, Instrument: instrument,
		Version: timeline.input.Evidence.MarketViewRevision, LogicalTime: logical,
		Bids: []exchangecontracts.PriceLevel{level}, Asks: []exchangecontracts.PriceLevel{level}}, true, nil
}

type ownerConsoleInputMetadata struct{ input trend.Input }

// Metadata returns the exact versioned filter set embedded in the input.
func (metadata ownerConsoleInputMetadata) Metadata(state simulation.BookState) (domain.InstrumentMetadata, error) {
	if state.Instrument != metadata.input.Instrument {
		return domain.InstrumentMetadata{}, fmt.Errorf("owner_console_metadata_identity_mismatch")
	}
	return metadata.input.Sizing.InstrumentMetadata, nil
}
