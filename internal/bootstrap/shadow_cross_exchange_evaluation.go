package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"axiom/internal/accounting"
	"axiom/internal/domain"
	"axiom/internal/execution"
	marketrecorder "axiom/internal/recorder"
	"axiom/internal/replay"
	"axiom/internal/risk"
	"axiom/internal/strategies/crossarb"
)

func (session *ownerConsoleCrossExchangeShadowSession) evaluateReadyInput(ctx context.Context) error {
	if !session.entries.Load() {
		return nil
	}
	for _, collector := range session.collectors {
		if !collector.HealthSnapshot().Eligible {
			return nil
		}
	}
	now := time.Now().UTC()
	market, err := session.captureMarket(ctx, now)
	if err != nil {
		if errors.Is(err, errPublicShadowCrossExchangeMarketInputUnavailable) {
			return nil
		}
		return err
	}
	session.stateMutex.Lock()
	alreadyEvaluated := market.Coherent.Identity == session.lastViewID
	needsInitialization := session.balances == nil
	session.stateMutex.Unlock()
	if alreadyEvaluated {
		return nil
	}
	if needsInitialization {
		balances, initializationErr := session.store.InitializeCrossExchangeShadowInventory(ctx,
			session.claim, market.Markets, market.Trigger.UTC)
		if initializationErr != nil {
			return initializationErr
		}
		session.stateMutex.Lock()
		session.balances = balances
		session.stateMutex.Unlock()
	}
	input, err := session.buildInput(market)
	if err != nil {
		return err
	}
	instrument := market.Markets[0].Snapshot.Instrument
	if err = session.recordActivity(ctx, session.evaluatingActivity(now, instrument)); err != nil {
		return err
	}
	return session.recordAndProcess(ctx, &input, market.Coherent.Identity, instrument)
}

func (session *ownerConsoleCrossExchangeShadowSession) buildInput(market SandboxCrossExchangeMarketInput) (crossarb.Input, error) {
	session.stateMutex.Lock()
	current := cloneOwnerConsoleCrossExchangeSnapshots(session.balances)
	session.stateMutex.Unlock()
	if len(current) != 2 || len(market.Markets) != 2 {
		return crossarb.Input{}, fmt.Errorf("shadow_cross_exchange_capital_unavailable")
	}
	inventory, feeBalances, err := session.crossExchangeShadowInventory(current, market)
	if err != nil {
		return crossarb.Input{}, err
	}
	budget, err := sandboxCrossSafeQuoteBudget(market, session.configuration.MaximumNotional)
	if err != nil {
		return crossarb.Input{}, fmt.Errorf("shadow_cross_exchange_capital_invalid")
	}
	maximumSpread, err := ownerConsoleCrossExchangeMaximumSpread(market)
	if err != nil {
		return crossarb.Input{}, err
	}
	zeroPercent, _ := domain.ParsePercent("0")
	restoration, err := sandboxCrossExchangeRestoration(session.claim.Configuration, market, inventory,
		budget, maximumSpread, zeroPercent)
	if err != nil {
		return crossarb.Input{}, err
	}
	riskInput, err := session.riskInput(market, inventory, budget, maximumSpread)
	if err != nil {
		return crossarb.Input{}, err
	}
	input := crossarb.Input{Ordinal: market.Trigger.IngestOrdinal,
		LogicalTime: market.Trigger.MonotonicNanos, Now: market.Trigger.UTC,
		Markets: append([]crossarb.MarketInput(nil), market.Markets...), Coherent: market.Coherent,
		Inventory: inventory, QuoteBudget: budget, FeeBalances: feeBalances,
		Configuration: session.configuration, ConfigurationHash: session.claim.ConfigurationHash,
		InstrumentMetadataSetHash: market.InstrumentMetadataSetHash, Restoration: restoration,
		CentralRisk: &riskInput, Simulation: ownerConsoleCrossExchangeFixedSimulation(market)}
	if _, err = input.EvaluationInput(); err != nil || input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return crossarb.Input{}, fmt.Errorf("shadow_cross_exchange_input_invalid")
	}
	if _, _, _, err = input.RecordedSimulation(); err != nil {
		return crossarb.Input{}, fmt.Errorf("shadow_cross_exchange_simulation_input_invalid: %w", err)
	}
	return input, nil
}

func (session *ownerConsoleCrossExchangeShadowSession) crossExchangeShadowInventory(
	current map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot,
	market SandboxCrossExchangeMarketInput,
) ([]crossarb.VenueInventory, map[string]domain.Balance, error) {
	base := market.Markets[0].Snapshot.Instrument.Base
	totalBase, _ := domain.ParseBalance("0")
	totalUSDT, _ := domain.ParseBalance("0")
	for _, exchange := range []string{"binance", "bybit"} {
		var err error
		totalBase, err = totalBase.Add(current[exchange][base].Available)
		if err == nil {
			totalUSDT, err = totalUSDT.Add(current[exchange][domain.AssetSymbol("USDT")].Available)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("shadow_cross_exchange_capital_invalid")
		}
	}
	inventory := make([]crossarb.VenueInventory, 0, 2)
	feeBalances := make(map[string]domain.Balance, 2)
	for _, exchange := range []string{"binance", "bybit"} {
		balances := current[exchange]
		inventory = append(inventory, crossarb.VenueInventory{Owner: "cross-exchange:" + session.claim.ConfigurationHash,
			Exchange: exchange, BaseAsset: base, OwnedBase: balances[base].Available,
			TotalEligibleBase: totalBase, OwnedUSDT: balances[domain.AssetSymbol("USDT")].Available,
			TotalEligibleUSDT: totalUSDT, Revision: balances[base].Revision})
		feeBalances[exchange+":USDT"] = balances[domain.AssetSymbol("USDT")].Available
	}
	return inventory, feeBalances, nil
}

func ownerConsoleCrossExchangeMaximumSpread(market SandboxCrossExchangeMarketInput) (domain.Percent, error) {
	maximum, _ := domain.ParsePercent("0")
	for _, member := range market.Markets {
		if len(member.Snapshot.Bids) == 0 || len(member.Snapshot.Asks) == 0 {
			return domain.Percent{}, fmt.Errorf("shadow_cross_exchange_spread_invalid")
		}
		spread, err := domain.CalculateRelativeSpread(member.Snapshot.Bids[0].Price,
			member.Snapshot.Asks[0].Price, 18)
		if err != nil {
			return domain.Percent{}, fmt.Errorf("shadow_cross_exchange_spread_invalid")
		}
		if spread.Compare(maximum) > 0 {
			maximum = spread
		}
	}
	return maximum, nil
}

func (session *ownerConsoleCrossExchangeShadowSession) riskInput(market SandboxCrossExchangeMarketInput,
	inventory []crossarb.VenueInventory, budget domain.Balance, spread domain.Percent,
) (crossarb.RiskInput, error) {
	if len(inventory) != 2 {
		return crossarb.RiskInput{}, fmt.Errorf("shadow_cross_exchange_risk_invalid")
	}
	zero := ownerConsolePercent("0")
	totalMoney, _ := domain.ParseMoney(inventory[0].TotalEligibleUSDT.String())
	budgetMoney, budgetErr := domain.ParseMoney(budget.String())
	exposure, exposureErr := domain.CalculateConservativePercent(budgetMoney, totalMoney, 18)
	reserve, _ := domain.ParsePercent("0.15")
	openOrders, quality, problem := uint32(0), uint8(100), false
	maximumAge, maximumClock := time.Duration(0), time.Duration(0)
	for _, member := range market.Markets {
		age := ownerConsoleBookAge(market.Trigger.MonotonicNanos, member.Observation.PublishedOffsetNanos)
		if age > maximumAge {
			maximumAge = age
		}
	}
	for _, collector := range session.collectors {
		health := collector.HealthSnapshot()
		drift := health.ClockOffset
		if drift < 0 {
			drift = -drift
		}
		drift += health.ClockUncertainty
		if drift > maximumClock {
			maximumClock = drift
		}
	}
	if budgetErr != nil || exposureErr != nil {
		return crossarb.RiskInput{}, fmt.Errorf("shadow_cross_exchange_risk_invalid")
	}
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	queueLag := time.Duration(0)
	return crossarb.RiskInput{Policies: []risk.Policy{policy}, EvaluatedAt: market.Trigger.UTC,
		Cautious: risk.CautiousControls{ReducedSize: true, StricterEdge: true, InstrumentEligible: true},
		Observations: risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero,
			Rolling24HourLoss: &zero, StrategyLoss: &zero, AssetExposure: &exposure,
			CombinedExposure: &exposure, ExchangeExposure: &exposure, Reserve: &reserve,
			ReservedCapital: &exposure, Spread: &spread, Slippage: &zero, OpenOrders: &openOrders,
			BookAge: &maximumAge, QueueLag: &queueLag, ClockDrift: &maximumClock, QualityScore: &quality,
			Health: risk.HealthInputs{Gap: &problem, StaleData: &problem, ReconciliationFault: &problem,
				AccountingFault: &problem, UnknownOrder: &problem, PersistenceFault: &problem,
				DiskFault: &problem, APIError: &problem, LeaseLost: &problem}}}, nil
}

func ownerConsoleCrossExchangeFixedSimulation(market SandboxCrossExchangeMarketInput) *crossarb.SimulationInput {
	offset := market.Trigger.MonotonicNanos + 1
	result := &crossarb.SimulationInput{Latency: crossarb.LatencyDistribution{Version: "fixed-one-nanosecond-v1",
		BuySamplesNanos: []uint64{1}, SellSamplesNanos: []uint64{1}, VerificationNanos: 1,
		RetryNanos: 1, RecoveryNanos: 1}, Recovery: crossarb.RecoveryPolicy{}}
	for _, member := range market.Markets {
		result.Markets = append(result.Markets, crossarb.TimedMarketInput{Offset: offset, Market: member})
		result.Directives = append(result.Directives, crossarb.TimedDirective{Exchange: string(member.Snapshot.Exchange),
			Phase: crossarb.PhaseArrival, Offset: offset,
			Directive: crossarb.LegDirective{State: execution.OrderFilled}})
	}
	return result
}

func (session *ownerConsoleCrossExchangeShadowSession) recordAndProcess(ctx context.Context, input *crossarb.Input,
	viewID string, instrument domain.Instrument,
) error {
	eventID := "cross-view-" + viewID[:24]
	session.stateMutex.Lock()
	before := ownerConsoleCrossExchangeAvailable(session.balances)
	session.stateMutex.Unlock()
	var projection *crossarb.RecordedProjection
	ordinal, err := session.decisions.RecordDecisionInputBuilt(marketrecorder.DecisionInput{Instrument: instrument,
		EventID: eventID, LogicalTime: input.LogicalTime, ReceivedAt: input.Now}, func(assigned uint64) ([]byte, error) {
		input.Ordinal = assigned
		prepared, projected, prepareErr := crossarb.AttachCleanRecordedReduction(*input,
			"shadow/cross-exchange/"+session.claim.ID, before)
		if prepareErr != nil {
			return nil, prepareErr
		}
		projection, *input = projected, prepared
		return json.Marshal(input)
	})
	if err != nil || ordinal != input.Ordinal {
		return fmt.Errorf("shadow_cross_exchange_decision_record_failed")
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("shadow_cross_exchange_decision_encode_failed")
	}
	result, err := session.processor.Process(ctx, replay.Event{Ordinal: input.Ordinal,
		LogicalTime: input.LogicalTime, Canonical: payload})
	if err != nil {
		return err
	}
	if (projection == nil) != (input.Reduction == nil) {
		return fmt.Errorf("shadow_cross_exchange_projection_mismatch")
	}
	updated, err := session.store.RecordCrossExchangeShadowDecision(ctx, session.claim, *input, result)
	if err != nil {
		return err
	}
	session.stateMutex.Lock()
	session.balances, session.lastViewID, session.lastOrdinal = updated, viewID, input.Ordinal
	session.stateMutex.Unlock()
	return session.recordActivity(ctx, session.currentActivity(time.Now().UTC()))
}

func ownerConsoleCrossExchangeAvailable(values map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot) crossarb.VenueBalances {
	result := make(crossarb.VenueBalances, len(values))
	for exchange, balances := range values {
		result[exchange] = make(map[domain.AssetSymbol]domain.Balance, len(balances))
		for asset, value := range balances {
			result[exchange][asset] = value.Available
		}
	}
	return result
}

func cloneOwnerConsoleCrossExchangeSnapshots(values map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot) map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot {
	result := make(map[string]map[domain.AssetSymbol]accounting.BalanceSnapshot, len(values))
	for exchange, balances := range values {
		result[exchange] = make(map[domain.AssetSymbol]accounting.BalanceSnapshot, len(balances))
		for asset, value := range balances {
			result[exchange][asset] = value
		}
	}
	return result
}
