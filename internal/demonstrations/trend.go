// Package demonstrations contains synthetic, deterministic owner walkthroughs.
// A demonstration is not historical data or profitability evidence. Each
// scenario must call the same production strategy and shared-pipeline packages
// used by durable offline runs.
package demonstrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/config"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
	"axiom/internal/strategies/trend"
)

// TrendFollowingID is the stable owner-facing identifier for the bundled
// synthetic Trend Following walkthrough.
const TrendFollowingID = "trend-following-basics"

// Result is one fully deterministic walkthrough outcome. The canonical event
// payloads are retained only so the API layer can present a safe, structured
// explanation; no market credentials or external transport are involved.
type Result struct {
	ID                string               `json:"id"`
	StrategyID        string               `json:"strategy_id"`
	StrategyVersion   string               `json:"strategy_version"`
	Synthetic         bool                 `json:"synthetic"`
	AdvisoryOnly      bool                 `json:"advisory_only"`
	AdvisoryEvidence  json.RawMessage      `json:"advisory_evidence,omitempty"`
	ConfigurationHash string               `json:"configuration_hash"`
	Accepted          backtest.EventResult `json:"accepted"`
	Rejected          backtest.EventResult `json:"rejected"`
	Metrics           backtest.Metrics     `json:"metrics"`
	ResultHash        string               `json:"result_hash"`
}

// RunTrendFollowing executes the complete shared Trend Following pipeline
// against a deterministic synthetic input. It intentionally never opens a
// dataset, account, network client, or credential boundary.
func RunTrendFollowing(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	processor, acceptedInput, configuration, err := newTrendFollowingProcessor()
	if err != nil {
		return Result{}, err
	}
	accepted, err := processTrendInput(ctx, processor, acceptedInput)
	if err != nil || len(accepted.Orders) == 0 || len(accepted.ExecutionEvents) == 0 {
		return Result{}, fmt.Errorf("demonstration_pipeline_incomplete")
	}
	rejectedInput := acceptedInput
	rejectedInput.Ordinal++
	rejectedInput.LogicalTime++
	rejectedInput.MarketHealthy = false
	rejected, err := processTrendInput(ctx, processor, rejectedInput)
	if err != nil || len(rejected.Orders) == 0 || string(rejected.Orders) != "[]" {
		return Result{}, fmt.Errorf("demonstration_rejection_incomplete")
	}
	result := Result{
		ID:                TrendFollowingID,
		StrategyID:        "trend-following",
		StrategyVersion:   "trend-following@1.0.0",
		Synthetic:         true,
		ConfigurationHash: configuration.Hash,
		Accepted:          accepted,
		Rejected:          rejected,
		Metrics:           processor.Metrics(),
	}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func newTrendFollowingProcessor() (*trend.OperationalProcessor, trend.Input, trend.Configuration, error) {
	configuration, err := trend.NewConfiguration(config.DefaultConfiguration().Trend)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	evaluator, err := trend.NewEvaluator(configuration)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	input, err := trendInput(configuration)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	adapter, err := trend.NewAdapter(evaluator)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	owned, err := trendPortfolio(configuration.Hash)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	registry := portfolio.NewAssetRegistry()
	liquidity := portfolio.NewLiquidityPool()
	depth, err := domain.ParseQuantity("1")
	if err != nil || liquidity.Open(input.Sizing.LiquidityDomain, depth) != nil {
		return nil, trend.Input{}, trend.Configuration{}, fmt.Errorf("demonstration_liquidity_invalid")
	}
	allocator, err := portfolio.NewAllocator(owned, registry, liquidity)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	pipelineAllocator, err := portfolio.NewPipelineAllocator(allocator)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	vault := portfolio.NewApprovalVault()
	riskEngine, err := risk.NewEngine(demoRiskAudit{}, demoRiskAlerts{})
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	if err = riskEngine.ManualTransition(risk.StateNormal, demoRecovery(input.Now)); err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	pipelineRisk, err := risk.NewPipelineEngine(riskEngine, vault, registry,
		demoRiskInputs{at: input.Now.Add(time.Nanosecond)})
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	planner, err := trend.NewPlanner("paper", input.Sizing.LiquidityDomain, adapter)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	eligibilityPlanner, err := portfolio.NewEligibilityPlanner(planner, vault, registry)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	guard, err := portfolio.NewBrokerGuard(owned, registry)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	broker, err := trendBroker(input, guard)
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	pipeline, err := backtest.NewPipelineProcessor(backtest.PipelineDependencies{
		Strategy: adapter, Allocator: pipelineAllocator, Risk: pipelineRisk,
		Planner: eligibilityPlanner, Broker: broker,
		Reduce:  pipelineAllocator.ReduceSimulation,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} },
	})
	if err != nil {
		return nil, trend.Input{}, trend.Configuration{}, err
	}
	operational, err := trend.NewOperationalProcessor(evaluator, pipeline, owned)
	return operational, input, configuration, err
}

func processTrendInput(ctx context.Context, processor *trend.OperationalProcessor, input trend.Input) (backtest.EventResult, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	return processor.Process(ctx, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: canonical})
}

func trendPortfolio(configurationHash string) (*portfolio.Portfolio, error) {
	runID, runErr := domain.NewRunID("guided-trend-run")
	portfolioID, portfolioErr := domain.NewPortfolioID("guided-trend-portfolio")
	accountID, accountErr := domain.NewVirtualAccountID("guided-trend-account")
	if runErr != nil || portfolioErr != nil || accountErr != nil {
		return nil, fmt.Errorf("demonstration_identity_invalid")
	}
	return portfolio.InitializeV1ATrend(runID, portfolioID, accountID, configurationHash,
		accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), Sequence: 1})
}

func trendInput(configuration trend.Configuration) (trend.Input, error) {
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		return trend.Input{}, err
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]exchangecontracts.Candle, 200)
	for index := range candles {
		closeValue := 100 + index
		if index == len(candles)-1 {
			closeValue = 301
		}
		open := start.Add(time.Duration(index) * 4 * time.Hour)
		candles[index], err = demoCandle(instrument, open, closeValue, index+1)
		if err != nil {
			return trend.Input{}, err
		}
	}
	priceTick, err := domain.ParsePrice("0.01")
	if err != nil {
		return trend.Input{}, err
	}
	quantityStep, err := domain.ParseQuantity("0.0001")
	if err != nil {
		return trend.Input{}, err
	}
	minimumNotional, err := domain.ParseNotional("10")
	if err != nil {
		return trend.Input{}, err
	}
	equity, err := domain.ParseMoney("500")
	if err != nil {
		return trend.Input{}, err
	}
	entry, err := domain.ParsePrice("300")
	if err != nil {
		return trend.Input{}, err
	}
	gap, err := domain.ParsePrice("0.5")
	if err != nil {
		return trend.Input{}, err
	}
	deterioration, err := domain.ParsePrice("0.1")
	if err != nil {
		return trend.Input{}, err
	}
	fee, err := domain.ParseRate("0.001")
	if err != nil {
		return trend.Input{}, err
	}
	return trend.Input{Ordinal: 200, LogicalTime: uint64(100 * time.Second), Now: candles[len(candles)-1].ReceivedAt.UTC.Add(3 * time.Second),
		Instrument: instrument, Candles: candles, MarketHealthy: true, BookAge: time.Millisecond,
		Sizing: trend.SizingState{Equity: equity, AvailableCash: equity, MinimumReserve: mustMoney("75"),
			NotionalLimits: []domain.Money{mustMoney("150")}, EntryReference: entry, FirstExecutablePrice: entry,
			GapAllowance: gap, LatencyDeterioration: deterioration, EntryFeeRate: fee, ExitFeeRate: fee,
			InstrumentMetadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: start,
				PriceTick: priceTick, QuantityStep: quantityStep, MinimumQuantity: quantityStep, MinimumNotional: minimumNotional},
			CentralRiskEligible: true, LiquidityDomain: "guided-trend-liquidity", FencingToken: 1},
		Evidence: trend.InputEvidence{CandleViewID: "guided-candles", CandleViewRevision: 200,
			MarketViewID: "guided-book", MarketViewRevision: 1, InstrumentMetadataID: "guided-metadata",
			AssetEligibilityVersion: 1, ConfigurationVersion: "guided.config.v1", ConfigurationHash: configuration.Hash,
			StrategyVersion: configuration.Version, PortfolioRevision: 1, PositionRevision: 1,
			FeeModelID: "guided-fee-v1", LatencyModelID: "guided-latency-v1", FillModelID: "guided-fill-v1",
			SlippageModelID: "guided-slippage-v1", GapModelID: "guided-gap-v1", CorrelationID: "guided-correlation",
			CausationID: "guided-trend-input"}}, nil
}

func demoCandle(instrument domain.Instrument, open time.Time, closeValue, sequence int) (exchangecontracts.Candle, error) {
	price := func(value int) (domain.Price, error) { return domain.ParsePrice(strconv.Itoa(value)) }
	opening, err := price(closeValue - 1)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	high, err := price(closeValue + 1)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	low, err := price(closeValue - 2)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	closePrice, err := price(closeValue)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	volume, err := domain.ParseQuantity("1")
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	close := open.Add(4 * time.Hour)
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: "4h", OpenTime: open, CloseTime: close,
		Open: opening, High: high, Low: low, Close: closePrice, Volume: volume, Closed: true,
		ReceivedAt: domain.EventTime{UTC: close, Sequence: uint64(sequence)}, RawPayloadHash: fmt.Sprintf("guided-candle-%03d", sequence)}, nil
}

func trendBroker(input trend.Input, guard simulation.BoundaryGuard) (*simulation.SimulatedBroker, error) {
	randomness, err := runtimecore.NewRandomness([]byte(strings.Repeat("g", 32)))
	if err != nil {
		return nil, err
	}
	zeroRate, _ := domain.ParseRate("0")
	feeRate, _ := domain.ParseRate("0.001")
	zeroPercent, _ := domain.ParsePercent("0")
	partial, _ := domain.ParsePercent("0.5")
	quantity, _ := domain.ParseQuantity("1")
	bid, _ := domain.ParsePrice("299.99")
	state := simulation.BookState{Exchange: "binance", Instrument: input.Instrument, Version: 1,
		LogicalTime: input.LogicalTime + uint64(time.Millisecond), Bids: []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}},
		Asks: []exchangecontracts.PriceLevel{{Price: input.Sizing.EntryReference, Quantity: quantity}}}
	models := simulation.BrokerModels{Fee: simulation.FeeModel{Version: "guided-fee-v1", TakerRate: feeRate, MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18},
		Price:   simulation.PriceModel{Version: "guided-price-v1", Spread: zeroPercent, Slippage: zeroPercent, Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18},
		Latency: simulation.LatencyModel{Version: "guided-latency-v1", Samples: []time.Duration{time.Millisecond}},
		Fill:    simulation.FillModel{Version: "guided-fill-v1", PartialRatio: partial, QuantityScale: 18}}
	return simulation.NewBroker(randomness, demoTimeline{state}, demoMetadata{input.Sizing.InstrumentMetadata}, guard, simulation.NewLiquidityLedger(), models)
}

type demoTimeline struct{ state simulation.BookState }

func (timeline demoTimeline) AtOrAfter(instrument domain.Instrument, logicalTime uint64) (simulation.BookState, bool, error) {
	if instrument != timeline.state.Instrument || logicalTime > timeline.state.LogicalTime {
		return simulation.BookState{}, false, nil
	}
	return timeline.state, true, nil
}

type demoMetadata struct{ metadata domain.InstrumentMetadata }

func (source demoMetadata) Metadata(simulation.BookState) (domain.InstrumentMetadata, error) {
	return source.metadata, nil
}

type demoRiskInputs struct{ at time.Time }

func (inputs demoRiskInputs) Current() (risk.Observations, []risk.Policy, time.Time, error) {
	zero, one := mustPercent("0"), mustPercent("1")
	open, quality := uint32(0), uint8(100)
	fresh := time.Millisecond
	problem := false
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero, Rolling24HourLoss: &zero, StrategyLoss: &zero,
		AssetExposure: &zero, CombinedExposure: &zero, ExchangeExposure: &zero, Reserve: &one, ReservedCapital: &zero,
		Spread: &zero, Slippage: &zero, OpenOrders: &open, BookAge: &fresh, QueueLag: &fresh, ClockDrift: &fresh, QualityScore: &quality,
		Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem, AccountingFault: &problem,
			UnknownOrder: &problem, PersistenceFault: &problem, DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}, []risk.Policy{policy}, inputs.at, nil
}
func demoRecovery(at time.Time) risk.RecoveryEvidence {
	return risk.RecoveryEvidence{Reconciled: true, PersistenceHealthy: true, BooksFresh: true, UnknownOrdersResolved: true, Reauthenticated: true, AuditDurable: true, Actor: "guided-demo", Reason: "synthetic walkthrough", At: at}
}

type demoRiskAudit struct{}

func (demoRiskAudit) Append(risk.AuditEvent) error { return nil }

type demoRiskAlerts struct{}

func (demoRiskAlerts) Emit(string, risk.Action, risk.State) error { return nil }

func mustMoney(value string) domain.Money     { parsed, _ := domain.ParseMoney(value); return parsed }
func mustPercent(value string) domain.Percent { parsed, _ := domain.ParsePercent(value); return parsed }
func resultHash(result Result) (string, error) {
	result.ResultHash = ""
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
