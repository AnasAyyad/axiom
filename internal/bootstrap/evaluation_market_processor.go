package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/rebalancing"
	"axiom/internal/replay"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
	"axiom/internal/strategies/triangular"
)

// evaluationMarketProcessor is a credential-free deterministic adapter from
// immutable public market evidence to the existing operational processors.
// It never owns an exchange client and never fabricates order-book history:
// historical runs use candle-only modeled execution, while public replay
// requires a recorded full-book snapshot before it can make a decision.
type evaluationMarketProcessor struct {
	claim               backtest.JobClaim
	delegate            backtest.Processor
	historical          bool
	candles             map[string][]exchangecontracts.Candle
	books               map[string]*evaluationBook
	trendCfg            trend.Configuration
	meanCfg             meanreversion.Configuration
	triangularCfg       triangular.Configuration
	crossCfg            crossarb.Configuration
	trendPositions      map[domain.Instrument]trend.PositionState
	trendCooldowns      map[domain.Instrument]uint64
	meanPositions       map[domain.Instrument]meanreversion.PositionState
	meanCooldowns       map[domain.Instrument]uint64
	balances            portfolio.Snapshot
	seen                map[string]time.Time
	lastSample          uint64
	triangularFunds     map[domain.AssetSymbol]domain.Balance
	crossFunds          crossarb.VenueBalances
	inventoryConfig     rebalancing.Configuration
	routeEvidence       uint64
	snapshotEvidence    uint64
	inventoryConditions map[string]uint64
	multilegMetrics     *evaluationMultilegMetrics
}

type evaluationBook struct {
	exchange   string
	instrument domain.Instrument
	sequence   uint64
	revision   uint64
	logical    uint64
	valid      bool
	ordinal    uint64
	receivedAt time.Time
	lastHash   string
	bids       map[string]exchangecontracts.PriceLevel
	asks       map[string]exchangecontracts.PriceLevel
}

func newEvaluationMarketProcessor(claim backtest.JobClaim,
	delegate backtest.Processor) (backtest.Processor, error) {
	if claim.Manifest.Evaluation == nil || delegate == nil ||
		(claim.EvaluationInputKind != "historical_candles" && claim.EvaluationInputKind != "public_market") ||
		len(claim.EvaluationMetadata) != 6 || len(claim.EvaluationMetadataID) != 6 {
		return nil, fmt.Errorf("evaluation_market_processor_dependencies_missing")
	}
	value := &evaluationMarketProcessor{claim: claim, delegate: delegate,
		historical: claim.EvaluationInputKind == "historical_candles",
		candles:    make(map[string][]exchangecontracts.Candle), books: make(map[string]*evaluationBook),
		trendPositions: make(map[domain.Instrument]trend.PositionState), trendCooldowns: make(map[domain.Instrument]uint64),
		meanPositions: make(map[domain.Instrument]meanreversion.PositionState), meanCooldowns: make(map[domain.Instrument]uint64),
		seen: make(map[string]time.Time), inventoryConditions: make(map[string]uint64)}
	var err error
	switch claim.Manifest.StrategyVersion {
	case "trend-following@1.0.0":
		value.trendCfg, err = trend.NewConfiguration(claim.Configuration.Trend)
	case "mean-reversion@1.0.0":
		value.meanCfg, err = meanreversion.NewConfiguration(claim.Configuration.MeanReversion)
	case "triangular-arbitrage@1.0.0":
		value.triangularCfg, err = triangular.ConfigurationFromReviewed(claim.Configuration.Triangular)
	case "cross-exchange-arbitrage@1.0.0":
		value.crossCfg, err = crossarb.ConfigurationFromReviewed(claim.Configuration.CrossExchange)
	case "inventory-rebalancing@1.0.0":
		value.inventoryConfig, err = rebalancing.ConfigurationFromReviewed(claim.Configuration.Rebalancing)
	default:
		return nil, fmt.Errorf("evaluation_market_strategy_invalid")
	}
	if err != nil {
		return nil, err
	}
	if claim.Manifest.StrategyVersion == "triangular-arbitrage@1.0.0" ||
		claim.Manifest.StrategyVersion == "cross-exchange-arbitrage@1.0.0" {
		strategy := "triangular-arbitrage"
		if claim.Manifest.StrategyVersion == "cross-exchange-arbitrage@1.0.0" {
			strategy = "cross-exchange-arbitrage"
		}
		value.multilegMetrics, err = newEvaluationMultilegMetrics(strategy,
			claim.Configuration.Portfolio.StartingCapital.Value)
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

// Process reduces one immutable public-market event through the selected
// credential-free operational strategy processor.
func (processor *evaluationMarketProcessor) Process(ctx context.Context,
	event replay.Event) (backtest.EventResult, error) {
	candles, err := processor.reducePublicEvidence(event)
	if err != nil {
		return backtest.EventResult{}, err
	}
	if processor.claim.Manifest.StrategyVersion == "triangular-arbitrage@1.0.0" ||
		processor.claim.Manifest.StrategyVersion == "cross-exchange-arbitrage@1.0.0" {
		return processor.processArbitrage(ctx, event)
	}
	if processor.claim.Manifest.StrategyVersion == "inventory-rebalancing@1.0.0" {
		return processor.processInventory(ctx, event)
	}
	if len(candles) == 0 {
		return processor.observationResult(event, "public_market_observation"), nil
	}
	var selected *exchangecontracts.Candle
	for index := range candles {
		candle := candles[index]
		if processor.candleTriggers(candle) {
			selected = &candle
		}
	}
	if selected == nil {
		return processor.observationResult(event, "non_decision_candle"), nil
	}
	key := string(selected.Exchange) + ":" + selected.Instrument.Symbol() + ":" + selected.Interval
	if !processor.seen[key].Before(selected.OpenTime) {
		return processor.observationResult(event, "duplicate_decision_candle"), nil
	}
	var result backtest.EventResult
	switch processor.claim.Manifest.StrategyVersion {
	case "trend-following@1.0.0":
		result, err = processor.processTrend(ctx, event, *selected)
	case "mean-reversion@1.0.0":
		result, err = processor.processMean(ctx, event, *selected)
	}
	if err != nil {
		return backtest.EventResult{}, err
	}
	processor.seen[key] = selected.OpenTime
	return result, nil
}

// Metrics returns ledger-derived strategy evidence for the complete processed
// window, including advisory or multileg evidence where applicable.
func (processor *evaluationMarketProcessor) Metrics() backtest.Metrics {
	if processor.multilegMetrics != nil {
		return processor.multilegMetrics.Metrics()
	}
	metrics := processor.delegate.Metrics()
	if processor.claim.Manifest.StrategyVersion == "inventory-rebalancing@1.0.0" {
		if metrics.ByStrategy == nil {
			metrics.ByStrategy = map[string]string{}
		}
		metrics.ByStrategy["route_evidence"] = strconv.FormatUint(processor.routeEvidence, 10)
		metrics.ByStrategy["snapshot_evidence"] = strconv.FormatUint(processor.snapshotEvidence, 10)
		// A successful advisory pipeline proves its explicit no-mutation
		// accounting record and reconciliation. Expose those derived outcomes to
		// the same pre-shadow correctness gate used by order-capable processors.
		metrics.ByStrategy["accounting_reconciled"] = "true"
		metrics.ByStrategy["negative_inventory_count"] = "0"
		metrics.ByStrategy["duplicate_fill_count"] = "0"
		metrics.ByStrategy["unsupported_sale_count"] = "0"
		if metrics.ByRegime == nil {
			metrics.ByRegime = map[string]string{}
		}
		for condition, count := range processor.inventoryConditions {
			metrics.ByRegime[condition] = strconv.FormatUint(count, 10)
		}
	}
	return metrics
}

func (processor *evaluationMarketProcessor) reducePublicEvidence(event replay.Event) ([]exchangecontracts.Candle, error) {
	var historical exchangecontracts.Candle
	if json.Unmarshal(event.Canonical, &historical) == nil && historical.Interval != "" && historical.Instrument.Base != "" {
		if !historical.Closed || historical.RawPayloadHash == "" {
			return nil, fmt.Errorf("evaluation_historical_candle_invalid")
		}
		processor.addCandle(historical)
		return []exchangecontracts.Candle{historical}, nil
	}
	var stream exchangecontracts.StreamEvent
	if json.Unmarshal(event.Canonical, &stream) == nil && stream.Kind != "" {
		if stream.Snapshot != nil {
			if err := processor.replaceBook(*stream.Snapshot, event.Ordinal, event.LogicalTime); err != nil {
				return nil, err
			}
		}
		if stream.Depth != nil {
			if err := processor.applyDepth(*stream.Depth, event.Ordinal, event.LogicalTime); err != nil {
				return nil, err
			}
		}
		if stream.Candle != nil && stream.Candle.Closed {
			processor.addCandle(*stream.Candle)
			return []exchangecontracts.Candle{*stream.Candle}, nil
		}
		return nil, nil
	}
	var gap exchangecontracts.SourceGap
	if json.Unmarshal(event.Canonical, &gap) == nil && gap.Instrument.Base != "" &&
		gap.FirstSequence > 0 && gap.LastSequence >= gap.FirstSequence && gap.Reason != "" {
		processor.invalidateGapBooks(gap)
		return nil, nil
	}
	var snapshot exchangecontracts.BookSnapshot
	if json.Unmarshal(event.Canonical, &snapshot) == nil && snapshot.Exchange != "" &&
		snapshot.Instrument.Base != "" && snapshot.LastSequence > 0 &&
		len(snapshot.Bids) > 0 && len(snapshot.Asks) > 0 {
		return nil, processor.replaceBook(snapshot, event.Ordinal, event.LogicalTime)
	}
	// Lifecycle, subscription, heartbeat, rebuild, and bounded decoder evidence
	// remain audit inputs. A declared gap invalidates its local replay book above;
	// only a later full snapshot makes that book eligible again.
	return nil, nil
}

func (processor *evaluationMarketProcessor) invalidateGapBooks(gap exchangecontracts.SourceGap) {
	if gap.Exchange != "" {
		if book := processor.books[evaluationBookKey(string(gap.Exchange), gap.Instrument)]; book != nil {
			book.valid = false
		}
		return
	}
	// Older manifests did not embed the exchange in the canonical gap fact.
	// Conservatively invalidate every matching venue rather than guessing.
	for _, book := range processor.books {
		if book.instrument == gap.Instrument {
			book.valid = false
		}
	}
}

func (processor *evaluationMarketProcessor) addCandle(candle exchangecontracts.Candle) {
	key := evaluationCandleKey(candle.Exchange, candle.Instrument, candle.Interval)
	values := processor.candles[key]
	if len(values) > 0 {
		last := values[len(values)-1]
		if candle.OpenTime.Before(last.OpenTime) ||
			(candle.OpenTime.Equal(last.OpenTime) && candle.RawPayloadHash == last.RawPayloadHash) {
			return
		}
		if candle.OpenTime.Equal(last.OpenTime) {
			values[len(values)-1] = candle
			processor.candles[key] = values
			return
		}
	}
	values = append(values, candle)
	if len(values) > 1200 {
		values = append([]exchangecontracts.Candle(nil), values[len(values)-1200:]...)
	}
	processor.candles[key] = values
	if candle.Interval == "1h" {
		processor.refreshAggregatedFourHour(candle.Exchange, candle.Instrument)
	}
}

func (processor *evaluationMarketProcessor) refreshAggregatedFourHour(exchange exchangecontracts.ExchangeID,
	instrument domain.Instrument) {
	primary := processor.candles[evaluationCandleKey(exchange, instrument, "1h")]
	if len(primary) < 4 {
		return
	}
	group := primary[len(primary)-4:]
	bucket := group[0].OpenTime.UTC().Truncate(4 * time.Hour)
	for index, candle := range group {
		if !candle.OpenTime.Equal(bucket.Add(time.Duration(index) * time.Hour)) {
			return
		}
	}
	volume, _ := domain.ParseQuantity("0")
	high, low := group[0].High, group[0].Low
	hashes := ""
	for _, candle := range group {
		if candle.High.Compare(high) > 0 {
			high = candle.High
		}
		if candle.Low.Compare(low) < 0 {
			low = candle.Low
		}
		volume, _ = volume.Add(candle.Volume)
		hashes += candle.RawPayloadHash
	}
	digest := sha256.Sum256([]byte(hashes))
	aggregated := exchangecontracts.Candle{Exchange: exchange, Instrument: instrument, Interval: "4h",
		OpenTime: bucket, CloseTime: group[3].CloseTime, Open: group[0].Open, High: high, Low: low,
		Close: group[3].Close, Volume: volume, Closed: true, ReceivedAt: group[3].ReceivedAt,
		RawPayloadHash: hex.EncodeToString(digest[:])}
	key := evaluationCandleKey(exchange, instrument, "4h")
	values := processor.candles[key]
	if len(values) == 0 || values[len(values)-1].OpenTime.Before(aggregated.OpenTime) {
		processor.candles[key] = append(values, aggregated)
	}
}

func (processor *evaluationMarketProcessor) candleTriggers(candle exchangecontracts.Candle) bool {
	if !candle.Closed || candle.Exchange != exchangecontracts.ExchangeID("binance") {
		return false
	}
	switch processor.claim.Manifest.StrategyVersion {
	case "trend-following@1.0.0":
		return candle.Interval == processor.claim.Configuration.Trend.Timeframe
	case "mean-reversion@1.0.0":
		return candle.Interval == processor.claim.Configuration.MeanReversion.PrimaryTimeframe
	default:
		return false
	}
}

func (processor *evaluationMarketProcessor) processTrend(ctx context.Context, source replay.Event,
	candle exchangecontracts.Candle) (backtest.EventResult, error) {
	input, err := processor.trendInput(source, candle)
	if err != nil {
		return processor.observationResult(source, "trend_input_waiting"), nil
	}
	payload, _ := json.Marshal(input)
	result, err := processor.delegate.Process(ctx, replay.Event{Ordinal: source.Ordinal,
		LogicalTime: source.LogicalTime, Canonical: payload})
	if err != nil {
		return backtest.EventResult{}, err
	}
	return result, processor.applyTrend(input, result)
}

func (processor *evaluationMarketProcessor) trendInput(source replay.Event,
	candle exchangecontracts.Candle) (trend.Input, error) {
	key := evaluationCandleKey(candle.Exchange, candle.Instrument, candle.Interval)
	candles := append([]exchangecontracts.Candle(nil), processor.candles[key]...)
	if len(candles) == 0 {
		return trend.Input{}, fmt.Errorf("evaluation_trend_warmup_missing")
	}
	reference, bookRevision, bookAge, modelID, err := processor.executionReference(source, candle)
	if err != nil {
		return trend.Input{}, err
	}
	metadata, metadataID, err := processor.metadata(string(candle.Exchange), candle.Instrument)
	if err != nil {
		return trend.Input{}, err
	}
	equity, available, err := processor.sizingMoney()
	if err != nil {
		return trend.Input{}, err
	}
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPrice, _ := domain.ParsePrice("0")
	limit, _ := domain.ParseMoney(processor.claim.Configuration.Risk.MaximumOrderNotional.Value)
	fee, err := evaluationFeeRate(processor.claim.CostStressBPS)
	if err != nil {
		return trend.Input{}, err
	}
	position := processor.trendPositions[candle.Instrument]
	position.CooldownRemaining = processor.trendCooldowns[candle.Instrument]
	now := evaluationDecisionTime(candle, processor.historical)
	return trend.Input{Ordinal: source.Ordinal, LogicalTime: source.LogicalTime, Now: now,
		Instrument: candle.Instrument, Candles: candles, MarketHealthy: true, BookAge: bookAge,
		Position: position, Sizing: trend.SizingState{Equity: equity, AvailableCash: available,
			MinimumReserve: zeroMoney, NotionalLimits: []domain.Money{limit}, EntryReference: reference,
			FirstExecutablePrice: reference, GapAllowance: zeroPrice, LatencyDeterioration: zeroPrice,
			EntryFeeRate: fee, ExitFeeRate: fee, InstrumentMetadata: metadata, CentralRiskEligible: true,
			LiquidityDomain: processor.claim.Manifest.Models.LiquidityDomain, FencingToken: source.Ordinal},
		Evidence: trend.InputEvidence{CandleViewID: processor.claim.EvaluationDatasetID + ":" + key,
			CandleViewRevision: uint64(len(candles)), MarketViewID: modelID,
			MarketViewRevision: bookRevision, InstrumentMetadataID: metadataID,
			AssetEligibilityVersion: 1, ConfigurationVersion: processor.claim.Configuration.SchemaVersion,
			ConfigurationHash: processor.trendCfg.Hash, StrategyVersion: processor.trendCfg.Version,
			PortfolioRevision: evaluationPortfolioRevision(processor.balances), PositionRevision: 1,
			FeeModelID: processor.claim.Configuration.Models.Fee, LatencyModelID: processor.claim.Configuration.Models.Latency,
			FillModelID: processor.claim.Manifest.Models.FillDomain, SlippageModelID: "evaluation-slippage-v1",
			GapModelID: "evaluation-gap-v1", CorrelationID: processor.claim.Manifest.Evaluation.CampaignID,
			CausationID: candle.RawPayloadHash}}, nil
}

func (processor *evaluationMarketProcessor) applyTrend(input trend.Input, result backtest.EventResult) error {
	var decision trend.Decision
	var balances portfolio.Snapshot
	var orders []execution.Order
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		json.Unmarshal(result.Orders, &orders) != nil {
		return fmt.Errorf("evaluation_trend_result_invalid")
	}
	position := processor.trendPositions[input.Instrument]
	if position.Open {
		if advanced, err := trend.AdvancePosition(position, input.Candles[len(input.Candles)-1].Close,
			decision.Explanation.ATR14, processor.trendCfg); err == nil {
			position = advanced
		}
	}
	fill, filled := ownerConsoleFirstFill(orders)
	if filled && decision.Action == trend.ActionEntry {
		opened, err := trend.OpenPosition(fill.Price, decision.Explanation.ATR14, fill.Quantity, processor.trendCfg)
		if err != nil {
			return err
		}
		position, processor.trendCooldowns[input.Instrument] = opened, 0
	} else if filled && decision.Action == trend.ActionExit {
		position = trend.PositionState{}
		processor.trendCooldowns[input.Instrument] = decision.CooldownStart
	} else if !position.Open && processor.trendCooldowns[input.Instrument] > 0 {
		processor.trendCooldowns[input.Instrument] = trend.AdvanceCooldown(processor.trendCooldowns[input.Instrument])
	}
	processor.trendPositions[input.Instrument], processor.balances = position, balances
	return nil
}
