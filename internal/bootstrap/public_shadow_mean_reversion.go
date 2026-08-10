package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/execution"
	"axiom/internal/marketdata"
	"axiom/internal/portfolio"
	marketrecorder "axiom/internal/recorder"
	"axiom/internal/replay"
	"axiom/internal/strategies/meanreversion"
)

func (session *ownerConsoleLiveShadowSession) evaluateReadyMeanReversionInputs(ctx context.Context) error {
	for instrument, collector := range session.collectors {
		primary, primaryErr := collector.Views().CompletedCandles(session.claim.ExchangeID, instrument,
			session.meanConfig.PrimaryTimeframe)
		higher, higherErr := collector.Views().CompletedCandles(session.claim.ExchangeID, instrument,
			session.meanConfig.HigherTimeframe)
		if primaryErr != nil || higherErr != nil || primary.Version() == 0 || higher.Version() == 0 ||
			len(primary.Candles()) == 0 || len(higher.Candles()) == 0 {
			continue
		}
		latest := primary.Candles()[len(primary.Candles())-1]
		now := time.Now().UTC()
		if !latest.Closed || now.Before(latest.ReceivedAt.UTC.Add(session.meanConfig.FinalizationDelay)) ||
			!latest.CloseTime.After(session.seen[instrument]) {
			continue
		}
		health := collector.HealthSnapshot()
		book, bookErr := collector.Views().Book(session.claim.ExchangeID, instrument)
		if bookErr != nil || !health.Eligible {
			continue
		}
		input, inputErr := session.buildMeanReversionInput(instrument, primary, higher, book, now, health.Eligible)
		if inputErr != nil {
			return inputErr
		}
		if activityErr := session.recordShadowActivity(ctx,
			session.evaluatingShadowActivity(now, instrument)); activityErr != nil {
			return activityErr
		}
		if processErr := session.recordAndProcessMeanReversion(ctx, &input); processErr != nil {
			return processErr
		}
		session.seen[instrument] = latest.CloseTime
	}
	return nil
}

func (session *ownerConsoleLiveShadowSession) buildMeanReversionInput(instrument domain.Instrument,
	primaryView, higherView marketdata.CandleView, book marketdata.BookView, now time.Time,
	marketHealthy bool) (meanreversion.Input, error) {
	metadata, exists := session.metadata[instrument]
	if !exists || len(book.Bids()) == 0 || len(book.Asks()) == 0 {
		return meanreversion.Input{}, fmt.Errorf("shadow_decision_evidence_incomplete")
	}
	primary := mergeOwnerConsoleCandles(session.primaryHistory[instrument], primaryView.Candles(), now)
	higher := mergeOwnerConsoleCandles(session.higherHistory[instrument], higherView.Candles(), now)
	if len(primary) == 0 || len(higher) == 0 {
		return meanreversion.Input{}, fmt.Errorf("shadow_decision_candles_incomplete")
	}
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	position := session.meanPositions[instrument]
	position.CooldownRemaining = session.cooldowns[instrument]
	sizing, spread, logical, err := session.meanReversionSizing(metadata, book, now, position)
	if err != nil {
		return meanreversion.Input{}, err
	}
	evidence := session.meanReversionEvidence(instrument, primaryView, higherView, book, primary, higher)
	return meanreversion.Input{Ordinal: 0, LogicalTime: logical, Now: now, Instrument: instrument,
		PrimaryCandles: primary, HigherCandles: higher, MarketHealthy: marketHealthy,
		MarketDataQualityPass: marketHealthy, ExchangeRiskPaused: false, Spread: spread,
		BookAge: ownerConsoleBookAge(logical, book.Observation().PublishedOffsetNanos), Position: position,
		Sizing: sizing, Evidence: evidence}, nil
}

func (session *ownerConsoleLiveShadowSession) meanReversionSizing(metadata domain.InstrumentMetadata,
	book marketdata.BookView, now time.Time, position meanreversion.PositionState,
) (meanreversion.SizingState, domain.Percent, uint64, error) {
	reference := book.Asks()[0].Price
	if position.Open {
		reference = book.Bids()[0].Price
	}
	equity, available, reserve, err := session.shadowSizingMoney()
	if err != nil {
		return meanreversion.SizingState{}, domain.Percent{}, 0, err
	}
	feeRate, err := publicShadowFeeRate(session.claim.Configuration.Models.Fee)
	if err != nil {
		return meanreversion.SizingState{}, domain.Percent{}, 0, err
	}
	zeroPrice, _ := domain.ParsePrice("0")
	orderLimit, err := domain.ParseMoney(session.claim.Configuration.Risk.MaximumOrderNotional.Value)
	if err != nil {
		return meanreversion.SizingState{}, domain.Percent{}, 0, err
	}
	spread, err := domain.CalculateRelativeSpread(book.Bids()[0].Price, book.Asks()[0].Price, 18)
	if err != nil {
		return meanreversion.SizingState{}, domain.Percent{}, 0, err
	}
	logical := session.client.MonotonicOffset()
	if logical == 0 {
		return meanreversion.SizingState{}, domain.Percent{}, 0,
			fmt.Errorf("shadow_monotonic_time_unavailable")
	}
	sizing := meanreversion.SizingState{Equity: equity, AvailableCash: available, MinimumReserve: reserve,
		NotionalLimits: []domain.Money{orderLimit}, FirstExecutablePrice: reference, FirstExecutableAt: now,
		GapAllowance: zeroPrice, SlippageAllowance: zeroPrice, EntryFeeRate: feeRate, ExitFeeRate: feeRate,
		InstrumentMetadata: metadata, CentralRiskEligible: session.entries.Load(),
		LiquidityDomain: session.claim.Models.LiquidityDomain, FencingToken: logical}
	return sizing, spread, logical, nil
}

func (session *ownerConsoleLiveShadowSession) meanReversionEvidence(instrument domain.Instrument,
	primaryView, higherView marketdata.CandleView, book marketdata.BookView,
	primary, higher []exchangecontracts.Candle,
) meanreversion.InputEvidence {
	coherent := ownerConsoleLocalHash([]byte(fmt.Sprintf("%s:%s:%d:%d:%d:%s:%s",
		session.claim.ExchangeID, instrument.Symbol(), primaryView.Version(), higherView.Version(), book.Version(),
		primary[len(primary)-1].RawPayloadHash, higher[len(higher)-1].RawPayloadHash)))
	riskPayload, _ := json.Marshal(session.claim.Configuration.Risk)
	return meanreversion.InputEvidence{
		PrimaryCandleViewID:       session.claim.ExchangeID + "-" + instrument.Symbol() + "-" + session.meanConfig.PrimaryTimeframe,
		PrimaryCandleViewRevision: primaryView.Version(),
		HigherCandleViewID:        session.claim.ExchangeID + "-" + instrument.Symbol() + "-" + session.meanConfig.HigherTimeframe,
		HigherCandleViewRevision:  higherView.Version(),
		MarketViewID:              session.claim.ExchangeID + "-" + instrument.Symbol() + "-book",
		MarketViewRevision:        book.Version(), CoherentViewID: coherent, CoherentVersionVectorHash: coherent,
		InstrumentMetadataID: session.metadataIDs[instrument], AssetEligibilityVersion: 1,
		ConfigurationSnapshotID: session.claim.ConfigurationID,
		ConfigurationVersion:    session.claim.Configuration.SchemaVersion,
		ConfigurationHash:       session.meanConfig.Hash, StrategyVersion: session.meanConfig.Version,
		StrategyHash: session.meanConfig.Hash, PortfolioRevision: session.balances.Revision,
		PositionRevision:  positionRevision(session.balances, instrument),
		RiskPolicyID:      "configuration-risk:" + session.claim.ConfigurationID,
		RiskPolicyVersion: session.claim.Configuration.Revision, RiskPolicyHash: ownerConsoleLocalHash(riskPayload),
		FeeModelID:     session.claim.Configuration.Models.Fee,
		LatencyModelID: session.claim.Configuration.Models.Latency, FillModelID: session.claim.Models.FillDomain,
		SlippageModelID: session.claim.SlippageModelID, GapModelID: session.claim.GapModelID,
		CorrelationModelID: "strategy-set-v1", CorrelationID: session.claim.ID,
		CausationID: fmt.Sprintf("candle-%d", primary[len(primary)-1].CloseTime.UnixMilli()),
	}
}

func (session *ownerConsoleLiveShadowSession) recordAndProcessMeanReversion(ctx context.Context,
	input *meanreversion.Input) error {
	eventID := fmt.Sprintf("mean-reversion-decision-%s-%d", input.Instrument.Symbol(),
		input.PrimaryCandles[len(input.PrimaryCandles)-1].CloseTime.UnixMilli())
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
	if err = session.store.RecordMeanReversionShadowDecision(ctx, session.claim, *input, result); err != nil {
		return err
	}
	return session.applyMeanReversionResult(*input, result)
}

func (session *ownerConsoleLiveShadowSession) applyMeanReversionResult(input meanreversion.Input,
	result backtest.EventResult) error {
	var decision meanreversion.Decision
	var balances portfolio.Snapshot
	var orders []execution.Order
	if json.Unmarshal(result.Decision, &decision) != nil || json.Unmarshal(result.Balances, &balances) != nil ||
		json.Unmarshal(result.Orders, &orders) != nil {
		return fmt.Errorf("shadow_result_invalid")
	}
	session.stateMutex.Lock()
	defer session.stateMutex.Unlock()
	position := session.meanPositions[input.Instrument]
	if position.Open && (decision.Action == meanreversion.ActionExit ||
		decision.ReasonCode == meanreversion.ReasonHoldPosition) {
		position = meanreversion.AdvanceHolding(position)
	}
	fill, filled := ownerConsoleFirstFill(orders)
	if filled && decision.Action == meanreversion.ActionEntry {
		opened, err := meanreversion.OpenPosition(fill.Price, decision.Explanation.ATR14, fill.Quantity,
			session.meanConfig)
		if err != nil {
			return err
		}
		position, session.cooldowns[input.Instrument] = opened, 0
	} else if filled && decision.Action == meanreversion.ActionExit {
		remaining, err := position.Quantity.Subtract(fill.Quantity)
		if err != nil {
			return err
		}
		zero, _ := domain.ParseQuantity("0")
		if remaining.Compare(zero) == 0 {
			position = meanreversion.PositionState{CooldownRemaining: decision.CooldownStart}
			session.cooldowns[input.Instrument] = decision.CooldownStart
		} else {
			position.Quantity = remaining
		}
	} else if !position.Open && session.cooldowns[input.Instrument] > 0 {
		session.cooldowns[input.Instrument] = meanreversion.AdvanceCooldown(session.cooldowns[input.Instrument])
		position.CooldownRemaining = session.cooldowns[input.Instrument]
	}
	session.meanPositions[input.Instrument], session.balances = position, balances
	session.lastOrdinal = input.Ordinal
	return nil
}
