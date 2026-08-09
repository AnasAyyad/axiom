package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/marketdata"
	"axiom/internal/portfolio"
	marketrecorder "axiom/internal/recorder"
	"axiom/internal/replay"
	"axiom/internal/strategies/trend"
	"axiom/internal/strategies/triangular"
)

func (session *ownerConsoleLiveShadowSession) evaluateReadyInputs(ctx context.Context) error {
	switch session.claim.StrategyID {
	case "trend-following-1-0-0":
		return session.evaluateReadyTrendInputs(ctx)
	case "mean-reversion-1-0-0":
		return session.evaluateReadyMeanReversionInputs(ctx)
	case "triangular-arbitrage-1-0-0":
		return session.evaluateReadyTriangularInputs(ctx)
	default:
		return fmt.Errorf("shadow_strategy_runtime_unavailable")
	}
}

func (session *ownerConsoleLiveShadowSession) evaluateReadyTriangularInputs(ctx context.Context) error {
	if !session.entries.Load() {
		return nil
	}
	for _, collector := range session.collectors {
		if !collector.HealthSnapshot().Eligible {
			return nil
		}
	}
	now := time.Now().UTC()
	input, err := session.buildTriangularInput(ctx, now)
	if err != nil {
		if errors.Is(err, errPublicShadowTriangularMarketInputUnavailable) {
			return nil
		}
		return err
	}
	viewID, instrument, err := ownerConsoleTriangularInputView(input)
	if err != nil {
		return err
	}
	session.stateMutex.Lock()
	alreadyEvaluated := viewID == session.lastMarketViewID
	session.stateMutex.Unlock()
	if alreadyEvaluated {
		return nil
	}
	if err = session.recordShadowActivity(ctx, session.evaluatingShadowActivity(now, instrument)); err != nil {
		return err
	}
	return session.recordAndProcessTriangular(ctx, &input, viewID, instrument)
}

func (session *ownerConsoleLiveShadowSession) recordAndProcessTriangular(
	ctx context.Context,
	input *triangular.Input,
	viewID string,
	instrument domain.Instrument,
) error {
	ordinal, projection, err := session.recordTriangularInput(input, viewID, instrument)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("shadow_triangular_decision_record_failed")
	}
	result, err := session.processor.Process(ctx, replay.Event{Ordinal: ordinal,
		LogicalTime: input.LogicalTime, Canonical: payload})
	if err != nil {
		return err
	}
	balances, err := session.store.RecordTriangularShadowDecision(ctx, session.claim, *input, result)
	if err != nil {
		return err
	}
	session.applyTriangularShadowBalances(*input, viewID, balances)
	if projection != nil && (projection.Simulation.Recovery.Quarantined ||
		projection.Simulation.Saga.State == execution.PlanQuarantined) {
		session.entries.Store(false)
	}
	return nil
}

func (session *ownerConsoleLiveShadowSession) recordTriangularInput(input *triangular.Input, viewID string,
	instrument domain.Instrument,
) (uint64, *triangular.RecordedProjection, error) {
	var projection *triangular.RecordedProjection
	ordinal, err := session.decisions.RecordDecisionInputBuilt(marketrecorder.DecisionInput{
		Instrument: instrument, EventID: "triangle-view-" + viewID[:24],
		LogicalTime: input.LogicalTime, ReceivedAt: input.Now}, func(assigned uint64) ([]byte, error) {
		input.Ordinal = assigned
		prepared, projected, prepareErr := triangular.AttachCleanRecordedReduction(*input,
			"shadow/triangular/"+session.claim.ID)
		if prepareErr != nil {
			return nil, prepareErr
		}
		projection, *input = projected, prepared
		return json.Marshal(input)
	})
	if err != nil || ordinal != input.Ordinal {
		return 0, nil, fmt.Errorf("shadow_triangular_decision_record_failed")
	}
	return ordinal, projection, nil
}

func (session *ownerConsoleLiveShadowSession) applyTriangularShadowBalances(input triangular.Input, viewID string,
	balances map[domain.AssetSymbol]accounting.BalanceSnapshot,
) {
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	for asset, balance := range balances {
		session.balances.Balances[asset] = balance
	}
	if input.Reduction != nil {
		session.balances.Revision++
	}
	session.lastOrdinal, session.lastMarketViewID = input.Ordinal, viewID
}

func ownerConsoleTriangularInputView(input triangular.Input) (string, domain.Instrument, error) {
	type member struct {
		Exchange        string
		Instrument      domain.Instrument
		Sequence        uint64
		Generation      uint64
		IngestOrdinal   uint64
		PublishedOffset uint64
	}
	members := make([]member, 0, len(input.Markets))
	var primary domain.Instrument
	for _, market := range input.Markets {
		instrument := market.Snapshot.Instrument
		if instrument.Symbol() == "BTCUSDT" {
			primary = instrument
		}
		members = append(members, member{Exchange: string(market.Snapshot.Exchange), Instrument: instrument,
			Sequence: market.Snapshot.LastSequence, Generation: market.Observation.ConnectionGeneration,
			IngestOrdinal:   market.Observation.IngestOrdinal,
			PublishedOffset: market.Observation.PublishedOffsetNanos})
	}
	if len(members) != 3 || primary.Base == "" {
		return "", domain.Instrument{}, fmt.Errorf("shadow_triangular_market_view_invalid")
	}
	sort.Slice(members, func(left, right int) bool {
		return members[left].Instrument.Symbol() < members[right].Instrument.Symbol()
	})
	payload, err := json.Marshal(members)
	if err != nil {
		return "", domain.Instrument{}, fmt.Errorf("shadow_triangular_market_view_invalid")
	}
	return ownerConsoleLocalHash(payload), primary, nil
}

func (session *ownerConsoleLiveShadowSession) evaluateReadyTrendInputs(ctx context.Context) error {
	for instrument, collector := range session.collectors {
		candles, err := collector.Views().CompletedCandles(session.claim.ExchangeID, instrument, "4h")
		if err != nil || candles.Version() == 0 || len(candles.Candles()) == 0 {
			continue
		}
		latest := candles.Candles()[len(candles.Candles())-1]
		now := time.Now().UTC()
		if !latest.Closed || !now.After(latest.CloseTime.Add(session.trendConfig.FinalizationDelay)) ||
			!latest.CloseTime.After(session.seen[instrument]) {
			continue
		}
		health := collector.HealthSnapshot()
		book, bookErr := collector.Views().Book(session.claim.ExchangeID, instrument)
		if bookErr != nil || !health.Eligible {
			continue
		}
		input, inputErr := session.buildTrendInput(instrument, candles, book, now, health.Eligible)
		if inputErr != nil {
			return inputErr
		}
		if activityErr := session.recordShadowActivity(ctx,
			session.evaluatingShadowActivity(now, instrument)); activityErr != nil {
			return activityErr
		}
		if processErr := session.recordAndProcess(ctx, &input); processErr != nil {
			return processErr
		}
		session.seen[instrument] = latest.CloseTime
	}
	return nil
}

func (session *ownerConsoleLiveShadowSession) buildTrendInput(instrument domain.Instrument, candleView marketdata.CandleView,
	book marketdata.BookView, now time.Time, marketHealthy bool) (trend.Input, error) {
	metadata, exists := session.metadata[instrument]
	if !exists || len(book.Bids()) == 0 || len(book.Asks()) == 0 {
		return trend.Input{}, fmt.Errorf("shadow_decision_evidence_incomplete")
	}
	candles := mergeOwnerConsoleCandles(session.history[instrument], candleView.Candles(), now)
	if len(candles) == 0 {
		return trend.Input{}, fmt.Errorf("shadow_decision_candles_incomplete")
	}
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	position := session.positions[instrument]
	position.CooldownRemaining = session.cooldowns[instrument]
	reference := book.Asks()[0].Price
	if position.Open {
		reference = book.Bids()[0].Price
	}
	equity, available, reserve, err := session.shadowSizingMoney()
	if err != nil {
		return trend.Input{}, err
	}
	feeRate, err := publicShadowFeeRate(session.claim.Configuration.Models.Fee)
	if err != nil {
		return trend.Input{}, err
	}
	zeroPrice, _ := domain.ParsePrice("0")
	orderLimit, err := domain.ParseMoney(session.claim.Configuration.Risk.MaximumOrderNotional.Value)
	if err != nil {
		return trend.Input{}, err
	}
	bookAge := ownerConsoleBookAge(session.client.MonotonicOffset(), book.Observation().PublishedOffsetNanos)
	logical := session.client.MonotonicOffset()
	if logical == 0 {
		return trend.Input{}, fmt.Errorf("shadow_monotonic_time_unavailable")
	}
	evidence := session.ownerConsoleInputEvidence(instrument, metadata, candleView, book, candles)
	sizing := trend.SizingState{Equity: equity, AvailableCash: available, MinimumReserve: reserve,
		NotionalLimits: []domain.Money{orderLimit}, EntryReference: reference, FirstExecutablePrice: reference,
		GapAllowance: zeroPrice, LatencyDeterioration: zeroPrice, EntryFeeRate: feeRate, ExitFeeRate: feeRate,
		InstrumentMetadata: metadata, CentralRiskEligible: session.entries.Load(),
		LiquidityDomain: session.claim.Models.LiquidityDomain, FencingToken: logical}
	return trend.Input{LogicalTime: logical, Now: now, Instrument: instrument, Candles: candles,
		MarketHealthy: marketHealthy, BookAge: bookAge, Position: position,
		Sizing: sizing, Evidence: evidence}, nil
}

func (session *ownerConsoleLiveShadowSession) ownerConsoleInputEvidence(instrument domain.Instrument,
	metadata domain.InstrumentMetadata, candleView marketdata.CandleView, book marketdata.BookView,
	candles []exchangecontracts.Candle) trend.InputEvidence {
	return trend.InputEvidence{CandleViewID: session.claim.ExchangeID + "-" + instrument.Symbol() + "-4h",
		CandleViewRevision: candleView.Version(), MarketViewID: session.claim.ExchangeID + "-" + instrument.Symbol() + "-book",
		MarketViewRevision: book.Version(), InstrumentMetadataID: session.metadataIDs[instrument],
		AssetEligibilityVersion: 1, ConfigurationVersion: session.claim.Configuration.SchemaVersion,
		ConfigurationHash: session.trendConfig.Hash, StrategyVersion: session.trendConfig.Version,
		PortfolioRevision: session.balances.Revision, PositionRevision: positionRevision(session.balances, instrument),
		FeeModelID: session.claim.Configuration.Models.Fee, LatencyModelID: session.claim.Configuration.Models.Latency,
		FillModelID: session.claim.Models.FillDomain, SlippageModelID: session.claim.SlippageModelID,
		GapModelID: session.claim.GapModelID, CorrelationID: session.claim.ID,
		CausationID: fmt.Sprintf("candle-%d", candles[len(candles)-1].CloseTime.UnixMilli())}
}

func (session *ownerConsoleLiveShadowSession) shadowSizingMoney() (domain.Money, domain.Money, domain.Money, error) {
	settlement, _ := domain.ParseAssetSymbol(session.claim.Configuration.Portfolio.SettlementAsset)
	balance, exists := session.balances.Balances[settlement]
	if !exists {
		return domain.Money{}, domain.Money{}, domain.Money{}, fmt.Errorf("shadow_settlement_balance_missing")
	}
	available, err := domain.ParseMoney(balance.Available.String())
	if err != nil {
		return domain.Money{}, domain.Money{}, domain.Money{}, err
	}
	totalBalance, err := balance.Available.Add(balance.Reserved)
	if err != nil {
		return domain.Money{}, domain.Money{}, domain.Money{}, err
	}
	equity, err := domain.ParseMoney(totalBalance.String())
	capitalQuantity, quantityErr := domain.ParseQuantity(session.claim.Configuration.Portfolio.StartingCapital.Value)
	reserveRate, rateErr := domain.ParsePrice("0.15")
	reserveNotional, reserveErr := domain.CalculateNotional(reserveRate, capitalQuantity, 18)
	reserve, moneyErr := domain.ParseMoney(reserveNotional.String())
	if err != nil || quantityErr != nil || rateErr != nil || reserveErr != nil || moneyErr != nil {
		return domain.Money{}, domain.Money{}, domain.Money{}, fmt.Errorf("shadow_sizing_projection_invalid")
	}
	return equity, available, reserve, nil
}

func (session *ownerConsoleLiveShadowSession) recordAndProcess(ctx context.Context, input *trend.Input) error {
	eventID := fmt.Sprintf("decision-%s-%d", input.Instrument.Symbol(), input.Candles[len(input.Candles)-1].CloseTime.UnixMilli())
	ordinal, err := session.decisions.RecordDecisionInputBuilt(marketrecorder.DecisionInput{
		Instrument: input.Instrument, EventID: eventID, LogicalTime: input.LogicalTime, ReceivedAt: input.Now},
		func(assigned uint64) ([]byte, error) {
			input.Ordinal = assigned
			return json.Marshal(input)
		})
	if err != nil || ordinal != input.Ordinal {
		return fmt.Errorf("shadow_decision_record_failed")
	}
	payload, _ := json.Marshal(input)
	result, err := session.processor.Process(ctx, replay.Event{Ordinal: ordinal,
		LogicalTime: input.LogicalTime, Canonical: payload})
	if err != nil {
		return err
	}
	if err = session.store.RecordShadowDecision(ctx, session.claim, *input, result); err != nil {
		return err
	}
	return session.applyTrendResult(*input, result)
}

func (session *ownerConsoleLiveShadowSession) applyTrendResult(input trend.Input, result backtest.EventResult) error {
	var decision trend.Decision
	var balances portfolio.Snapshot
	var orders []execution.Order
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		json.Unmarshal(result.Orders, &orders) != nil {
		return fmt.Errorf("shadow_result_invalid")
	}
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	position := session.positions[input.Instrument]
	if position.Open {
		advanced, err := trend.AdvancePosition(position, input.Candles[len(input.Candles)-1].Close,
			decision.Explanation.ATR14, session.trendConfig)
		if err == nil {
			position = advanced
		}
	}
	fill, filled := ownerConsoleFirstFill(orders)
	if filled && decision.Action == trend.ActionEntry {
		opened, err := trend.OpenPosition(fill.Price, decision.Explanation.ATR14, fill.Quantity, session.trendConfig)
		if err != nil {
			return err
		}
		position, session.cooldowns[input.Instrument] = opened, 0
	} else if filled && decision.Action == trend.ActionExit {
		position = trend.PositionState{}
		session.cooldowns[input.Instrument] = decision.CooldownStart
	} else if !position.Open && session.cooldowns[input.Instrument] > 0 {
		session.cooldowns[input.Instrument] = trend.AdvanceCooldown(session.cooldowns[input.Instrument])
	}
	session.positions[input.Instrument], session.balances = position, balances
	session.lastOrdinal = input.Ordinal
	return nil
}

func ownerConsoleFirstFill(orders []execution.Order) (execution.FillFact, bool) {
	for _, order := range orders {
		if len(order.Fills) > 0 {
			return order.Fills[0], true
		}
	}
	return execution.FillFact{}, false
}

func mergeOwnerConsoleCandles(history, live []exchangecontracts.Candle, now time.Time) []exchangecontracts.Candle {
	items := make(map[int64]exchangecontracts.Candle, len(history)+len(live))
	for _, candle := range append(append([]exchangecontracts.Candle(nil), history...), live...) {
		if candle.Closed && !candle.CloseTime.After(now) {
			items[candle.OpenTime.UnixNano()] = candle
		}
	}
	result := make([]exchangecontracts.Candle, 0, len(items))
	for _, candle := range items {
		result = append(result, candle)
	}
	sort.Slice(result, func(left, right int) bool { return result[left].OpenTime.Before(result[right].OpenTime) })
	if len(result) > 1000 {
		result = result[len(result)-1000:]
	}
	return result
}

func publicShadowFeeRate(model string) (domain.Rate, error) {
	if model != "fixed-bps-v1" {
		return domain.Rate{}, fmt.Errorf("shadow_fee_model_unsupported")
	}
	return domain.ParseRate("0.001")
}

func ownerConsoleBookAge(current, published uint64) time.Duration {
	if published == 0 || current < published || current-published > uint64(time.Duration(1<<63-1)) {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(current - published)
}

func positionRevision(snapshot portfolio.Snapshot, instrument domain.Instrument) uint64 {
	for _, position := range snapshot.Positions {
		if position.Instrument == instrument {
			return position.Revision
		}
	}
	return 1
}
