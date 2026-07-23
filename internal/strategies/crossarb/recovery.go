package crossarb

import (
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

func recoverSimulation(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result *SimulationResult,
) error {
	if result.Outcome == OutcomeDelayedUnknown {
		result.Recovery.VerificationCompleted = true
		result.Recovery.RetryAttempted = anyRetry(result.Legs)
		result.Recovery.Quarantined = true
		result.Recovery.Disposition = "unknown_after_verification_quarantined"
		return quarantineSaga(candidate, saga, planID, result)
	}
	result.Recovery.VerificationCompleted = allUnknownVerified(result.Legs)
	result.Recovery.RetryAttempted = anyRetry(result.Legs)
	result.Recovery.RetrySucceeded = retrySucceeded(result.Legs)
	result.Recovery.UnwindAttempted = true
	recovered := protectedUnwind(candidate, timeline, latency, result)
	result.Recovery.UnwindSucceeded = recovered
	if recovered {
		result.Recovery.Disposition = "protected_inventory_unwind"
		return resolveSaga(saga, result.Recovery.Disposition, false)
	}
	result.Recovery.Quarantined = true
	result.Recovery.Disposition = "unresolved_exposure_quarantined"
	return quarantineSaga(candidate, saga, planID, result)
}

func protectedUnwind(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	result *SimulationResult,
) bool {
	offset := maximumArrival(result.Legs) + latency.RecoveryNanos
	usdt, _ := domain.ParseAssetSymbol("USDT")
	for _, exposure := range result.Exposures {
		market, err := timeline.MarketAt(exposure.Exchange, candidate.Instrument, offset)
		if err != nil {
			return false
		}
		var request arbitrage.Request
		if exposure.Kind == "base_acquired" {
			request = arbitrage.Request{
				Source: candidate.Instrument.Base, Target: usdt,
				Input: quantity(exposure.Quantity.String()), Book: market.Book, Rules: market.Rules,
			}
		} else {
			request = arbitrage.Request{
				Source: usdt, Target: candidate.Instrument.Base,
				Input: candidate.Buy.Input, Book: market.Book, Rules: market.Rules,
			}
		}
		conversion, conversionErr := arbitrage.Convert(request)
		if conversionErr != nil {
			return false
		}
		if exposure.Kind == "base_depleted" &&
			conversion.NetOutput.Compare(quantity(exposure.Quantity.String())) < 0 {
			return false
		}
	}
	result.Exposures = nil
	return true
}

func quarantineSaga(
	candidate Candidate,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result *SimulationResult,
) error {
	if saga.Snapshot().State != execution.PlanRecoveryRequired {
		uncertain := quantity(candidate.Buy.TradeQuantity.String())
		order := simulationOrder(planID, candidate, 0, execution.OrderRecoveryRequired, uncertain)
		exposure := []execution.Exposure{{
			Asset: candidate.Instrument.Base, Quantity: balance(uncertain.String()),
		}}
		if err := saga.ApplyOrder(order, exposure); err != nil {
			return err
		}
	}
	return resolveSaga(saga, result.Recovery.Disposition, true)
}

func resolveSaga(saga *execution.SagaReducer, disposition string, quarantined bool) error {
	if saga.Snapshot().State != execution.PlanRecoveryRequired {
		return strategyError("recovery_state_invalid")
	}
	zero := balance("0")
	if err := saga.AddRecovery(execution.RecoveryAttempt{
		Attempt: 1, Action: "protected_inventory_restoration", Disposition: disposition,
		LossAsset: "USDT", Loss: zero,
	}); err != nil {
		return err
	}
	return saga.ResolveRecovery(disposition, quarantined)
}

func maximumArrival(legs []LegSimulation) uint64 {
	maximum := uint64(0)
	for _, leg := range legs {
		if leg.ArrivalOffsetNanos > maximum {
			maximum = leg.ArrivalOffsetNanos
		}
	}
	return maximum
}

func anyRetry(legs []LegSimulation) bool {
	for _, leg := range legs {
		if leg.RetryCount > 0 {
			return true
		}
	}
	return false
}

func retrySucceeded(legs []LegSimulation) bool {
	for _, leg := range legs {
		if leg.RetryCount > 0 && leg.FinalState != execution.OrderUnknown {
			return true
		}
	}
	return false
}

func allUnknownVerified(legs []LegSimulation) bool {
	for _, leg := range legs {
		if leg.InitialState == execution.OrderUnknown && leg.VerificationCount == 0 {
			return false
		}
	}
	return true
}
