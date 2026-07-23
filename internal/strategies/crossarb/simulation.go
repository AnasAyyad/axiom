package crossarb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

// Simulate dispatches the two virtual legs on a stable concurrent schedule,
// verifies unknown states, and then performs only risk-authorized retry or a
// protected simulated unwind.
func Simulate(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	policy RecoveryPolicy,
) (SimulationResult, error) {
	events, err := validateSimulation(candidate, timeline, latency, policy)
	if err != nil {
		return SimulationResult{}, err
	}
	saga, planID, err := newConcurrentSaga(candidate)
	if err != nil {
		return SimulationResult{}, err
	}
	if err = saga.Activate(); err != nil {
		return SimulationResult{}, err
	}
	result := SimulationResult{
		CandidateID: candidate.ID, Legs: make([]LegSimulation, 2),
		LatencyVersion: latency.Version,
	}
	arrivalResults, profitable := arrivalLegs(candidate, timeline, events)
	if !profitable {
		return finishNegativeArrival(candidate, saga, planID, events, result)
	}
	for _, event := range events {
		leg, legErr := simulateArrival(
			candidate, timeline, latency, policy, event, arrivalResults[event.index],
		)
		if legErr != nil {
			return SimulationResult{}, legErr
		}
		result.Legs[event.index] = leg
	}
	result.Outcome = classifyOutcome(result.Legs)
	result.ActualBuy = result.Legs[0].Result
	result.ActualSell = result.Legs[1].Result
	result.Exposures = venueExposures(candidate, result.Legs)
	result.ActualUSDTNet = actualUSDTNet(result.Legs)
	if err = applyConcurrentSaga(saga, planID, candidate, events, &result); err != nil {
		return SimulationResult{}, err
	}
	if err = recoverIfNeeded(candidate, timeline, latency, saga, planID, &result); err != nil {
		return SimulationResult{}, err
	}
	result.Saga = saga.Snapshot()
	result.CanonicalHash = simulationHash(result)
	return result, nil
}

func recoverIfNeeded(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result *SimulationResult,
) error {
	if !requiresRecovery(*result) {
		return nil
	}
	return recoverSimulation(candidate, timeline, latency, saga, planID, result)
}

func validateSimulation(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	policy RecoveryPolicy,
) ([]scheduledLeg, error) {
	if timeline == nil || candidate.ID == "" || candidate.BuyExchange == candidate.SellExchange ||
		policy.MaximumRetries > candidateRecoveryMaximum(candidate) ||
		(policy.RiskAllowsRetry && policy.MaximumRetries != 1) ||
		(!policy.RiskAllowsRetry && policy.MaximumRetries != 0) {
		return nil, strategyError("simulation_invalid")
	}
	return schedule(candidate, latency)
}

func arrivalLegs(
	candidate Candidate,
	timeline Timeline,
	events []scheduledLeg,
) ([2]arbitrage.Result, bool) {
	var results [2]arbitrage.Result
	buyMarket, err := timeline.MarketAt(candidate.BuyExchange, candidate.Instrument, eventsForIndex(events, 0).offset)
	if err != nil {
		return results, false
	}
	buy, err := arbitrage.Convert(arbitrage.Request{
		Source: candidate.Buy.Source, Target: candidate.Buy.Target, Input: candidate.Buy.Input,
		Book: buyMarket.Book, Rules: buyMarket.Rules,
	})
	if err != nil {
		return results, false
	}
	sellMarket, err := timeline.MarketAt(candidate.SellExchange, candidate.Instrument, eventsForIndex(events, 1).offset)
	if err != nil {
		return results, false
	}
	sell, err := arbitrage.Convert(arbitrage.Request{
		Source: candidate.Sell.Source, Target: candidate.Sell.Target, Input: buy.NetOutput,
		Book: sellMarket.Book, Rules: sellMarket.Rules,
	})
	if err != nil {
		return results, false
	}
	economics, err := closedCycleEconomics(buy, sell, restorationFromCandidate(candidate))
	zero, _ := domain.ParsePnL("0")
	if err != nil || economics.ExpectedClosedCycleProfit.Compare(zero) <= 0 ||
		economics.WorstClosedCycleProfit.Compare(zero) <= 0 {
		return results, false
	}
	results[0], results[1] = buy, sell
	return results, true
}

func simulateArrival(
	candidate Candidate,
	timeline Timeline,
	latency LatencyDistribution,
	policy RecoveryPolicy,
	event scheduledLeg,
	full arbitrage.Result,
) (LegSimulation, error) {
	directive, err := timeline.DirectiveAt(event.exchange, PhaseArrival, event.offset)
	if err != nil {
		return LegSimulation{}, strategyError("arrival_status_unavailable")
	}
	leg := LegSimulation{
		Index: event.index, Exchange: event.exchange, ArrivalOffsetNanos: event.offset,
		InitialState: directive.State, VerifiedState: directive.State, FinalState: directive.State,
		Input: directive.Input,
	}
	if directive.State == execution.OrderUnknown {
		leg, err = resolveUnknown(timeline, latency, policy, event, leg)
		if err != nil || leg.FinalState == execution.OrderUnknown {
			return leg, err
		}
		directive.State, directive.Input = leg.FinalState, leg.Input
	}
	result, err := resultForDirective(candidate, timeline, event, directive, full)
	if err != nil {
		return LegSimulation{}, err
	}
	leg.Result = result
	return leg, nil
}

func resolveUnknown(
	timeline Timeline,
	latency LatencyDistribution,
	policy RecoveryPolicy,
	event scheduledLeg,
	leg LegSimulation,
) (LegSimulation, error) {
	verifyOffset := event.offset + latency.VerificationNanos
	verified, err := timeline.DirectiveAt(event.exchange, PhaseVerification, verifyOffset)
	if err != nil {
		return LegSimulation{}, strategyError("unknown_verification_unavailable")
	}
	leg.VerificationCount, leg.VerifiedState = 1, verified.State
	leg.FinalState, leg.Input = verified.State, verified.Input
	if verified.State != execution.OrderUnknown || !policy.RiskAllowsRetry {
		return leg, nil
	}
	retryOffset := verifyOffset + latency.RetryNanos
	retried, err := timeline.DirectiveAt(event.exchange, PhaseRetry, retryOffset)
	if err != nil {
		return LegSimulation{}, strategyError("retry_status_unavailable")
	}
	leg.RetryCount, leg.FinalState, leg.Input = 1, retried.State, retried.Input
	return leg, nil
}

func resultForDirective(
	candidate Candidate,
	timeline Timeline,
	event scheduledLeg,
	directive LegDirective,
	full arbitrage.Result,
) (*arbitrage.Result, error) {
	switch directive.State {
	case execution.OrderFilled:
		copy := full
		return &copy, nil
	case execution.OrderPartiallyFilled:
		expected := full.Input
		zero := quantity("0")
		if directive.Input.Compare(zero) <= 0 || directive.Input.Compare(expected) >= 0 {
			return nil, strategyError("partial_fill_invalid")
		}
		market, err := timeline.MarketAt(event.exchange, candidate.Instrument, event.offset)
		if err != nil {
			return nil, strategyError("partial_book_unavailable")
		}
		result, err := arbitrage.Convert(arbitrage.Request{
			Source: full.Source, Target: full.Target, Input: directive.Input,
			Book: market.Book, Rules: market.Rules,
		})
		if err != nil {
			return nil, strategyError("partial_conversion_rejected")
		}
		return &result, nil
	case execution.OrderCanceled, execution.OrderRejected, execution.OrderExpired, execution.OrderUnknown:
		return nil, nil
	default:
		return nil, strategyError("arrival_state_invalid")
	}
}

func finishNegativeArrival(
	candidate Candidate,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	events []scheduledLeg,
	result SimulationResult,
) (SimulationResult, error) {
	result.Outcome = OutcomeNegativeBeforeArrival
	for _, event := range events {
		result.Legs[event.index] = LegSimulation{
			Index: event.index, Exchange: event.exchange, ArrivalOffsetNanos: event.offset,
			InitialState: execution.OrderExpired, VerifiedState: execution.OrderExpired,
			FinalState: execution.OrderExpired,
		}
		order := simulationOrder(planID, candidate, event.index, execution.OrderExpired, quantity("0"))
		if err := saga.ApplyOrder(order, nil); err != nil {
			return SimulationResult{}, err
		}
	}
	result.Saga = saga.Snapshot()
	result.ActualUSDTNet, _ = domain.ParsePnL("0")
	result.CanonicalHash = simulationHash(result)
	return result, nil
}

func eventsForIndex(events []scheduledLeg, index int) scheduledLeg {
	for _, event := range events {
		if event.index == index {
			return event
		}
	}
	panic("validated schedule missing leg")
}

func restorationFromCandidate(candidate Candidate) RestorationEconomics {
	return RestorationEconomics{
		ModelVersion:                   "closed-inventory-cycle.v1",
		LatencyModelVersion:            candidate.ModelVersion,
		RecoveryModelVersion:           candidate.ModelVersion,
		InventoryShadowPriceVersion:    candidate.ModelVersion,
		ConcentrationModelVersion:      candidate.ModelVersion,
		LatencyDeterioration:           candidate.Economics.LatencyDeterioration,
		RecoveryAllowance:              candidate.Economics.RecoveryAllowance,
		MarginalInventoryReplacement:   candidate.Economics.MarginalInventoryReplacement,
		NaturalReversalCost:            candidate.Economics.NaturalReversalCost,
		AdvisoryRebalancingCost:        candidate.Economics.AdvisoryRebalancingCost,
		ExchangeConcentrationPenalty:   candidate.Economics.ExchangeConcentrationPenalty,
		USDTVenueConcentrationPenalty:  candidate.Economics.USDTVenueConcentrationPenalty,
		MaximumOneLegLoss:              candidate.Economics.MaximumOneLegLoss,
		EstimatedRestorationDelayNanos: candidate.Economics.RestorationDelayNanos,
	}
}

func candidateRecoveryMaximum(candidate Candidate) uint32 {
	if candidate.ConfigurationVersion == "cross-exchange.v1b.1" {
		return 1
	}
	return 0
}

func simulationHash(result SimulationResult) string {
	copy := result
	copy.CanonicalHash = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// RestoreSimulation rejects any checkpoint whose immutable outcome, saga,
// fills, exposure, or recovery evidence no longer matches its canonical hash.
func RestoreSimulation(result SimulationResult) (SimulationResult, error) {
	if result.CandidateID == "" || len(result.Legs) != 2 || result.LatencyVersion == "" ||
		result.CanonicalHash == "" || simulationHash(result) != result.CanonicalHash ||
		result.Saga.ID.Value() == "" || result.Saga.Revision == 0 {
		return SimulationResult{}, strategyError("simulation_restore_rejected")
	}
	return result, nil
}
