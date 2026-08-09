package demonstrations

import (
	"context"
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
	runtimecore "axiom/internal/runtime"
	"axiom/internal/simulation"
	"axiom/internal/strategies/meanreversion"
)

// MeanReversionID is the stable owner-facing identifier for the bundled
// synthetic Mean Reversion walkthrough.
const MeanReversionID = "mean-reversion-basics"

// RunMeanReversion executes an accepted mean-reversion entry and an explicit
// market-health rejection through the real credential-free shared pipeline.
func RunMeanReversion(ctx context.Context) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("demonstration_context_invalid")
	}
	processor, input, configuration, err := newMeanReversionProcessor()
	if err != nil {
		return Result{}, err
	}
	accepted, err := processMeanReversionInput(ctx, processor, input)
	if err != nil || len(accepted.Orders) == 0 || len(accepted.ExecutionEvents) == 0 {
		return Result{}, fmt.Errorf("demonstration_pipeline_incomplete: orders=%d executions=%d error=%v", len(accepted.Orders), len(accepted.ExecutionEvents), err)
	}
	rejectedInput := input
	rejectedInput.Ordinal++
	rejectedInput.LogicalTime++
	rejectedInput.MarketHealthy = false
	rejected, err := processMeanReversionInput(ctx, processor, rejectedInput)
	if err != nil || string(rejected.Orders) != "[]" {
		return Result{}, fmt.Errorf("demonstration_rejection_incomplete")
	}
	result := Result{ID: MeanReversionID, StrategyID: "mean-reversion", StrategyVersion: "mean-reversion@1.0.0",
		Synthetic: true, ConfigurationHash: configuration.Hash, Accepted: accepted, Rejected: rejected, Metrics: processor.Metrics()}
	hash, err := resultHash(result)
	if err != nil {
		return Result{}, err
	}
	result.ResultHash = hash
	return result, nil
}

func newMeanReversionProcessor() (*meanreversion.OperationalProcessor, meanreversion.Input, meanreversion.Configuration, error) {
	configuration, err := meanreversion.NewConfiguration(config.DefaultMultiStrategyConfiguration().MeanReversion)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	evaluator, err := meanreversion.NewEvaluator(configuration)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	input, err := meanReversionInput(configuration)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	adapter, err := meanreversion.NewAdapter(evaluator)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	owned, err := meanReversionPortfolio(configuration.Hash)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	core, err := newDemonstrationPipelineCore(owned, input.Sizing.LiquidityDomain, input.Now)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	pipeline, err := newMeanReversionDemonstrationPipeline(adapter, input, owned, core)
	if err != nil {
		return nil, meanreversion.Input{}, meanreversion.Configuration{}, err
	}
	operational, err := meanreversion.NewOperationalProcessor(evaluator, pipeline, func() (json.RawMessage, error) { return json.Marshal(owned.Snapshot()) })
	return operational, input, configuration, err
}

func meanReversionPortfolio(configurationHash string) (*portfolio.Portfolio, error) {
	runID, runErr := domain.NewRunID("guided-mean-run")
	portfolioID, portfolioErr := domain.NewPortfolioID("guided-mean-portfolio")
	accountID, accountErr := domain.NewVirtualAccountID("guided-mean-account")
	capital, capitalErr := domain.ParseBalance("500")
	if runErr != nil || portfolioErr != nil || accountErr != nil || capitalErr != nil {
		return nil, fmt.Errorf("demonstration_identity_invalid")
	}
	return portfolio.InitializeMeanReversion(runID, portfolioID, accountID, configurationHash, capital,
		accounting.NewMemoryJournal(), domain.EventTime{UTC: time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC), Sequence: 1})
}

func newMeanReversionDemonstrationPipeline(adapter *meanreversion.Adapter, input meanreversion.Input,
	owned *portfolio.Portfolio, core demonstrationPipelineCore,
) (*backtest.PipelineProcessor, error) {
	strategyPlanner, err := meanreversion.NewPlanner("paper", input.Sizing.LiquidityDomain, adapter)
	if err != nil {
		return nil, err
	}
	planner, err := portfolio.NewEligibilityPlanner(strategyPlanner, core.vault, core.registry)
	if err != nil {
		return nil, err
	}
	guard, err := portfolio.NewBrokerGuard(owned, core.registry)
	if err != nil {
		return nil, err
	}
	broker, err := meanReversionBroker(input, guard)
	if err != nil {
		return nil, err
	}
	return backtest.NewPipelineProcessor(backtest.PipelineDependencies{
		Strategy: adapter, Allocator: core.allocator, Risk: core.risk, Planner: planner, Broker: broker,
		Reduce:  core.allocator.ReduceSimulation,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} },
	})
}

func processMeanReversionInput(ctx context.Context, processor *meanreversion.OperationalProcessor, input meanreversion.Input) (backtest.EventResult, error) {
	canonical, err := json.Marshal(input)
	if err != nil {
		return backtest.EventResult{}, err
	}
	return processor.Process(ctx, replay.Event{Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: canonical})
}

func meanReversionInput(configuration meanreversion.Configuration) (meanreversion.Input, error) {
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		return meanreversion.Input{}, err
	}
	signalEnd := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	primary, higher := make([]exchangecontracts.Candle, 28), make([]exchangecontracts.Candle, 210)
	for index := range primary {
		closeValue := 100 + index%2
		if index == len(primary)-1 {
			closeValue = 95
		}
		primary[index], err = meanReversionCandle(instrument, "1h", signalEnd.Add(time.Duration(index-len(primary))*time.Hour), time.Hour, closeValue, index+1, "guided-primary-")
		if err != nil {
			return meanreversion.Input{}, err
		}
	}
	for index := range higher {
		higher[index], err = meanReversionCandle(instrument, "4h", signalEnd.Add(time.Duration(index-len(higher))*4*time.Hour), 4*time.Hour, 100, index+1, "guided-higher-")
		if err != nil {
			return meanreversion.Input{}, err
		}
	}
	spread, spreadErr := domain.ParsePercent("0.0005")
	equity, equityErr := domain.ParseMoney("500")
	reserve, reserveErr := domain.ParseMoney("75")
	limit, limitErr := domain.ParseMoney("75")
	executable, executableErr := domain.ParsePrice("95.1")
	gap, gapErr := domain.ParsePrice("0.1")
	fee, feeErr := domain.ParseRate("0.001")
	tick, tickErr := domain.ParsePrice("0.01")
	step, stepErr := domain.ParseQuantity("0.00001")
	minimum, minimumErr := domain.ParseNotional("10")
	if spreadErr != nil || equityErr != nil || reserveErr != nil || limitErr != nil || executableErr != nil || gapErr != nil || feeErr != nil || tickErr != nil || stepErr != nil || minimumErr != nil {
		return meanreversion.Input{}, fmt.Errorf("demonstration_input_invalid")
	}
	return meanreversion.Input{Ordinal: 1, LogicalTime: 100, Now: signalEnd.Add(3100 * time.Millisecond), Instrument: instrument,
		PrimaryCandles: primary, HigherCandles: higher, MarketHealthy: true, MarketDataQualityPass: true, Spread: spread, BookAge: 10 * time.Millisecond,
		Sizing:   meanreversion.SizingState{Equity: equity, AvailableCash: equity, MinimumReserve: reserve, NotionalLimits: []domain.Money{limit}, FirstExecutablePrice: executable, FirstExecutableAt: signalEnd.Add(100 * time.Millisecond), GapAllowance: gap, SlippageAllowance: gap, EntryFeeRate: fee, ExitFeeRate: fee, InstrumentMetadata: domain.InstrumentMetadata{Instrument: instrument, Version: 1, EffectiveAt: signalEnd.Add(-24 * time.Hour), PriceTick: tick, QuantityStep: step, MinimumQuantity: step, MinimumNotional: minimum}, CentralRiskEligible: true, LiquidityDomain: "guided-mean-liquidity", FencingToken: 1},
		Evidence: meanreversion.InputEvidence{PrimaryCandleViewID: "guided-primary", PrimaryCandleViewRevision: 1, HigherCandleViewID: "guided-higher", HigherCandleViewRevision: 1, MarketViewID: "guided-book", MarketViewRevision: 1, CoherentViewID: strings.Repeat("a", 64), CoherentVersionVectorHash: strings.Repeat("a", 64), InstrumentMetadataID: "guided-metadata", AssetEligibilityVersion: 1, ConfigurationSnapshotID: "guided-configuration", ConfigurationVersion: "axiom.configuration@1.2.0", ConfigurationHash: configuration.Hash, StrategyVersion: configuration.Version, StrategyHash: strings.Repeat("b", 64), PortfolioRevision: 1, PositionRevision: 1, RiskPolicyID: "guided-risk", RiskPolicyVersion: 1, RiskPolicyHash: strings.Repeat("c", 64), FeeModelID: "guided-fee-v1", LatencyModelID: "guided-latency-v1", FillModelID: "guided-fill-v1", SlippageModelID: "guided-slippage-v1", GapModelID: "guided-gap-v1", CorrelationModelID: "guided-correlation-v1", CorrelationID: "guided-mean-correlation", CausationID: "guided-mean-input"}}, nil
}

func meanReversionCandle(instrument domain.Instrument, interval string, open time.Time, duration time.Duration, closeValue, sequence int, prefix string) (exchangecontracts.Candle, error) {
	price := func(value int) (domain.Price, error) { return domain.ParsePrice(strconv.Itoa(value)) }
	opening, err := price(closeValue)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	high, err := price(closeValue + 1)
	if err != nil {
		return exchangecontracts.Candle{}, err
	}
	low, err := price(closeValue - 1)
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
	return exchangecontracts.Candle{Exchange: "binance", Instrument: instrument, Interval: interval, OpenTime: open, CloseTime: open.Add(duration), Open: opening, High: high, Low: low, Close: closePrice, Volume: volume, Closed: true, ReceivedAt: domain.EventTime{UTC: open.Add(duration + 100*time.Millisecond), Sequence: uint64(sequence)}, RawPayloadHash: prefix + strconv.Itoa(sequence)}, nil
}

func meanReversionBroker(input meanreversion.Input, guard simulation.BoundaryGuard) (*simulation.SimulatedBroker, error) {
	randomness, err := runtimecore.NewRandomness([]byte(strings.Repeat("m", 32)))
	if err != nil {
		return nil, err
	}
	zeroRate, _ := domain.ParseRate("0")
	feeRate, _ := domain.ParseRate("0.001")
	zeroPercent, _ := domain.ParsePercent("0")
	partial, _ := domain.ParsePercent("0.5")
	quantity, _ := domain.ParseQuantity("1")
	bid, _ := domain.ParsePrice("95")
	state := simulation.BookState{Exchange: "binance", Instrument: input.Instrument, Version: 1, LogicalTime: input.LogicalTime + uint64(time.Millisecond), Bids: []exchangecontracts.PriceLevel{{Price: bid, Quantity: quantity}}, Asks: []exchangecontracts.PriceLevel{{Price: input.Sizing.FirstExecutablePrice, Quantity: quantity}}}
	models := simulation.BrokerModels{Fee: simulation.FeeModel{Version: "guided-fee-v1", TakerRate: feeRate, MakerRate: zeroRate, RebateRate: zeroRate, DecimalScale: 18}, Price: simulation.PriceModel{Version: "guided-price-v1", Spread: zeroPercent, Slippage: zeroPercent, Impact: zeroPercent, AdverseSelection: zeroPercent, DecimalScale: 18}, Latency: simulation.LatencyModel{Version: "guided-latency-v1", Samples: []time.Duration{time.Millisecond}}, Fill: simulation.FillModel{Version: "guided-fill-v1", PartialRatio: partial, QuantityScale: 18}}
	return simulation.NewBroker(randomness, demoTimeline{state}, demoMetadata{input.Sizing.InstrumentMetadata}, guard, simulation.NewLiquidityLedger(), models)
}
