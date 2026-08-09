package crossarb

import (
	"encoding/json"
	"reflect"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
	"axiom/internal/risk"
	"axiom/internal/strategies/arbitrage"
)

// ApprovedSandboxSaga is the credential-free, strategy-owned projection of
// one centrally approved paired plan. It retains separate venue economics and
// order identities but has no authenticated adapter or credential authority.
type ApprovedSandboxSaga struct {
	Input     Input
	Candidate Candidate
	Decision  risk.Decision
	Claims    portfolio.ClaimGroup
	Execution execution.Saga
}

// SandboxLeg is one exact credential-free IOC order materialization. The
// concurrent plan retains each venue separately so a coordinator can bind the
// leg to that venue's account, fence, inventory, and public evidence.
type SandboxLeg struct {
	Index      uint32
	OrderID    domain.VirtualOrderID
	Exchange   string
	Instrument domain.Instrument
	Side       domain.Side
	Quantity   domain.Quantity
	LimitPrice domain.Price
	Notional   domain.Notional
	Source     domain.AssetSymbol
	Target     domain.AssetSymbol
}

// DecodeApprovedSandboxSaga verifies the exact concurrent saga produced by
// the strategy planner and rejects expired, modified, or unapproved payloads.
func DecodeApprovedSandboxSaga(planned backtest.SagaPlan) (ApprovedSandboxSaga, error) {
	var value sagaPlan
	if planned.Ordinal == 0 || json.Unmarshal(planned.Payload, &value) != nil ||
		value.Approval.Allocation.Input.Ordinal != planned.Ordinal ||
		value.Approval.Allocation.Input.ValidateEventBinding(
			planned.Ordinal, value.Approval.Allocation.Input.LogicalTime,
		) != nil || value.Approval.Decision.Action != risk.ActionApprove ||
		value.Approval.Allocation.Claims.State != portfolio.ClaimActive ||
		value.Approval.Allocation.Candidate.ID == "" ||
		value.Approval.Allocation.Input.LogicalTime >
			value.Approval.Allocation.Candidate.ExpiresOffsetNanos {
		return ApprovedSandboxSaga{}, strategyError("sandbox_saga_plan_invalid")
	}
	expected, _, err := newConcurrentSaga(value.Approval.Allocation.Candidate)
	if err != nil || !reflect.DeepEqual(value.Execution, expected.Snapshot()) ||
		value.Execution.Policy != execution.DispatchConcurrent || len(value.Execution.Legs) != 2 {
		return ApprovedSandboxSaga{}, strategyError("sandbox_saga_plan_invalid")
	}
	return ApprovedSandboxSaga{Input: value.Approval.Allocation.Input,
		Candidate: value.Approval.Allocation.Candidate, Decision: value.Approval.Decision,
		Claims: value.Approval.Allocation.Claims, Execution: value.Execution}, nil
}

// SandboxLegs revalidates the selected two-venue candidate and derives the
// worst executable limit from the exact coherent books used by evaluation.
func (approved ApprovedSandboxSaga) SandboxLegs() ([]SandboxLeg, error) {
	if !approvedSandboxSagaCurrent(approved) {
		return nil, strategyError("sandbox_saga_plan_invalid")
	}
	candidateLegs := []arbitrage.Result{approved.Candidate.Buy, approved.Candidate.Sell}
	result := make([]SandboxLeg, 0, len(candidateLegs))
	for index, candidateLeg := range candidateLegs {
		limit, err := sandboxLimitPrice(approved.Input, candidateLeg)
		if err != nil {
			return nil, err
		}
		notional, err := domain.CalculateNotional(limit, candidateLeg.TradeQuantity, 18)
		if err != nil {
			return nil, strategyError("sandbox_saga_leg_invalid")
		}
		result = append(result, SandboxLeg{Index: uint32(index),
			OrderID:  approved.Execution.Legs[index].OrderID,
			Exchange: candidateLeg.Exchange, Instrument: candidateLeg.Instrument,
			Side: candidateLeg.Side, Quantity: candidateLeg.TradeQuantity,
			LimitPrice: limit, Notional: notional,
			Source: candidateLeg.Source, Target: candidateLeg.Target})
	}
	return result, nil
}

func approvedSandboxSagaCurrent(approved ApprovedSandboxSaga) bool {
	evaluation, err := approved.Input.EvaluationInput()
	if err != nil || approved.Decision.Action != risk.ActionApprove ||
		approved.Claims.State != portfolio.ClaimActive ||
		approved.Input.LogicalTime > approved.Candidate.ExpiresOffsetNanos {
		return false
	}
	candidates, err := Evaluate(evaluation)
	if err != nil {
		return false
	}
	found := false
	for _, candidate := range candidates {
		if reflect.DeepEqual(candidate, approved.Candidate) {
			found = true
			break
		}
	}
	expected, _, err := newConcurrentSaga(approved.Candidate)
	return found && err == nil && reflect.DeepEqual(approved.Execution, expected.Snapshot())
}

func sandboxLimitPrice(input Input, leg arbitrage.Result) (domain.Price, error) {
	for _, market := range input.Markets {
		if string(market.Snapshot.Exchange) != leg.Exchange ||
			market.Snapshot.Instrument != leg.Instrument {
			continue
		}
		levels := market.Snapshot.Bids
		if leg.Side == domain.SideBuy {
			levels = market.Snapshot.Asks
		} else if leg.Side != domain.SideSell {
			return domain.Price{}, strategyError("sandbox_saga_leg_invalid")
		}
		remaining := leg.TradeQuantity
		for _, level := range levels {
			if remaining.Compare(level.Quantity) <= 0 {
				return level.Price, nil
			}
			var err error
			remaining, err = remaining.Subtract(level.Quantity)
			if err != nil {
				return domain.Price{}, strategyError("sandbox_saga_leg_invalid")
			}
		}
	}
	return domain.Price{}, strategyError("sandbox_saga_leg_invalid")
}
