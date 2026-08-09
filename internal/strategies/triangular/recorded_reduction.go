package triangular

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/reconciliation"
	"axiom/internal/strategies/arbitrage"
)

// RecordedProjection is the independently reproducible terminal state for one
// accepted virtual cycle. It is prepared before the canonical input is
// recorded and recomputed before durable shadow evidence is committed.
type RecordedProjection struct {
	Candidate         Candidate
	Plan              execution.Saga
	Simulation        SimulationResult
	AvailableBalances map[domain.AssetSymbol]domain.Balance
}

// AttachCleanRecordedReduction evaluates, ranks, simulates, and projects one
// canonical input. An ordinary no-op returns a nil projection and deliberately
// leaves Reduction absent because no allocation or execution was performed.
func AttachCleanRecordedReduction(input Input, scope string) (Input, *RecordedProjection, error) {
	if scope == "" || input.Reduction != nil {
		return Input{}, nil, strategyError("recorded_reduction_invalid")
	}
	projection, err := recordedProjection(input)
	if err != nil {
		var rejected *Error
		if errors.As(err, &rejected) && rejected.Code == "no_eligible_cycle" {
			return input, nil, nil
		}
		return Input{}, nil, err
	}
	state, err := recordedProjectionState(input, projection)
	if err != nil {
		return Input{}, nil, err
	}
	input.Reduction = &ReductionInput{Reconciliation: ReconciliationInput{
		Scope: scope, Expected: state, Actual: state, At: input.Now,
	}}
	return input, projection, nil
}

// ValidateCleanRecordedReduction recomputes every simulator-authoritative hash
// from the immutable input. It rejects a missing, mismatched, or discrepancy-
// bearing reduction before a live-shadow transaction can publish it.
func ValidateCleanRecordedReduction(input Input) (*RecordedProjection, error) {
	reduction, err := input.RecordedReduction()
	if err != nil || len(reduction.Reconciliation.Expected.Duplicates) != 0 ||
		len(reduction.Reconciliation.Expected.Differences) != 0 ||
		len(reduction.Reconciliation.Actual.Duplicates) != 0 ||
		len(reduction.Reconciliation.Actual.Differences) != 0 {
		return nil, strategyError("recorded_reduction_invalid")
	}
	projection, err := recordedProjection(input)
	if err != nil {
		return nil, err
	}
	state, err := recordedProjectionState(input, projection)
	if err != nil || !reflect.DeepEqual(reduction.Reconciliation.Expected, state) ||
		!reflect.DeepEqual(reduction.Reconciliation.Actual, state) ||
		!reduction.Reconciliation.At.Equal(input.Now) {
		return nil, strategyError("recorded_reduction_invalid")
	}
	return projection, nil
}

func recordedProjection(input Input) (*RecordedProjection, error) {
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
	timeline, latency, err := input.RecordedSimulation()
	if err != nil {
		return nil, err
	}
	result, err := Simulate(candidate, timeline, latency)
	if err != nil {
		return nil, err
	}
	planned, _, err := newSequentialSaga(candidate)
	if err != nil {
		return nil, err
	}
	before := recordedAvailableBalances(input)
	balances, err := ProjectAvailableBalances(before, candidate, result)
	if err != nil {
		return nil, err
	}
	return &RecordedProjection{Candidate: candidate, Plan: planned.Snapshot(), Simulation: result,
		AvailableBalances: balances}, nil
}

type recordedBalanceFact struct {
	Asset     domain.AssetSymbol `json:"asset"`
	Available domain.Balance     `json:"available"`
}

type recordedFillFacts struct {
	Legs     []arbitrage.Result `json:"legs"`
	Recovery RecoveryResult     `json:"recovery"`
}

type recordedPositionFacts struct {
	Outcome  SimulationOutcome `json:"outcome"`
	Recovery RecoveryResult    `json:"recovery"`
	State    string            `json:"state"`
}

type recordedJournalFacts struct {
	CandidateID string             `json:"candidate_id"`
	Outcome     SimulationOutcome  `json:"outcome"`
	FinalUSDT   domain.Quantity    `json:"final_usdt"`
	Legs        []arbitrage.Result `json:"legs"`
	Recovery    RecoveryResult     `json:"recovery"`
}

func recordedProjectionState(input Input, projection *RecordedProjection) (reconciliation.State, error) {
	if projection == nil || projection.Candidate.ID == "" ||
		projection.Simulation.CandidateID != projection.Candidate.ID {
		return reconciliation.State{}, strategyError("recorded_reduction_invalid")
	}
	before := canonicalRecordedBalances(recordedAvailableBalances(input))
	after := canonicalRecordedBalances(projection.AvailableBalances)
	orders, err := recordedProjectionHash(projection.Simulation.Saga)
	if err != nil {
		return reconciliation.State{}, err
	}
	fills, err := recordedProjectionHash(recordedFillFacts{Legs: projection.Simulation.Legs,
		Recovery: projection.Simulation.Recovery})
	if err != nil {
		return reconciliation.State{}, err
	}
	reservations, err := recordedProjectionHash(projection.Candidate.Claims)
	if err != nil {
		return reconciliation.State{}, err
	}
	balances, err := recordedProjectionHash(after)
	if err != nil {
		return reconciliation.State{}, err
	}
	positions, err := recordedProjectionHash(recordedPositionFacts{Outcome: projection.Simulation.Outcome,
		Recovery: projection.Simulation.Recovery, State: string(projection.Simulation.Saga.State)})
	if err != nil {
		return reconciliation.State{}, err
	}
	ownership, journal, projections, err := recordedEvidenceHashes(input, projection, before, after)
	if err != nil {
		return reconciliation.State{}, err
	}
	return reconciliation.State{Orders: orders, Fills: fills, Reservations: reservations,
		Balances: balances, Positions: positions, Ownership: ownership, Journal: journal,
		Projections: projections}, nil
}

func recordedEvidenceHashes(input Input, projection *RecordedProjection,
	before, after []recordedBalanceFact,
) (string, string, string, error) {
	ownership, err := recordedProjectionHash(struct {
		Exchange string                `json:"exchange"`
		Balances []recordedBalanceFact `json:"balances"`
	}{Exchange: input.Exchange, Balances: before})
	if err != nil {
		return "", "", "", err
	}
	journal, err := recordedProjectionHash(recordedJournalFacts{CandidateID: projection.Candidate.ID,
		Outcome: projection.Simulation.Outcome, FinalUSDT: projection.Simulation.FinalUSDT,
		Legs: projection.Simulation.Legs, Recovery: projection.Simulation.Recovery})
	if err != nil {
		return "", "", "", err
	}
	projections, err := recordedProjectionHash(struct {
		CandidateHash  string                `json:"candidate_hash"`
		SimulationHash string                `json:"simulation_hash"`
		Balances       []recordedBalanceFact `json:"balances"`
	}{CandidateHash: projection.Candidate.ID,
		SimulationHash: projection.Simulation.CanonicalHash, Balances: after})
	if err != nil {
		return "", "", "", err
	}
	return ownership, journal, projections, nil
}

func recordedAvailableBalances(input Input) map[domain.AssetSymbol]domain.Balance {
	result := cloneBalances(input.FeeBalances)
	zero, _ := domain.ParseBalance("0")
	for _, value := range []string{"BTC", "ETH"} {
		asset, _ := domain.ParseAssetSymbol(value)
		if _, exists := result[asset]; !exists {
			result[asset] = zero
		}
	}
	settlement, _ := domain.ParseAssetSymbol("USDT")
	result[settlement] = input.AvailableSettlement
	return result
}

func canonicalRecordedBalances(values map[domain.AssetSymbol]domain.Balance) []recordedBalanceFact {
	result := make([]recordedBalanceFact, 0, len(values))
	for asset, available := range values {
		result = append(result, recordedBalanceFact{Asset: asset, Available: available})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Asset < result[right].Asset })
	return result
}

func recordedProjectionHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil || len(payload) == 0 {
		return "", strategyError("recorded_reduction_invalid")
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}
