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
	"axiom/internal/strategies/meanreversion"
)

type ownerConsoleMeanReversionInputAwareProcessor struct {
	inputs   *ownerConsoleMeanReversionInputContext
	delegate backtest.Processor
}

// Process publishes the exact mean-reversion input before invoking the pipeline.
func (processor *ownerConsoleMeanReversionInputAwareProcessor) Process(ctx context.Context, event replay.Event) (backtest.EventResult, error) {
	var input meanreversion.Input
	if json.Unmarshal(event.Canonical, &input) != nil || processor.inputs.Set(input) != nil {
		return backtest.EventResult{}, fmt.Errorf("owner_console_mean_reversion_input_invalid")
	}
	return processor.delegate.Process(ctx, event)
}

// Metrics returns the delegate's canonical result metrics.
func (processor *ownerConsoleMeanReversionInputAwareProcessor) Metrics() backtest.Metrics {
	return processor.delegate.Metrics()
}

// ownerConsoleMeanReversionInputContext makes the event's immutable mean reversion evidence the
// sole source for risk and simulation. It intentionally does not derive a
// substitute candle, book, or model from live transport.
type ownerConsoleMeanReversionInputContext struct {
	mutex sync.RWMutex
	input meanreversion.Input
	set   bool
}

// Set replaces the exact immutable input visible to downstream adapters.
func (inputs *ownerConsoleMeanReversionInputContext) Set(input meanreversion.Input) error {
	if input.Ordinal == 0 || input.LogicalTime == 0 || input.Now.IsZero() || input.Now.Location() != time.UTC ||
		input.Instrument.Product != domain.ProductSpot || input.Sizing.InstrumentMetadata.Instrument != input.Instrument ||
		input.Evidence.MarketViewRevision == 0 || input.Evidence.PrimaryCandleViewRevision == 0 ||
		input.Evidence.HigherCandleViewRevision == 0 {
		return fmt.Errorf("owner_console_mean_reversion_input_invalid")
	}
	inputs.mutex.Lock()
	inputs.input, inputs.set = input, true
	inputs.mutex.Unlock()
	return nil
}

func (inputs *ownerConsoleMeanReversionInputContext) current() (meanreversion.Input, error) {
	inputs.mutex.RLock()
	defer inputs.mutex.RUnlock()
	if !inputs.set {
		return meanreversion.Input{}, fmt.Errorf("owner_console_mean_reversion_input_unavailable")
	}
	return inputs.input, nil
}

// Current returns the conservative risk projection for the current input.
func (inputs *ownerConsoleMeanReversionInputContext) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	input, err := inputs.current()
	if err != nil {
		return risk.Observations{}, nil, time.Time{}, fmt.Errorf("owner_console_mean_reversion_risk_input_unavailable")
	}
	zero := ownerConsolePercent("0")
	one := ownerConsolePercent("1")
	openOrders, quality := uint32(0), uint8(100)
	queueLag, clockDrift := time.Duration(0), time.Duration(0)
	problem := !input.MarketHealthy || !input.MarketDataQualityPass || input.ExchangeRiskPaused
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	observations := risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero, Rolling24HourLoss: &zero,
		StrategyLoss: &zero, AssetExposure: &zero, CombinedExposure: &zero, ExchangeExposure: &zero,
		Reserve: &one, ReservedCapital: &zero, Spread: &input.Spread, Slippage: &zero, OpenOrders: &openOrders,
		BookAge: &input.BookAge, QueueLag: &queueLag, ClockDrift: &clockDrift, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
			AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
			DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}
	return observations, []risk.Policy{policy}, input.Now, nil
}

type ownerConsoleMeanReversionDynamicBroker struct {
	claim      backtest.JobClaim
	inputs     *ownerConsoleMeanReversionInputContext
	guard      simulation.BoundaryGuard
	randomness *runtimecore.Randomness
	liquidity  *simulation.LiquidityLedger
}

func newOwnerConsoleMeanReversionDynamicBroker(claim backtest.JobClaim, inputs *ownerConsoleMeanReversionInputContext,
	guard simulation.BoundaryGuard) (*ownerConsoleMeanReversionDynamicBroker, error) {
	seed, err := hex.DecodeString(claim.Manifest.Seed)
	if err != nil {
		return nil, fmt.Errorf("owner_console_worker_seed_invalid")
	}
	randomness, err := runtimecore.NewRandomness(seed)
	if err != nil {
		return nil, err
	}
	return &ownerConsoleMeanReversionDynamicBroker{claim: claim, inputs: inputs, guard: guard, randomness: randomness,
		liquidity: simulation.NewLiquidityLedger()}, nil
}

// Submit builds exact per-input simulation models and performs no network I/O.
func (broker *ownerConsoleMeanReversionDynamicBroker) Submit(ctx context.Context, plan execution.SimulatedPlan) ([]execution.OrderEvent, error) {
	input, err := broker.inputs.current()
	if err != nil || input.Evidence.FeeModelID != broker.claim.Configuration.Models.Fee ||
		input.Evidence.LatencyModelID != broker.claim.Configuration.Models.Latency {
		return nil, fmt.Errorf("owner_console_simulation_model_mismatch")
	}
	models, err := ownerConsoleMeanReversionBrokerModels(input, broker.claim)
	if err != nil {
		return nil, err
	}
	exchange := broker.claim.ExchangeID
	if exchange == "" {
		exchange = "binance"
	}
	simulated, err := simulation.NewBroker(broker.randomness, ownerConsoleMeanReversionInputTimeline{input: input, exchange: exchange},
		ownerConsoleMeanReversionInputMetadata{input: input}, broker.guard, broker.liquidity, models)
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
func (broker *ownerConsoleMeanReversionDynamicBroker) Cancel(context.Context, domain.VirtualOrderID, string) ([]execution.OrderEvent, error) {
	return nil, fmt.Errorf("owner_console_simulation_order_not_active")
}

func ownerConsoleMeanReversionBrokerModels(input meanreversion.Input,
	claim backtest.JobClaim) (simulation.BrokerModels, error) {
	namespace := claim.Manifest.Models
	if namespace.FillDomain == "" || input.Evidence.FillModelID != namespace.FillDomain ||
		input.Evidence.LatencyModelID != "fixed-zero-v1" {
		return simulation.BrokerModels{}, fmt.Errorf("owner_console_simulation_model_mismatch")
	}
	zeroRate, _ := domain.ParseRate("0")
	zeroPercent, _ := domain.ParsePercent("0")
	onePercent, _ := domain.ParsePercent("1")
	fee, err := stressedRate(input.Sizing.EntryFeeRate, claim.CostStressBPS)
	if err != nil {
		return simulation.BrokerModels{}, err
	}
	spread, err := stressedPercent(input.Spread, claim.CostStressBPS)
	if err != nil {
		return simulation.BrokerModels{}, err
	}
	slippageText, err := offlineParameter(claim.Configuration.MeanReversion.Parameters,
		"mean_reversion.maximum_simulated_slippage")
	if err != nil {
		return simulation.BrokerModels{}, err
	}
	slippage, err := domain.ParsePercent(slippageText)
	if err != nil {
		return simulation.BrokerModels{}, err
	}
	slippage, err = stressedPercent(slippage, claim.CostStressBPS)
	if err != nil {
		return simulation.BrokerModels{}, err
	}
	return simulation.BrokerModels{
		Fee: simulation.FeeModel{Version: input.Evidence.FeeModelID, TakerRate: fee,
			MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18},
		Price: simulation.PriceModel{Version: "recorded-first-executable-v1", Spread: spread,
			Slippage: slippage, Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: input.Evidence.LatencyModelID, Samples: []time.Duration{0}},
		Fill:    simulation.FillModel{Version: input.Evidence.FillModelID, PartialRatio: onePercent, QuantityScale: 18},
	}, nil
}

type ownerConsoleMeanReversionInputTimeline struct {
	input    meanreversion.Input
	exchange string
}

// AtOrAfter returns only the recorded first executable observation.
func (timeline ownerConsoleMeanReversionInputTimeline) AtOrAfter(instrument domain.Instrument, logical uint64) (simulation.BookState, bool, error) {
	if instrument != timeline.input.Instrument || timeline.input.Sizing.FirstExecutablePrice.String() == "0" {
		return simulation.BookState{}, false, nil
	}
	quantity, _ := domain.ParseQuantity("1000000000")
	level := exchangecontracts.PriceLevel{Price: timeline.input.Sizing.FirstExecutablePrice, Quantity: quantity}
	if logical <= timeline.input.LogicalTime {
		logical = timeline.input.LogicalTime + 1
	}
	if timeline.exchange != "binance" && timeline.exchange != "bybit" {
		return simulation.BookState{}, false, fmt.Errorf("owner_console_simulation_exchange_invalid")
	}
	return simulation.BookState{Exchange: timeline.exchange, Instrument: instrument, Version: timeline.input.Evidence.MarketViewRevision,
		LogicalTime: logical, Bids: []exchangecontracts.PriceLevel{level}, Asks: []exchangecontracts.PriceLevel{level}}, true, nil
}

type ownerConsoleMeanReversionInputMetadata struct{ input meanreversion.Input }

// Metadata returns the exact versioned filter set embedded in the input.
func (metadata ownerConsoleMeanReversionInputMetadata) Metadata(state simulation.BookState) (domain.InstrumentMetadata, error) {
	if state.Instrument != metadata.input.Instrument {
		return domain.InstrumentMetadata{}, fmt.Errorf("owner_console_metadata_identity_mismatch")
	}
	return metadata.input.Sizing.InstrumentMetadata, nil
}

type ownerConsoleRunRiskAudit struct{}

// Append accepts the deterministic run-local transition captured by the run output.
func (*ownerConsoleRunRiskAudit) Append(risk.AuditEvent) error { return nil }

type ownerConsoleRunRiskAlerts struct{}

// Emit records no external side effect for a credential-free offline run.
func (ownerConsoleRunRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

var _ execution.Broker = (*ownerConsoleDynamicBroker)(nil)
var _ risk.ObservationProvider = (*ownerConsoleDecisionInputContext)(nil)
var _ backtest.Processor = (*ownerConsoleInputAwareProcessor)(nil)
var _ execution.Broker = (*ownerConsoleMeanReversionDynamicBroker)(nil)
var _ risk.ObservationProvider = (*ownerConsoleMeanReversionInputContext)(nil)
var _ backtest.Processor = (*ownerConsoleMeanReversionInputAwareProcessor)(nil)
