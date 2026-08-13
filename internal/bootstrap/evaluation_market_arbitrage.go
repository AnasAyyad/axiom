package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/replay"
	runtimecore "axiom/internal/runtime"
	"axiom/internal/strategies/crossarb"
	"axiom/internal/strategies/triangular"
)

func (processor *evaluationMarketProcessor) processArbitrage(ctx context.Context,
	event replay.Event) (backtest.EventResult, error) {
	if processor.historical {
		return processor.observationResult(event, "order_book_history_unavailable"), nil
	}
	bucket := event.LogicalTime / uint64(time.Second)
	if bucket == 0 || bucket == processor.lastSample {
		return processor.observationResult(event, "arbitrage_sample_waiting"), nil
	}
	var result backtest.EventResult
	var evaluated bool
	var err error
	switch processor.claim.Manifest.StrategyVersion {
	case "triangular-arbitrage@1.0.0":
		result, evaluated, err = processor.processTriangular(ctx, event)
	case "cross-exchange-arbitrage@1.0.0":
		result, evaluated, err = processor.processCrossExchange(ctx, event)
	default:
		return backtest.EventResult{}, fmt.Errorf("evaluation_arbitrage_strategy_invalid")
	}
	if err != nil {
		return backtest.EventResult{}, err
	}
	if !evaluated {
		return processor.observationResult(event, "arbitrage_coherent_books_waiting"), nil
	}
	processor.lastSample = bucket
	return result, nil
}

func (processor *evaluationMarketProcessor) processTriangular(ctx context.Context,
	event replay.Event) (backtest.EventResult, bool, error) {
	input, ok, err := processor.triangularInput(event)
	if err != nil || !ok {
		return backtest.EventResult{}, false, err
	}
	prepared, projection, err := triangular.AttachCleanRecordedReduction(input,
		"evaluation/triangular/"+processor.claim.ID)
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	result, err := processor.delegate.Process(ctx, replay.Event{Ordinal: event.Ordinal,
		LogicalTime: event.LogicalTime, Canonical: payload})
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	if projection != nil {
		processor.triangularFunds = cloneEvaluationBalances(projection.AvailableBalances)
	}
	if err = processor.multilegMetrics.observeTriangular(projection); err != nil {
		return backtest.EventResult{}, false, err
	}
	return result, true, nil
}

func (processor *evaluationMarketProcessor) triangularInput(event replay.Event) (triangular.Input, bool, error) {
	instruments, err := evaluationInstruments()
	if err != nil {
		return triangular.Input{}, false, err
	}
	markets := make([]triangular.MarketInput, 0, 3)
	metadataIDs := make([]string, 0, 3)
	for _, instrument := range []domain.Instrument{instruments["BTCUSDT"], instruments["ETHUSDT"], instruments["ETHBTC"]} {
		book := processor.books[evaluationBookKey("binance", instrument)]
		market, metadataID, ok, marketErr := processor.triangularMarket(book, event)
		if marketErr != nil || !ok {
			return triangular.Input{}, false, marketErr
		}
		markets, metadataIDs = append(markets, market), append(metadataIDs, metadataID)
	}
	now := latestTriangularTime(markets)
	available, budget, reserve, recovery, fees, err := processor.triangularCapital()
	if err != nil {
		return triangular.Input{}, false, err
	}
	riskInput, err := evaluationTriangularRisk(markets, event.LogicalTime, now)
	if err != nil {
		return triangular.Input{}, false, err
	}
	metadataHash := evaluationHash(metadataIDs...)
	input := triangular.Input{Ordinal: event.Ordinal, LogicalTime: event.LogicalTime, Now: now,
		Exchange: "binance", Markets: markets, FirstDetectedOffset: event.LogicalTime,
		AvailableSettlement: available, StrategyBudget: budget, GlobalReserveFloor: reserve,
		RecoveryAllowance: recovery, FeeBalances: fees, Configuration: processor.triangularCfg,
		ConfigurationHash: processor.claim.Manifest.ConfigurationHash, InstrumentMetadataID: metadataHash,
		CentralRisk: &riskInput, Simulation: evaluationTriangularSimulation(markets, event.LogicalTime)}
	if _, err = input.EvaluationInput(); err != nil {
		return triangular.Input{}, false, err
	}
	return input, true, nil
}

func (processor *evaluationMarketProcessor) triangularMarket(book *evaluationBook,
	event replay.Event) (triangular.MarketInput, string, bool, error) {
	if book == nil || !book.valid || event.LogicalTime < book.logical ||
		event.LogicalTime-book.logical > uint64(processor.triangularCfg.MaximumBookAge) {
		return triangular.MarketInput{}, "", false, nil
	}
	snapshot, observation, err := book.recordedView()
	if err != nil {
		return triangular.MarketInput{}, "", false, err
	}
	metadata, metadataID, err := processor.metadata(book.exchange, book.instrument)
	if err != nil {
		return triangular.MarketInput{}, "", false, err
	}
	rules, err := processor.evaluationRules(book.exchange, metadata, snapshot.ReceivedAt.UTC)
	if err != nil {
		return triangular.MarketInput{}, "", false, err
	}
	return triangular.MarketInput{Snapshot: snapshot, Observation: observation, Rules: rules}, metadataID, true, nil
}

func (processor *evaluationMarketProcessor) triangularCapital() (domain.Balance, domain.Balance,
	domain.Balance, domain.Balance, map[domain.AssetSymbol]domain.Balance, error) {
	capital, err := domain.ParseBalance(processor.claim.Configuration.Portfolio.StartingCapital.Value)
	if err != nil {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil, err
	}
	available := capital
	if processor.triangularFunds != nil {
		if current, exists := processor.triangularFunds[domain.AssetSymbol("USDT")]; exists {
			available = current
		}
	}
	budget, err := domain.ParseBalance(processor.triangularCfg.MaximumCycleNotional.String())
	reserveRate, _ := domain.ParsePercent("0.15")
	recoveryRate, _ := domain.ParsePercent("0.01")
	reserve, reserveErr := domain.ScaleBalanceCeiling(available, reserveRate, 18)
	recovery, recoveryErr := domain.ScaleBalanceCeiling(budget, recoveryRate, 18)
	btcFee, _ := domain.ParseBalance("0.001")
	fees := map[domain.AssetSymbol]domain.Balance{domain.AssetSymbol("USDT"): available,
		domain.AssetSymbol("BTC"): btcFee}
	if err != nil || reserveErr != nil || recoveryErr != nil {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil,
			fmt.Errorf("evaluation_triangular_capital_invalid")
	}
	return available, budget, reserve, recovery, fees, nil
}

func evaluationTriangularSimulation(markets []triangular.MarketInput,
	logical uint64) *triangular.SimulationInput {
	latency := triangular.LatencyModel{Version: "fixed-zero-v1", LegNanos: [3]uint64{1, 1, 1}, RecoveryNanos: 1}
	result := &triangular.SimulationInput{Latency: latency, Markets: make([]triangular.TimedMarketInput, 0, 12)}
	for _, offset := range []uint64{logical + 1, logical + 2, logical + 3, logical + 4} {
		for _, market := range markets {
			result.Markets = append(result.Markets, triangular.TimedMarketInput{Offset: offset, Market: market})
		}
	}
	return result
}

func (processor *evaluationMarketProcessor) processCrossExchange(ctx context.Context,
	event replay.Event) (backtest.EventResult, bool, error) {
	input, before, market, ok, err := processor.crossExchangeInput(event)
	if err != nil || !ok {
		return backtest.EventResult{}, false, err
	}
	input.Simulation = ownerConsoleCrossExchangeFixedSimulation(market)
	prepared, projection, err := crossarb.AttachCleanRecordedReduction(input,
		"evaluation/cross-exchange/"+processor.claim.ID, before)
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	payload, err := json.Marshal(prepared)
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	result, err := processor.delegate.Process(ctx, replay.Event{Ordinal: event.Ordinal,
		LogicalTime: event.LogicalTime, Canonical: payload})
	if err != nil {
		return backtest.EventResult{}, false, err
	}
	if projection != nil {
		processor.crossFunds = cloneEvaluationVenueBalances(projection.VenueBalances)
	}
	if err = processor.multilegMetrics.observeCross(projection); err != nil {
		return backtest.EventResult{}, false, err
	}
	return result, true, nil
}

func (processor *evaluationMarketProcessor) crossExchangeInput(event replay.Event) (crossarb.Input,
	crossarb.VenueBalances, SandboxCrossExchangeMarketInput, bool, error) {
	instrument, markets, coherent, market, now, ok, err := processor.crossExchangeMarkets(event)
	if err != nil || !ok {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	before, inventories, fees, err := processor.crossExchangeCapital(instrument, markets)
	if err != nil {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	budget := processor.crossCfg.MaximumNotional
	spread, err := ownerConsoleCrossExchangeMaximumSpread(market)
	if err != nil {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	zero, _ := domain.ParsePercent("0")
	restoration, err := sandboxCrossExchangeRestoration(processor.claim.Configuration, market,
		inventories, budget, spread, zero)
	if err != nil {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	riskInput, err := evaluationCrossRisk(markets, event.LogicalTime, now)
	if err != nil {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	input := crossarb.Input{Ordinal: event.Ordinal, LogicalTime: event.LogicalTime, Now: now,
		Markets: markets, Coherent: coherent, Inventory: inventories, QuoteBudget: budget,
		FeeBalances: fees, Configuration: processor.crossCfg,
		ConfigurationHash:         processor.claim.Manifest.ConfigurationHash,
		InstrumentMetadataSetHash: market.InstrumentMetadataSetHash, Restoration: restoration, CentralRisk: &riskInput}
	if _, err = input.EvaluationInput(); err != nil {
		return crossarb.Input{}, nil, SandboxCrossExchangeMarketInput{}, false, err
	}
	return input, before, market, true, nil
}

func (processor *evaluationMarketProcessor) crossExchangeMarkets(event replay.Event) (domain.Instrument,
	[]crossarb.MarketInput, crossarb.CoherentViewInput, SandboxCrossExchangeMarketInput, time.Time, bool, error) {
	instruments, err := evaluationInstruments()
	if err != nil {
		return domain.Instrument{}, nil, crossarb.CoherentViewInput{}, SandboxCrossExchangeMarketInput{}, time.Time{}, false, err
	}
	instrument := instruments["BTCUSDT"]
	markets := make([]crossarb.MarketInput, 0, 2)
	members := make([]runtimecore.ViewReference, 0, 2)
	metadataIDs := make([]string, 0, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		book := processor.books[evaluationBookKey(exchange, instrument)]
		market, reference, metadataID, ok, marketErr := processor.crossMarket(book, event)
		if marketErr != nil || !ok {
			return domain.Instrument{}, nil, crossarb.CoherentViewInput{}, SandboxCrossExchangeMarketInput{}, time.Time{}, false, marketErr
		}
		markets, members, metadataIDs = append(markets, market), append(members, reference), append(metadataIDs, metadataID)
	}
	sort.Slice(members, func(left, right int) bool { return members[left].Key.Exchange < members[right].Key.Exchange })
	policy := runtimecore.CoherentPolicy{Version: "axiom.coherent-view-policy.v1",
		MaximumBookAge:          processor.crossCfg.MaximumBookAge,
		MaximumInterBookSkew:    processor.crossCfg.MaximumInterBookSkew,
		MaximumClockUncertainty: processor.crossCfg.MaximumClockUncertainty}
	now := members[0].ReceiveUTC
	if members[1].ReceiveUTC.After(now) {
		now = members[1].ReceiveUTC
	}
	trigger := runtimecore.AsOfTrigger{MonotonicNanos: event.LogicalTime, IngestOrdinal: event.Ordinal, UTC: now}
	encoded, _ := json.Marshal(members)
	digest := sha256.Sum256(encoded)
	coherent := crossarb.CoherentViewInput{Identity: hex.EncodeToString(digest[:]), Policy: policy,
		Trigger: trigger, Members: members}
	metadataHash := evaluationHash(metadataIDs...)
	market := SandboxCrossExchangeMarketInput{Markets: markets, Coherent: coherent, Trigger: trigger,
		InstrumentMetadataSetHash: metadataHash}
	return instrument, markets, coherent, market, now, true, nil
}

func (processor *evaluationMarketProcessor) crossMarket(book *evaluationBook,
	event replay.Event) (crossarb.MarketInput, runtimecore.ViewReference, string, bool, error) {
	if book == nil || !book.valid || event.LogicalTime < book.logical ||
		event.LogicalTime-book.logical > uint64(processor.crossCfg.MaximumBookAge) {
		return crossarb.MarketInput{}, runtimecore.ViewReference{}, "", false, nil
	}
	snapshot, observation, err := book.recordedView()
	if err != nil {
		return crossarb.MarketInput{}, runtimecore.ViewReference{}, "", false, err
	}
	metadata, metadataID, err := processor.metadata(book.exchange, book.instrument)
	if err != nil {
		return crossarb.MarketInput{}, runtimecore.ViewReference{}, "", false, err
	}
	rules, err := processor.evaluationRules(book.exchange, metadata, snapshot.ReceivedAt.UTC)
	if err != nil {
		return crossarb.MarketInput{}, runtimecore.ViewReference{}, "", false, err
	}
	market := crossarb.MarketInput{Snapshot: snapshot, Observation: observation, Rules: rules}
	view, err := evaluationBookView(book.exchange, snapshot, observation)
	if err != nil {
		return crossarb.MarketInput{}, runtimecore.ViewReference{}, "", false, err
	}
	canonical, _ := view.MarshalJSON()
	digest := sha256.Sum256(canonical)
	reference := runtimecore.ViewReference{Key: runtimecore.MarketKey{Exchange: book.exchange, Instrument: book.instrument},
		BookVersion: view.Version(), ConnectionGeneration: view.Generation(),
		ReceiveMonotonicNanos: observation.ReceivedOffsetNanos, ReceiveUTC: observation.ReceivedAt.UTC,
		IngestOrdinal: observation.IngestOrdinal, ClockOffset: 0, ClockUncertainty: time.Nanosecond,
		StateHash: hex.EncodeToString(digest[:]), CollectorInstance: "evaluation-recorder",
		CollectorRegion: processor.claim.Manifest.Evaluation.CampaignID}
	return market, reference, metadataID, true, nil
}

func (processor *evaluationMarketProcessor) crossExchangeCapital(instrument domain.Instrument,
	markets []crossarb.MarketInput) (crossarb.VenueBalances,
	[]crossarb.VenueInventory, map[string]domain.Balance, error) {
	if processor.crossFunds == nil {
		capital, ok := rational(processor.claim.Configuration.Portfolio.StartingCapital.Value)
		if !ok || capital.Sign() <= 0 || len(markets) != 2 {
			return nil, nil, nil, fmt.Errorf("evaluation_cross_capital_invalid")
		}
		perVenue := new(big.Rat).Quo(capital, big.NewRat(4, 1))
		quote, quoteErr := domain.ParseBalance(floorEvaluationDecimal(perVenue, 18))
		if quoteErr != nil {
			return nil, nil, nil, fmt.Errorf("evaluation_cross_capital_invalid")
		}
		processor.crossFunds = make(crossarb.VenueBalances, 2)
		for _, market := range markets {
			if len(market.Snapshot.Asks) == 0 {
				return nil, nil, nil, fmt.Errorf("evaluation_cross_capital_invalid")
			}
			ask, askOK := rational(market.Snapshot.Asks[0].Price.String())
			if !askOK || ask.Sign() <= 0 {
				return nil, nil, nil, fmt.Errorf("evaluation_cross_capital_invalid")
			}
			baseValue := new(big.Rat).Quo(new(big.Rat).Set(perVenue), ask)
			base, baseErr := domain.ParseBalance(floorEvaluationDecimal(baseValue, 18))
			if baseErr != nil {
				return nil, nil, nil, fmt.Errorf("evaluation_cross_capital_invalid")
			}
			processor.crossFunds[string(market.Snapshot.Exchange)] = map[domain.AssetSymbol]domain.Balance{
				instrument.Base: base, domain.AssetSymbol("USDT"): quote}
		}
	}
	before := cloneEvaluationVenueBalances(processor.crossFunds)
	totalBase, _ := before["binance"][instrument.Base].Add(before["bybit"][instrument.Base])
	totalUSDT, _ := before["binance"][domain.AssetSymbol("USDT")].Add(before["bybit"][domain.AssetSymbol("USDT")])
	inventories := make([]crossarb.VenueInventory, 0, 2)
	fees := make(map[string]domain.Balance, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		inventories = append(inventories, crossarb.VenueInventory{Owner: "evaluation-cross:" + processor.claim.ID,
			Exchange: exchange, BaseAsset: instrument.Base, OwnedBase: before[exchange][instrument.Base],
			TotalEligibleBase: totalBase, OwnedUSDT: before[exchange][domain.AssetSymbol("USDT")],
			TotalEligibleUSDT: totalUSDT, Revision: 1})
		fees[exchange+":USDT"] = before[exchange][domain.AssetSymbol("USDT")]
	}
	return before, inventories, fees, nil
}
