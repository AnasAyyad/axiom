package crossarb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/reconciliation"
)

// RecordedProjection is the independently reproducible terminal state for one
// accepted virtual pair.
type RecordedProjection struct {
	Candidate     Candidate
	Plan          execution.Saga
	Simulation    SimulationResult
	VenueBalances VenueBalances
}

// AttachCleanRecordedReduction independently evaluates, ranks, simulates, and
// projects a canonical input. Ordinary no-op evaluations remain unreduced.
func AttachCleanRecordedReduction(input Input, scope string, before VenueBalances) (Input, *RecordedProjection, error) {
	if scope == "" || input.Reduction != nil {
		return Input{}, nil, strategyError("recorded_reduction_invalid")
	}
	projection, err := recordedProjection(input, before)
	if err != nil {
		var rejected *Error
		if errors.As(err, &rejected) && rejected.Code == "no_eligible_direction" {
			return input, nil, nil
		}
		return Input{}, nil, err
	}
	attribution, err := cleanPortfolioAttribution(projection.Candidate, projection.Simulation)
	if err != nil {
		return Input{}, nil, err
	}
	state, err := recordedProjectionState(input, before, projection, attribution)
	if err != nil {
		return Input{}, nil, err
	}
	input.Reduction = &ReductionInput{Attribution: attribution, Reconciliation: ReconciliationInput{
		Scope: scope, Expected: state, Actual: state, At: input.Now,
	}}
	return input, projection, nil
}

// ValidateCleanRecordedReduction recomputes every simulator-authoritative
// result and rejects tampered balances, attribution, or reconciliation.
func ValidateCleanRecordedReduction(input Input, before VenueBalances) (*RecordedProjection, error) {
	reduction, err := input.RecordedReduction()
	if err != nil || len(reduction.Reconciliation.Expected.Duplicates) != 0 ||
		len(reduction.Reconciliation.Expected.Differences) != 0 ||
		len(reduction.Reconciliation.Actual.Duplicates) != 0 ||
		len(reduction.Reconciliation.Actual.Differences) != 0 {
		return nil, strategyError("recorded_reduction_invalid")
	}
	projection, err := recordedProjection(input, before)
	if err != nil {
		return nil, err
	}
	attribution, err := cleanPortfolioAttribution(projection.Candidate, projection.Simulation)
	if err != nil || !reflect.DeepEqual(attribution, reduction.Attribution) {
		return nil, strategyError("recorded_reduction_invalid")
	}
	state, err := recordedProjectionState(input, before, projection, attribution)
	if err != nil || !reflect.DeepEqual(reduction.Reconciliation.Expected, state) ||
		!reflect.DeepEqual(reduction.Reconciliation.Actual, state) ||
		!reduction.Reconciliation.At.Equal(input.Now) {
		return nil, strategyError("recorded_reduction_invalid")
	}
	return projection, nil
}

func recordedProjection(input Input, before VenueBalances) (*RecordedProjection, error) {
	evaluation, err := input.EvaluationInput()
	if err != nil {
		return nil, err
	}
	candidates, err := Evaluate(evaluation)
	if err != nil {
		return nil, err
	}
	candidate, err := PreferredCandidate(candidates)
	if err != nil {
		return nil, err
	}
	timeline, latency, policy, err := input.RecordedSimulation()
	if err != nil {
		return nil, err
	}
	result, err := Simulate(candidate, timeline, latency, policy)
	if err != nil {
		return nil, err
	}
	planned, _, err := newConcurrentSaga(candidate)
	if err != nil {
		return nil, err
	}
	balances, err := ProjectVenueBalances(before, candidate, result)
	if err != nil {
		return nil, err
	}
	return &RecordedProjection{Candidate: candidate, Plan: planned.Snapshot(), Simulation: result,
		VenueBalances: balances}, nil
}

func cleanPortfolioAttribution(candidate Candidate, result SimulationResult) (PortfolioAttribution, error) {
	if result.Outcome != OutcomeBothFilled || result.ActualBuy == nil || result.ActualSell == nil {
		return PortfolioAttribution{}, strategyError("recorded_reduction_invalid")
	}
	executionValue, err := attributionFromPnL(result.ActualUSDTNet)
	combinedValue, combinedErr := attributionFromPnL(candidate.Economics.ExpectedClosedCycleProfit)
	fees, feeErr := addMoneyBalances(result.ActualBuy.FeeQuoteEquivalent, result.ActualSell.FeeQuoteEquivalent)
	spread, spreadErr := addMoneyBalances(result.ActualBuy.SpreadCost, result.ActualSell.SpreadCost)
	rebalancing, rebalanceErr := addMoneyBalances(candidate.Economics.MarginalInventoryReplacement,
		candidate.Economics.NaturalReversalCost, candidate.Economics.AdvisoryRebalancingCost,
		candidate.Economics.ExchangeConcentrationPenalty, candidate.Economics.USDTVenueConcentrationPenalty)
	zero, _ := domain.ParseBalance("0")
	if err != nil || combinedErr != nil || feeErr != nil || spreadErr != nil || rebalanceErr != nil {
		return PortfolioAttribution{}, strategyError("recorded_reduction_invalid")
	}
	projection := PortfolioAttribution{ExecutionPnL: executionValue,
		BTCInventoryPnL: AttributionValue{Amount: zero}, ETHInventoryPnL: AttributionValue{Amount: zero},
		StablecoinValuation: AttributionValue{Amount: zero}, Fees: fees, Spread: spread, Slippage: zero,
		Latency:  domainBalance(candidate.Economics.LatencyDeterioration),
		Recovery: domainBalance(result.Recovery.Loss), Rebalancing: rebalancing,
		CombinedPnL: combinedValue}
	for _, category := range []struct {
		name  string
		value domain.Balance
	}{{"execution_pnl", projection.ExecutionPnL.Amount},
		{"btc_inventory_market_pnl", projection.BTCInventoryPnL.Amount},
		{"eth_inventory_market_pnl", projection.ETHInventoryPnL.Amount},
		{"stablecoin_valuation", projection.StablecoinValuation.Amount}, {"fees", projection.Fees},
		{"spread", projection.Spread}, {"slippage", projection.Slippage}, {"latency", projection.Latency},
		{"recovery", projection.Recovery}, {"inventory_restoration", projection.Rebalancing},
		{"combined_pnl", projection.CombinedPnL.Amount}} {
		if category.value.Compare(zero) == 0 {
			projection.ZeroCategories = append(projection.ZeroCategories, category.name)
		}
	}
	return projection, nil
}

func attributionFromPnL(value domain.PnL) (AttributionValue, error) {
	text := value.String()
	gain := !strings.HasPrefix(text, "-")
	if !gain {
		text = strings.TrimPrefix(text, "-")
	}
	amount, err := domain.ParseBalance(text)
	return AttributionValue{Amount: amount, Gain: gain}, err
}

func addMoneyBalances(values ...domain.Money) (domain.Balance, error) {
	result, _ := domain.ParseBalance("0")
	for _, value := range values {
		amount, err := domain.ParseBalance(value.String())
		if err != nil {
			return domain.Balance{}, err
		}
		result, err = result.Add(amount)
		if err != nil {
			return domain.Balance{}, err
		}
	}
	return result, nil
}

type crossRecordedFillFacts struct {
	Legs     []LegSimulation `json:"legs"`
	Recovery RecoveryResult  `json:"recovery"`
}

func recordedProjectionState(input Input, before VenueBalances, projection *RecordedProjection,
	attribution PortfolioAttribution,
) (reconciliation.State, error) {
	if projection == nil || projection.Candidate.ID == "" ||
		projection.Simulation.CandidateID != projection.Candidate.ID {
		return reconciliation.State{}, strategyError("recorded_reduction_invalid")
	}
	values := []any{
		projection.Simulation.Saga,
		crossRecordedFillFacts{Legs: projection.Simulation.Legs, Recovery: projection.Simulation.Recovery},
		projection.Candidate.Claims,
		canonicalVenueBalances(projection.VenueBalances),
		struct {
			Outcome SimulationOutcome   `json:"outcome"`
			State   execution.PlanState `json:"state"`
		}{projection.Simulation.Outcome, projection.Simulation.Saga.State},
		canonicalVenueBalances(before),
		struct {
			CandidateID string               `json:"candidate_id"`
			Attribution PortfolioAttribution `json:"attribution"`
		}{projection.Candidate.ID, attribution},
		struct {
			CandidateHash  string             `json:"candidate_hash"`
			SimulationHash string             `json:"simulation_hash"`
			Balances       []venueBalanceFact `json:"balances"`
		}{projection.Candidate.ID, projection.Simulation.CanonicalHash,
			canonicalVenueBalances(projection.VenueBalances)},
	}
	hashes := make([]string, len(values))
	for index, value := range values {
		payload, err := json.Marshal(value)
		if err != nil || len(payload) == 0 {
			return reconciliation.State{}, strategyError("recorded_reduction_invalid")
		}
		digest := sha256.Sum256(payload)
		hashes[index] = hex.EncodeToString(digest[:])
	}
	return reconciliation.State{Orders: hashes[0], Fills: hashes[1], Reservations: hashes[2],
		Balances: hashes[3], Positions: hashes[4], Ownership: hashes[5], Journal: hashes[6],
		Projections: hashes[7]}, nil
}
