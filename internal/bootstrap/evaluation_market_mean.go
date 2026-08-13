package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/replay"
	"axiom/internal/strategies/meanreversion"
)

func (processor *evaluationMarketProcessor) processMean(ctx context.Context, source replay.Event,
	candle exchangecontracts.Candle) (backtest.EventResult, error) {
	input, err := processor.meanInput(source, candle)
	if err != nil {
		return processor.observationResult(source, "mean_reversion_input_waiting"), nil
	}
	payload, _ := json.Marshal(input)
	result, err := processor.delegate.Process(ctx, replay.Event{Ordinal: source.Ordinal,
		LogicalTime: source.LogicalTime, Canonical: payload})
	if err != nil {
		return backtest.EventResult{}, err
	}
	return result, processor.applyMean(input, result)
}

func (processor *evaluationMarketProcessor) meanInput(source replay.Event,
	candle exchangecontracts.Candle) (meanreversion.Input, error) {
	primaryKey := evaluationCandleKey(candle.Exchange, candle.Instrument,
		processor.claim.Configuration.MeanReversion.PrimaryTimeframe)
	higherKey := evaluationCandleKey(candle.Exchange, candle.Instrument,
		processor.claim.Configuration.MeanReversion.HigherTimeframe)
	primary := append([]exchangecontracts.Candle(nil), processor.candles[primaryKey]...)
	higher := append([]exchangecontracts.Candle(nil), processor.candles[higherKey]...)
	if len(primary) == 0 || len(higher) == 0 {
		return meanreversion.Input{}, fmt.Errorf("evaluation_mean_warmup_missing")
	}
	facts, err := processor.meanMarketFacts(source, candle)
	if err != nil {
		return meanreversion.Input{}, err
	}
	coherent := evaluationHash(facts.modelID, primary[len(primary)-1].RawPayloadHash,
		higher[len(higher)-1].RawPayloadHash, fmt.Sprint(source.Ordinal))
	position := processor.meanPositions[candle.Instrument]
	position.CooldownRemaining = processor.meanCooldowns[candle.Instrument]
	return meanreversion.Input{Ordinal: source.Ordinal, LogicalTime: source.LogicalTime, Now: facts.now,
		Instrument: candle.Instrument, PrimaryCandles: primary, HigherCandles: higher,
		MarketHealthy: true, MarketDataQualityPass: true, ExchangeRiskPaused: false,
		Spread: facts.spread, BookAge: facts.bookAge, Position: position, Sizing: facts.sizing,
		Evidence: processor.meanEvidence(source, candle, primaryKey, higherKey, primary, higher, coherent, facts)}, nil
}

type evaluationMeanMarketFacts struct {
	sizing              meanreversion.SizingState
	bookRevision        uint64
	bookAge             time.Duration
	modelID, metadataID string
	spread              domain.Percent
	now                 time.Time
}

func (processor *evaluationMarketProcessor) meanMarketFacts(source replay.Event,
	candle exchangecontracts.Candle) (evaluationMeanMarketFacts, error) {
	reference, bookRevision, bookAge, modelID, err := processor.executionReference(source, candle)
	if err != nil {
		return evaluationMeanMarketFacts{}, err
	}
	metadata, metadataID, err := processor.metadata(string(candle.Exchange), candle.Instrument)
	if err != nil {
		return evaluationMeanMarketFacts{}, err
	}
	equity, available, err := processor.sizingMoney()
	if err != nil {
		return evaluationMeanMarketFacts{}, err
	}
	zeroMoney, _ := domain.ParseMoney("0")
	zeroPrice, _ := domain.ParsePrice("0")
	spread, _ := domain.ParsePercent("0.0005")
	limit, _ := domain.ParseMoney(processor.claim.Configuration.Risk.MaximumOrderNotional.Value)
	fee, err := evaluationFeeRate(processor.claim.CostStressBPS)
	if err != nil {
		return evaluationMeanMarketFacts{}, err
	}
	sizing := meanreversion.SizingState{Equity: equity, AvailableCash: available, MinimumReserve: zeroMoney,
		NotionalLimits: []domain.Money{limit}, FirstExecutablePrice: reference,
		FirstExecutableAt: evaluationDecisionTime(candle, processor.historical), GapAllowance: zeroPrice,
		SlippageAllowance: zeroPrice, EntryFeeRate: fee, ExitFeeRate: fee, InstrumentMetadata: metadata,
		CentralRiskEligible: true, LiquidityDomain: processor.claim.Manifest.Models.LiquidityDomain,
		FencingToken: source.Ordinal}
	return evaluationMeanMarketFacts{sizing: sizing, bookRevision: bookRevision, bookAge: bookAge,
		modelID: modelID, metadataID: metadataID, spread: spread,
		now: evaluationDecisionTime(candle, processor.historical)}, nil
}

func (processor *evaluationMarketProcessor) meanEvidence(source replay.Event, candle exchangecontracts.Candle,
	primaryKey, higherKey string, primary, higher []exchangecontracts.Candle, coherent string,
	facts evaluationMeanMarketFacts) meanreversion.InputEvidence {
	riskPayload, _ := json.Marshal(processor.claim.Configuration.Risk)
	return meanreversion.InputEvidence{PrimaryCandleViewID: processor.claim.EvaluationDatasetID + ":" + primaryKey,
		PrimaryCandleViewRevision: uint64(len(primary)), HigherCandleViewID: processor.claim.EvaluationDatasetID + ":" + higherKey,
		HigherCandleViewRevision: uint64(len(higher)), MarketViewID: facts.modelID, MarketViewRevision: facts.bookRevision,
		CoherentViewID: coherent, CoherentVersionVectorHash: coherent, InstrumentMetadataID: facts.metadataID,
		AssetEligibilityVersion: 1, ConfigurationSnapshotID: processor.claim.EvaluationConfigurationID,
		ConfigurationVersion: processor.claim.Configuration.SchemaVersion, ConfigurationHash: processor.meanCfg.Hash,
		StrategyVersion: processor.meanCfg.Version, StrategyHash: processor.meanCfg.Hash,
		PortfolioRevision: evaluationPortfolioRevision(processor.balances), PositionRevision: 1,
		RiskPolicyID:      "evaluation-risk:" + processor.claim.EvaluationConfigurationID,
		RiskPolicyVersion: processor.claim.Configuration.Revision, RiskPolicyHash: evaluationHash(string(riskPayload)),
		FeeModelID: processor.claim.Configuration.Models.Fee, LatencyModelID: processor.claim.Configuration.Models.Latency,
		FillModelID: processor.claim.Manifest.Models.FillDomain, SlippageModelID: "evaluation-slippage-v1",
		GapModelID: "evaluation-gap-v1", CorrelationModelID: "evaluation-combined-v1",
		CorrelationID: processor.claim.Manifest.Evaluation.CampaignID, CausationID: candle.RawPayloadHash}
}

func (processor *evaluationMarketProcessor) applyMean(input meanreversion.Input, result backtest.EventResult) error {
	var decision meanreversion.Decision
	var balances portfolio.Snapshot
	var orders []execution.Order
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		json.Unmarshal(result.Orders, &orders) != nil {
		return fmt.Errorf("evaluation_mean_result_invalid")
	}
	position := processor.meanPositions[input.Instrument]
	if position.Open && (decision.Action == meanreversion.ActionExit ||
		decision.ReasonCode == meanreversion.ReasonHoldPosition) {
		position = meanreversion.AdvanceHolding(position)
	}
	fill, filled := ownerConsoleFirstFill(orders)
	if filled && decision.Action == meanreversion.ActionEntry {
		opened, err := meanreversion.OpenPosition(fill.Price, decision.Explanation.ATR14,
			fill.Quantity, processor.meanCfg)
		if err != nil {
			return err
		}
		position, processor.meanCooldowns[input.Instrument] = opened, 0
	} else if filled && decision.Action == meanreversion.ActionExit {
		remaining, err := position.Quantity.Subtract(fill.Quantity)
		if err != nil {
			return err
		}
		zero, _ := domain.ParseQuantity("0")
		if remaining.Compare(zero) == 0 {
			position = meanreversion.PositionState{CooldownRemaining: decision.CooldownStart}
			processor.meanCooldowns[input.Instrument] = decision.CooldownStart
		} else {
			position.Quantity = remaining
		}
	} else if !position.Open && processor.meanCooldowns[input.Instrument] > 0 {
		processor.meanCooldowns[input.Instrument] = meanreversion.AdvanceCooldown(processor.meanCooldowns[input.Instrument])
		position.CooldownRemaining = processor.meanCooldowns[input.Instrument]
	}
	processor.meanPositions[input.Instrument], processor.balances = position, balances
	return nil
}

func (processor *evaluationMarketProcessor) executionReference(source replay.Event,
	candle exchangecontracts.Candle) (domain.Price, uint64, time.Duration, string, error) {
	if processor.historical {
		return candle.Close, uint64(len(processor.candles[evaluationCandleKey(candle.Exchange,
				candle.Instrument, candle.Interval)])), 0,
			"historical-candle-execution-model:" + processor.claim.Manifest.Models.ID, nil
	}
	book := processor.books[evaluationBookKey(string(candle.Exchange), candle.Instrument)]
	if book == nil || !book.valid || len(book.bids) == 0 || len(book.asks) == 0 || source.LogicalTime < book.logical {
		return domain.Price{}, 0, 0, "", fmt.Errorf("evaluation_recorded_book_unavailable")
	}
	bid, ask, ok := book.best()
	if !ok {
		return domain.Price{}, 0, 0, "", fmt.Errorf("evaluation_recorded_book_invalid")
	}
	reference := ask.Price
	if processor.positionOpen(candle.Instrument) {
		reference = bid.Price
	}
	return reference, book.revision, time.Duration(source.LogicalTime - book.logical),
		"recorded-book:" + book.exchange + ":" + book.instrument.Symbol(), nil
}

func (processor *evaluationMarketProcessor) positionOpen(instrument domain.Instrument) bool {
	if processor.claim.Manifest.StrategyVersion == "trend-following@1.0.0" {
		return processor.trendPositions[instrument].Open
	}
	return processor.meanPositions[instrument].Open
}

func (processor *evaluationMarketProcessor) sizingMoney() (domain.Money, domain.Money, error) {
	starting, err := domain.ParseMoney(processor.claim.Configuration.Portfolio.StartingCapital.Value)
	if err != nil {
		return domain.Money{}, domain.Money{}, err
	}
	if processor.balances.Revision == 0 {
		return starting, starting, nil
	}
	settlement := processor.balances.Balances[processor.balances.Numeraire]
	available, err := domain.ParseMoney(settlement.Available.String())
	if err != nil {
		return domain.Money{}, domain.Money{}, err
	}
	reserved, err := domain.ParseMoney(settlement.Reserved.String())
	if err != nil {
		return domain.Money{}, domain.Money{}, err
	}
	equity, err := available.Add(reserved)
	return equity, available, err
}

func (processor *evaluationMarketProcessor) metadata(exchange string,
	instrument domain.Instrument) (domain.InstrumentMetadata, string, error) {
	key := exchange + ":" + instrument.Symbol()
	metadata, ok := processor.claim.EvaluationMetadata[key]
	id := processor.claim.EvaluationMetadataID[key]
	if !ok || id == "" || metadata.Validate() != nil || metadata.Instrument != instrument {
		return domain.InstrumentMetadata{}, "", fmt.Errorf("evaluation_instrument_metadata_missing")
	}
	return metadata, id, nil
}

func (processor *evaluationMarketProcessor) observationResult(event replay.Event,
	reason string) backtest.EventResult {
	decision, _ := json.Marshal(map[string]any{"action": "observation_only", "reason_code": reason,
		"simulation_only": true})
	balances := json.RawMessage(`{"observation_only":true}`)
	if processor.balances.Revision > 0 {
		balances, _ = json.Marshal(processor.balances)
	}
	return backtest.EventResult{Ordinal: event.Ordinal, Decision: decision, Orders: json.RawMessage("[]"),
		ExecutionEvents: json.RawMessage("[]"), Balances: balances}
}

func (processor *evaluationMarketProcessor) replaceBook(snapshot exchangecontracts.BookSnapshot,
	ordinal, logical uint64) error {
	if snapshot.LastSequence == 0 || len(snapshot.Bids) == 0 || len(snapshot.Asks) == 0 ||
		snapshot.RawPayloadHash == "" || logical == 0 {
		return fmt.Errorf("evaluation_book_snapshot_invalid")
	}
	book := &evaluationBook{exchange: string(snapshot.Exchange), instrument: snapshot.Instrument,
		sequence: snapshot.LastSequence, revision: 1, logical: logical, valid: true,
		ordinal: ordinal, receivedAt: snapshot.ReceivedAt.UTC, lastHash: snapshot.RawPayloadHash,
		bids: make(map[string]exchangecontracts.PriceLevel, len(snapshot.Bids)),
		asks: make(map[string]exchangecontracts.PriceLevel, len(snapshot.Asks))}
	if existing := processor.books[evaluationBookKey(book.exchange, book.instrument)]; existing != nil {
		book.revision = existing.revision + 1
	}
	for _, level := range snapshot.Bids {
		book.bids[level.Price.String()] = level
	}
	for _, level := range snapshot.Asks {
		book.asks[level.Price.String()] = level
	}
	if _, _, ok := book.best(); !ok {
		return fmt.Errorf("evaluation_book_snapshot_crossed")
	}
	processor.books[evaluationBookKey(book.exchange, book.instrument)] = book
	return nil
}

func (processor *evaluationMarketProcessor) applyDepth(update exchangecontracts.DepthUpdate,
	ordinal, logical uint64) error {
	book := processor.books[evaluationBookKey(string(update.Exchange), update.Instrument)]
	if book == nil || !book.valid {
		return nil
	}
	if update.LastSequence <= book.sequence {
		return nil
	}
	if update.FirstSequence > book.sequence+1 || update.LastSequence < update.FirstSequence || logical < book.logical {
		book.valid = false
		return fmt.Errorf("evaluation_book_sequence_gap")
	}
	zero, _ := domain.ParseQuantity("0")
	for _, level := range update.Bids {
		if level.Quantity.Compare(zero) == 0 {
			delete(book.bids, level.Price.String())
		} else {
			book.bids[level.Price.String()] = level
		}
	}
	for _, level := range update.Asks {
		if level.Quantity.Compare(zero) == 0 {
			delete(book.asks, level.Price.String())
		} else {
			book.asks[level.Price.String()] = level
		}
	}
	book.sequence, book.revision, book.logical = update.LastSequence, book.revision+1, logical
	book.ordinal, book.receivedAt, book.lastHash = ordinal, update.ReceivedAt.UTC, update.RawPayloadHash
	if _, _, ok := book.best(); !ok {
		book.valid = false
		return fmt.Errorf("evaluation_book_crossed")
	}
	return nil
}

func (book *evaluationBook) best() (exchangecontracts.PriceLevel, exchangecontracts.PriceLevel, bool) {
	bids := make([]exchangecontracts.PriceLevel, 0, len(book.bids))
	asks := make([]exchangecontracts.PriceLevel, 0, len(book.asks))
	for _, level := range book.bids {
		bids = append(bids, level)
	}
	for _, level := range book.asks {
		asks = append(asks, level)
	}
	if len(bids) == 0 || len(asks) == 0 {
		return exchangecontracts.PriceLevel{}, exchangecontracts.PriceLevel{}, false
	}
	sort.Slice(bids, func(left, right int) bool { return bids[left].Price.Compare(bids[right].Price) > 0 })
	sort.Slice(asks, func(left, right int) bool { return asks[left].Price.Compare(asks[right].Price) < 0 })
	return bids[0], asks[0], bids[0].Price.Compare(asks[0].Price) < 0
}

func evaluationCandleKey(exchange exchangecontracts.ExchangeID, instrument domain.Instrument,
	interval string) string {
	return string(exchange) + ":" + instrument.Symbol() + ":" + interval
}

func evaluationBookKey(exchange string, instrument domain.Instrument) string {
	return exchange + ":" + instrument.Symbol()
}

func evaluationDecisionTime(candle exchangecontracts.Candle, historical bool) time.Time {
	if historical {
		return candle.CloseTime.Add(2*time.Second + time.Nanosecond).UTC()
	}
	if !candle.ReceivedAt.UTC.IsZero() {
		return candle.ReceivedAt.UTC.UTC()
	}
	return candle.CloseTime.Add(2*time.Second + time.Nanosecond).UTC()
}

func evaluationFeeRate(stress int32) (domain.Rate, error) {
	base, _ := domain.ParseRate("0.001")
	return stressedRate(base, stress)
}

func evaluationPortfolioRevision(snapshot portfolio.Snapshot) uint64 {
	if snapshot.Revision == 0 {
		return 1
	}
	return snapshot.Revision
}

func evaluationHash(values ...string) string {
	digest := sha256.Sum256([]byte(fmt.Sprint(values)))
	return hex.EncodeToString(digest[:])
}

var _ backtest.Processor = (*evaluationMarketProcessor)(nil)
