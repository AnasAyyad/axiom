package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"axiom/internal/domain"
	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
	postgresstore "axiom/internal/storage/postgres"
	"axiom/internal/strategies/triangular"
)

var errPublicShadowTriangularMarketInputUnavailable = errors.New("shadow_triangular_market_input_unavailable")

func (session *ownerConsoleLiveShadowSession) buildTriangularInput(
	ctx context.Context,
	now time.Time,
) (triangular.Input, error) {
	market, err := session.captureTriangularMarket(ctx, now)
	if err != nil {
		return triangular.Input{}, err
	}
	available, budget, reserve, recovery, feeBalances, err := session.triangularCapital()
	if err != nil {
		return triangular.Input{}, err
	}
	riskInput, err := session.triangularRiskInput(market, available, reserve, recovery)
	if err != nil {
		return triangular.Input{}, err
	}
	input := triangular.Input{Ordinal: market.Trigger.IngestOrdinal,
		LogicalTime: market.Trigger.MonotonicNanos, Now: market.Trigger.UTC,
		Exchange: session.claim.ExchangeID, Markets: append([]triangular.MarketInput(nil), market.Markets...),
		FirstDetectedOffset: market.FirstDetectedOffset, AvailableSettlement: available,
		StrategyBudget: budget, GlobalReserveFloor: reserve, RecoveryAllowance: recovery,
		FeeBalances: feeBalances, Configuration: session.triangularConfig,
		ConfigurationHash:    session.claim.ConfigurationHash,
		InstrumentMetadataID: market.InstrumentMetadataID, CentralRisk: &riskInput,
		Simulation: ownerConsoleTriangularZeroLatencySimulation(market),
	}
	if _, err = input.EvaluationInput(); err != nil || input.ValidateEventBinding(input.Ordinal, input.LogicalTime) != nil {
		return triangular.Input{}, fmt.Errorf("shadow_triangular_input_invalid")
	}
	if _, _, err = input.RecordedSimulation(); err != nil {
		return triangular.Input{}, fmt.Errorf("shadow_triangular_simulation_input_invalid")
	}
	return input, nil
}

func (session *ownerConsoleLiveShadowSession) captureTriangularMarket(
	ctx context.Context,
	now time.Time,
) (SandboxTriangularMarketInput, error) {
	keys, err := ownerConsoleTriangularMarketKeys(session.claim)
	if err != nil {
		return SandboxTriangularMarketInput{}, err
	}
	source, err := newPublicShadowSagaMarketSource(session)
	if err != nil {
		return SandboxTriangularMarketInput{}, err
	}
	reader, err := NewSandboxSagaMarketInputReader(source)
	if err != nil {
		return SandboxTriangularMarketInput{}, err
	}
	capture, err := reader.captureTriangular(ctx, keys, now, session.triangularConfig.MaximumBookAge)
	if err != nil || !session.matchesConsumedTriangularTrigger(capture.coherent) {
		return SandboxTriangularMarketInput{}, errPublicShadowTriangularMarketInputUnavailable
	}
	markets := make([]triangular.MarketInput, 0, len(capture.members))
	for _, member := range capture.members {
		markets = append(markets, triangular.MarketInput{Snapshot: member.snapshot,
			Observation: member.view.Observation(), Rules: member.rules})
	}
	return SandboxTriangularMarketInput{Markets: markets, Trigger: capture.trigger,
		FirstDetectedOffset: capture.firstDetected, CoherentViewID: capture.coherent.Identity(),
		InstrumentMetadataID: sandboxSagaMarketEvidenceHash(capture.coherent.Identity(), capture.rules)}, nil
}

func (session *ownerConsoleLiveShadowSession) matchesConsumedTriangularTrigger(
	view runtimecore.CoherentView,
) bool {
	for _, member := range view.Members() {
		if member.Key.Instrument.Symbol() != "ETHBTC" {
			continue
		}
		session.stateMutex.Lock()
		matches := session.lastTriangularTriggerGeneration == member.ConnectionGeneration &&
			session.lastTriangularTriggerVersion == member.BookVersion
		session.stateMutex.Unlock()
		return matches
	}
	return false
}

func ownerConsoleTriangularMarketKeys(claim postgresstore.PublicShadowClaim) ([]runtimecore.MarketKey, error) {
	if claim.StrategyID != "triangular-arbitrage-1-0-0" || claim.ExchangeID == "" || len(claim.MarketScopes) != 3 {
		return nil, fmt.Errorf("shadow_triangular_market_scope_invalid")
	}
	keys := make([]runtimecore.MarketKey, 0, 3)
	seen := make(map[string]bool, 3)
	for index, scope := range claim.MarketScopes {
		instrument, err := sandboxSagaInstrument(scope.InstrumentID)
		if err != nil || scope.Ordinal != int16(index+1) || scope.ExchangeID != claim.ExchangeID ||
			scope.Purpose != "triangle_market" || seen[scope.InstrumentID] {
			return nil, fmt.Errorf("shadow_triangular_market_scope_invalid")
		}
		seen[scope.InstrumentID] = true
		keys = append(keys, runtimecore.MarketKey{Exchange: scope.ExchangeID, Instrument: instrument})
	}
	if !seen["BTCUSDT"] || !seen["ETHUSDT"] || !seen["ETHBTC"] {
		return nil, fmt.Errorf("shadow_triangular_market_scope_invalid")
	}
	sort.Slice(keys, func(left, right int) bool {
		return keys[left].Instrument.Symbol() < keys[right].Instrument.Symbol()
	})
	return keys, nil
}

func (session *ownerConsoleLiveShadowSession) triangularCapital() (
	domain.Balance,
	domain.Balance,
	domain.Balance,
	domain.Balance,
	map[domain.AssetSymbol]domain.Balance,
	error,
) {
	settlement, _ := domain.ParseAssetSymbol("USDT")
	session.stateMutex.Lock()
	snapshot := session.balances
	session.stateMutex.Unlock()
	balance, exists := snapshot.Balances[settlement]
	if !exists {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil,
			fmt.Errorf("shadow_triangular_settlement_unavailable")
	}
	available := balance.Available
	starting, startErr := domain.ParseBalance(session.claim.Configuration.Portfolio.StartingCapital.Value)
	reserveFraction, reserveFractionErr := domain.ParsePercent("0.15")
	reserve, reserveErr := domain.ScaleBalanceCeiling(starting, reserveFraction, 18)
	spendable, spendableErr := available.Subtract(reserve)
	maximum, maximumErr := domain.ParseBalance(session.triangularConfig.MaximumCycleNotional.String())
	if startErr != nil || reserveFractionErr != nil || reserveErr != nil || spendableErr != nil || maximumErr != nil {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil,
			fmt.Errorf("shadow_triangular_capital_invalid")
	}
	capacity := spendable
	if maximum.Compare(capacity) < 0 {
		capacity = maximum
	}
	recoveryFraction, err := triangularRecoveryReserveFraction(session.claim.Configuration.Triangular)
	if err != nil {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil, err
	}
	recovery, err := domain.ScaleBalanceCeiling(capacity, recoveryFraction, 18)
	zero, _ := domain.ParseBalance("0")
	if err != nil || recovery.Compare(zero) <= 0 || capacity.Compare(recovery) <= 0 {
		return domain.Balance{}, domain.Balance{}, domain.Balance{}, domain.Balance{}, nil,
			fmt.Errorf("shadow_triangular_capital_unavailable")
	}
	feeBalances := make(map[domain.AssetSymbol]domain.Balance, len(snapshot.Balances))
	for asset, owned := range snapshot.Balances {
		if owned.Available.Compare(zero) > 0 {
			feeBalances[asset] = owned.Available
		}
	}
	return available, available, reserve, recovery, feeBalances, nil
}

func (session *ownerConsoleLiveShadowSession) triangularRiskInput(
	market SandboxTriangularMarketInput,
	available, reserve, recovery domain.Balance,
) (triangular.RiskInput, error) {
	if len(market.Markets) != 3 || available.String() == "0" {
		return triangular.RiskInput{}, fmt.Errorf("shadow_triangular_risk_input_invalid")
	}
	ratios, err := session.triangularRiskRatios(available, reserve, recovery)
	if err != nil {
		return triangular.RiskInput{}, err
	}
	marketRisk, err := session.triangularMarketRisk(market)
	if err != nil {
		return triangular.RiskInput{}, err
	}
	zero, openOrders, quality := ownerConsolePercent("0"), uint32(0), uint8(100)
	queueLag := time.Duration(0)
	problem := false
	policy := risk.DefaultGlobalPolicy()
	policy.State = risk.StateNormal
	return triangular.RiskInput{Policies: []risk.Policy{policy}, EvaluatedAt: market.Trigger.UTC,
		Cautious: risk.CautiousControls{ReducedSize: true, StricterEdge: true, InstrumentEligible: true},
		Observations: risk.Observations{AccountDrawdown: &zero, UTCDayLoss: &zero,
			Rolling24HourLoss: &zero, StrategyLoss: &zero, AssetExposure: &ratios.exposure,
			CombinedExposure: &ratios.exposure, ExchangeExposure: &ratios.exposure,
			Reserve: &ratios.reserve, ReservedCapital: &ratios.reserved,
			Spread: &marketRisk.spread, Slippage: &zero, OpenOrders: &openOrders,
			BookAge: &marketRisk.bookAge, QueueLag: &queueLag, ClockDrift: &marketRisk.clockDrift,
			QualityScore: &quality, Health: risk.HealthInputs{Gap: &problem, StaleData: &problem,
				ReconciliationFault: &problem, AccountingFault: &problem, UnknownOrder: &problem,
				PersistenceFault: &problem, DiskFault: &problem, APIError: &problem,
				LeaseLost: &problem}}}, nil
}

type ownerConsoleTriangularRiskRatios struct {
	reserve, exposure, reserved domain.Percent
}

func (session *ownerConsoleLiveShadowSession) triangularRiskRatios(available, reserve,
	recovery domain.Balance,
) (ownerConsoleTriangularRiskRatios, error) {
	reserveMoney, reserveErr := domain.ParseMoney(reserve.String())
	availableMoney, availableErr := domain.ParseMoney(available.String())
	reserveRatio, ratioErr := domain.CalculateConservativePercent(reserveMoney, availableMoney, 18)
	maximumMoney, maximumErr := domain.ParseMoney(session.triangularConfig.MaximumCycleNotional.String())
	exposure, exposureErr := domain.CalculateConservativePercent(maximumMoney, availableMoney, 18)
	recoveryMoney, recoveryErr := domain.ParseMoney(recovery.String())
	reservedMoney, reservedErr := maximumMoney.Add(recoveryMoney)
	reserved, reservedRatioErr := domain.CalculateConservativePercent(reservedMoney, availableMoney, 18)
	if reserveErr != nil || availableErr != nil || ratioErr != nil || maximumErr != nil || exposureErr != nil ||
		recoveryErr != nil || reservedErr != nil || reservedRatioErr != nil {
		return ownerConsoleTriangularRiskRatios{}, fmt.Errorf("shadow_triangular_risk_input_invalid")
	}
	return ownerConsoleTriangularRiskRatios{reserve: reserveRatio, exposure: exposure, reserved: reserved}, nil
}

type ownerConsoleTriangularMarketRisk struct {
	spread              domain.Percent
	bookAge, clockDrift time.Duration
}

func (session *ownerConsoleLiveShadowSession) triangularMarketRisk(
	market SandboxTriangularMarketInput,
) (ownerConsoleTriangularMarketRisk, error) {
	result := ownerConsoleTriangularMarketRisk{spread: ownerConsolePercent("0")}
	logical := market.Trigger.MonotonicNanos
	for _, item := range market.Markets {
		if len(item.Snapshot.Bids) == 0 || len(item.Snapshot.Asks) == 0 {
			return ownerConsoleTriangularMarketRisk{}, fmt.Errorf("shadow_triangular_risk_input_invalid")
		}
		spread, err := domain.CalculateRelativeSpread(item.Snapshot.Bids[0].Price,
			item.Snapshot.Asks[0].Price, 18)
		if err != nil {
			return ownerConsoleTriangularMarketRisk{}, fmt.Errorf("shadow_triangular_risk_input_invalid")
		}
		if spread.Compare(result.spread) > 0 {
			result.spread = spread
		}
		age := ownerConsoleBookAge(logical, item.Observation.PublishedOffsetNanos)
		if age > result.bookAge {
			result.bookAge = age
		}
	}
	for _, collector := range session.collectors {
		health := collector.HealthSnapshot()
		drift := health.ClockOffset
		if drift < 0 {
			drift = -drift
		}
		drift += health.ClockUncertainty
		if drift > result.clockDrift {
			result.clockDrift = drift
		}
	}
	return result, nil
}

func ownerConsoleTriangularZeroLatencySimulation(
	market SandboxTriangularMarketInput,
) *triangular.SimulationInput {
	// The selected fixed-zero model still uses one-nanosecond monotonic steps so
	// sequential saga identities remain strictly ordered without claiming a
	// later public observation.
	latency := triangular.LatencyModel{Version: "fixed-zero-v1",
		LegNanos: [3]uint64{1, 1, 1}, RecoveryNanos: 1}
	result := &triangular.SimulationInput{Latency: latency, Markets: make([]triangular.TimedMarketInput, 0, 12)}
	for _, offset := range []uint64{market.Trigger.MonotonicNanos + 1, market.Trigger.MonotonicNanos + 2,
		market.Trigger.MonotonicNanos + 3, market.Trigger.MonotonicNanos + 4} {
		for _, item := range market.Markets {
			result.Markets = append(result.Markets, triangular.TimedMarketInput{Offset: offset, Market: item})
		}
	}
	return result
}
