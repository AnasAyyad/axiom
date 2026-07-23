package triangular

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/strategies/arbitrage"
)

// SimulationOutcome is one bounded B4 sequential-cycle result.
type SimulationOutcome string

// B4 records every required success, failure, recovery, and quarantine class.
const (
	OutcomeFullSuccess          SimulationOutcome = "full_success"
	OutcomePartialCycle         SimulationOutcome = "partial_cycle"
	OutcomeMissedLeg            SimulationOutcome = "missed_leg"
	OutcomeNegativeAfterLatency SimulationOutcome = "negative_after_latency"
	OutcomeStrandedAsset        SimulationOutcome = "stranded_asset"
)

// Timeline provides the deterministic future book/rules at one leg arrival.
type Timeline interface {
	MarketAt(exchange string, source, target domain.AssetSymbol, offset uint64) (Market, error)
}

// LatencyModel is the immutable sequential dispatch schedule.
type LatencyModel struct {
	Version       string
	LegNanos      [3]uint64
	RecoveryNanos uint64
}

// RecoveryResult records immediate conversion or explicit unresolved exposure.
type RecoveryResult struct {
	Attempted   bool
	Recovered   bool
	Quarantined bool
	Asset       domain.AssetSymbol
	Input       domain.Quantity
	OutputUSDT  domain.Quantity
	Loss        domain.PnL
	Leg         *arbitrage.Result
}

// SimulationResult is one restart-comparable sequential saga result.
type SimulationResult struct {
	CandidateID    string
	Outcome        SimulationOutcome
	ArrivalOffsets []uint64
	Legs           []arbitrage.Result
	FinalUSDT      domain.Quantity
	Recovery       RecoveryResult
	Saga           execution.Saga
	LatencyVersion string
	CanonicalHash  string
}

// Simulate executes actual rounded output through three deterministic future
// books, persists the shared saga, and attempts immediate USDT recovery.
func Simulate(candidate Candidate, timeline Timeline, latency LatencyModel) (SimulationResult, error) {
	if !validSimulationInput(candidate, timeline, latency) {
		return SimulationResult{}, strategyError("simulation_invalid")
	}
	saga, planID, err := newSequentialSaga(candidate)
	if err != nil {
		return SimulationResult{}, err
	}
	if err = saga.Activate(); err != nil {
		return SimulationResult{}, err
	}
	result := SimulationResult{
		CandidateID: candidate.ID, ArrivalOffsets: make([]uint64, 0, 3),
		Legs: make([]arbitrage.Result, 0, 3), LatencyVersion: latency.Version,
	}
	failure, err := simulateSequentialLegs(candidate, timeline, latency, saga, planID, &result)
	if err != nil {
		return SimulationResult{}, err
	}
	if failure != nil {
		return finishFailedSimulation(candidate, timeline, latency, saga, planID, result,
			failure.index, failure.current, failure.asset, failure.outcome, failure.arrival)
	}
	return finishSuccessfulSimulation(candidate, saga, result), nil
}

type simulationFailure struct {
	index   int
	current domain.Quantity
	asset   domain.AssetSymbol
	outcome SimulationOutcome
	arrival uint64
}

func simulateSequentialLegs(
	candidate Candidate,
	timeline Timeline,
	latency LatencyModel,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result *SimulationResult,
) (*simulationFailure, error) {
	current := candidate.Start
	arrival := candidate.DecisionOffsetNanos
	for index, original := range candidate.Legs {
		arrival += latency.LegNanos[index]
		result.ArrivalOffsets = append(result.ArrivalOffsets, arrival)
		if arrival > candidate.ExpiresOffsetNanos {
			return &simulationFailure{index, current, original.Source, OutcomeMissedLeg, arrival}, nil
		}
		leg, outcome, ok := conversionAtArrival(candidate, timeline, original, current, arrival, len(result.Legs))
		if !ok {
			return &simulationFailure{index, current, original.Source, outcome, arrival}, nil
		}
		current = leg.NetOutput
		result.Legs = append(result.Legs, leg)
		exposure := exposureFor(leg.Target, current)
		if err := saga.ApplyOrder(sagaOrder(planID, index, leg, execution.OrderFilled), exposure); err != nil {
			return nil, err
		}
	}
	result.FinalUSDT = current
	return nil, nil
}

func conversionAtArrival(
	candidate Candidate,
	timeline Timeline,
	original arbitrage.Result,
	current domain.Quantity,
	arrival uint64,
	filledLegs int,
) (arbitrage.Result, SimulationOutcome, bool) {
	market, err := timeline.MarketAt(candidate.Exchange, original.Source, original.Target, arrival)
	if err != nil || !market.Book.Eligible(arrival, candidateBookAge(candidate)) {
		if filledLegs == 0 {
			return arbitrage.Result{}, OutcomeMissedLeg, false
		}
		return arbitrage.Result{}, OutcomePartialCycle, false
	}
	leg, err := arbitrage.Convert(arbitrage.Request{
		Source: original.Source, Target: original.Target, Input: current,
		Book: market.Book, Rules: market.Rules,
	})
	return leg, OutcomePartialCycle, err == nil
}

func finishSuccessfulSimulation(
	candidate Candidate,
	saga *execution.SagaReducer,
	result SimulationResult,
) SimulationResult {
	result.Outcome = OutcomeFullSuccess
	if result.FinalUSDT.Compare(candidate.Start) <= 0 {
		result.Outcome = OutcomeNegativeAfterLatency
	}
	result.Saga = saga.Snapshot()
	result.CanonicalHash = simulationHash(result)
	return result
}

func validSimulationInput(candidate Candidate, timeline Timeline, latency LatencyModel) bool {
	return timeline != nil && candidate.ID != "" && len(candidate.Legs) == 3 &&
		latency.Version != "" && latency.LegNanos[0] > 0 && latency.LegNanos[1] > 0 &&
		latency.LegNanos[2] > 0 && latency.RecoveryNanos > 0
}

func finishFailedSimulation(
	candidate Candidate,
	timeline Timeline,
	latency LatencyModel,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result SimulationResult,
	failedIndex int,
	current domain.Quantity,
	currentAsset domain.AssetSymbol,
	outcome SimulationOutcome,
	arrival uint64,
) (SimulationResult, error) {
	if err := expireSimulationLegs(candidate, saga, planID, result, failedIndex, currentAsset, current); err != nil {
		return SimulationResult{}, err
	}
	result.Outcome = outcome
	if len(result.Legs) == 0 {
		result.FinalUSDT = candidate.Start
		result.Saga = saga.Snapshot()
		result.CanonicalHash = simulationHash(result)
		return result, nil
	}
	return recoverFailedSimulation(candidate, timeline, latency, saga, result, currentAsset, current, arrival)
}

func expireSimulationLegs(
	candidate Candidate,
	saga *execution.SagaReducer,
	planID domain.ExecutionPlanID,
	result SimulationResult,
	failedIndex int,
	currentAsset domain.AssetSymbol,
	current domain.Quantity,
) error {
	for index := failedIndex; index < len(candidate.Legs); index++ {
		exposure := []execution.Exposure(nil)
		if len(result.Legs) > 0 {
			exposure = exposureFor(currentAsset, current)
		}
		if err := saga.ApplyOrder(
			sagaOrder(planID, index, candidate.Legs[index], execution.OrderExpired), exposure,
		); err != nil {
			return err
		}
	}
	return nil
}

func recoverFailedSimulation(
	candidate Candidate,
	timeline Timeline,
	latency LatencyModel,
	saga *execution.SagaReducer,
	result SimulationResult,
	currentAsset domain.AssetSymbol,
	current domain.Quantity,
	arrival uint64,
) (SimulationResult, error) {
	recovery, err := recoverToUSDT(candidate, timeline, currentAsset, current, arrival+latency.RecoveryNanos)
	if err != nil {
		recovery = RecoveryResult{
			Attempted: true, Quarantined: true, Asset: currentAsset, Input: current,
		}
		result.Outcome = OutcomeStrandedAsset
	}
	result.Recovery = recovery
	disposition, loss := recoveryDisposition(recovery)
	if recovery.Recovered {
		result.FinalUSDT = recovery.OutputUSDT
	}
	if err = saga.AddRecovery(execution.RecoveryAttempt{
		Attempt: 1, Action: "convert_to_usdt", Disposition: disposition,
		LossAsset: currentAsset, Loss: loss,
	}); err != nil {
		return SimulationResult{}, err
	}
	if err = saga.ResolveRecovery(disposition, recovery.Quarantined); err != nil {
		return SimulationResult{}, err
	}
	result.Saga = saga.Snapshot()
	result.CanonicalHash = simulationHash(result)
	return result, nil
}

func recoveryDisposition(recovery RecoveryResult) (string, domain.Balance) {
	lossBalance, _ := domain.ParseBalance("0")
	disposition := "recovered_to_usdt"
	if recovery.Recovered {
		lossBalance = pnlMagnitude(recovery.Loss)
	} else {
		disposition = "unresolved_exposure_quarantined"
	}
	return disposition, lossBalance
}

func recoverToUSDT(
	candidate Candidate,
	timeline Timeline,
	asset domain.AssetSymbol,
	quantity domain.Quantity,
	arrival uint64,
) (RecoveryResult, error) {
	usdt, _ := domain.ParseAssetSymbol("USDT")
	market, err := timeline.MarketAt(candidate.Exchange, asset, usdt, arrival)
	if err != nil || !market.Book.Eligible(arrival, candidateBookAge(candidate)) {
		return RecoveryResult{}, strategyError("recovery_market_unavailable")
	}
	leg, err := arbitrage.Convert(arbitrage.Request{
		Source: asset, Target: usdt, Input: quantity, Book: market.Book, Rules: market.Rules,
	})
	if err != nil {
		return RecoveryResult{}, strategyError("recovery_conversion_failed")
	}
	loss, _ := arbitrage.QuantityDifference(candidate.Start, leg.NetOutput)
	return RecoveryResult{
		Attempted: true, Recovered: true, Asset: asset, Input: quantity,
		OutputUSDT: leg.NetOutput, Loss: loss, Leg: &leg,
	}, nil
}

func newSequentialSaga(candidate Candidate) (*execution.SagaReducer, domain.ExecutionPlanID, error) {
	planID, err := domain.NewExecutionPlanID("b4-" + candidate.ID[:24])
	if err != nil {
		var zero domain.ExecutionPlanID
		return nil, zero, err
	}
	reservation, _ := domain.NewReservationID("b4-claims-" + candidate.ID[:20])
	legs := make([]execution.SagaLeg, 3)
	for index := range legs {
		orderID, _ := domain.NewVirtualOrderID("b4-" + candidate.ID[:16] + "-leg-" + uintString(uint64(index+1)))
		var dependency *uint32
		if index > 0 {
			value := uint32(index - 1)
			dependency = &value
		}
		legs[index] = execution.SagaLeg{
			Index: uint32(index), OrderID: orderID, DependsOn: dependency, State: execution.OrderCreated,
		}
	}
	saga, err := execution.NewSaga(planID, execution.DispatchSequential, legs, []domain.ReservationID{reservation})
	return saga, planID, err
}

func sagaOrder(
	planID domain.ExecutionPlanID,
	index int,
	leg arbitrage.Result,
	state execution.OrderState,
) execution.Order {
	orderID, _ := domain.NewVirtualOrderID("b4-" + planID.Value()[3:19] + "-leg-" + uintString(uint64(index+1)))
	return execution.Order{
		Identity: execution.OrderIdentity{
			ID: orderID, PlanID: planID, ClientOrderID: "b4-sim-" + uintString(uint64(index+1)),
			Instrument: leg.Instrument, Side: leg.Side, Quantity: leg.TradeQuantity,
		},
		State: state, CumulativeQuantity: filledQuantity(state, leg.TradeQuantity), Revision: 1,
	}
}

func filledQuantity(state execution.OrderState, quantity domain.Quantity) domain.Quantity {
	if state == execution.OrderFilled {
		return quantity
	}
	zero, _ := domain.ParseQuantity("0")
	return zero
}

func exposureFor(asset domain.AssetSymbol, quantity domain.Quantity) []execution.Exposure {
	if asset == "USDT" {
		return nil
	}
	balance, _ := domain.ParseBalance(quantity.String())
	return []execution.Exposure{{Asset: asset, Quantity: balance}}
}

func candidateBookAge(candidate Candidate) time.Duration {
	return time.Duration(candidate.MaximumBookAgeNanos)
}

func pnlMagnitude(value domain.PnL) domain.Balance {
	text := value.String()
	if len(text) > 0 && text[0] == '-' {
		text = text[1:]
	}
	result, err := domain.ParseBalance(text)
	if err != nil {
		result, _ = domain.ParseBalance("0")
	}
	return result
}

func simulationHash(result SimulationResult) string {
	copy := result
	copy.CanonicalHash = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
