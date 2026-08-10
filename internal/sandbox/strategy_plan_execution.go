package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
)

// StrategyPlanExecution is the terminal, session-owned execution fact for a
// single automatic strategy plan. It is deliberately a read model: neither
// this value nor its source carries adapter, cancellation, or submission
// authority.
type StrategyPlanExecution struct {
	PlanID             string
	Side               domain.Side
	RequestedQuantity  domain.Quantity
	CumulativeQuantity domain.Quantity
	Fills              []execution.FillFact
	EvidenceHash       string
	ObservedAt         time.Time
}

// ValidFor proves that a terminal plan fact belongs to exactly one journal
// decision. A zero-fill rejection/cancellation is valid, while a partial fill
// remains explicit through CumulativeQuantity and must never be rounded up.
func (executionFact StrategyPlanExecution) ValidFor(
	entry StrategyDecisionJournalEntry,
	now time.Time,
) error {
	zero, err := domain.ParseQuantity("0")
	zeroPrice, priceErr := domain.ParsePrice("0")
	if err != nil || entry.PlanID == "" || executionFact.PlanID != entry.PlanID ||
		(executionFact.Side != domain.SideBuy && executionFact.Side != domain.SideSell) ||
		executionFact.RequestedQuantity.Compare(zero) <= 0 ||
		executionFact.CumulativeQuantity.Compare(zero) < 0 ||
		executionFact.CumulativeQuantity.Compare(executionFact.RequestedQuantity) > 0 ||
		executionFact.ObservedAt.IsZero() || executionFact.ObservedAt.Location() != time.UTC ||
		executionFact.ObservedAt.After(now) || !hash256(executionFact.EvidenceHash) {
		return contractError("strategy_plan_execution_invalid")
	}
	total := zero
	seen := make(map[string]struct{}, len(executionFact.Fills))
	for _, fill := range executionFact.Fills {
		if priceErr != nil || fill.ID.String() == "" || fill.Ordinal == 0 || fill.Quantity.Compare(zero) <= 0 ||
			fill.Price.Compare(zeroPrice) <= 0 {
			return contractError("strategy_plan_execution_invalid")
		}
		if _, duplicate := seen[fill.ID.String()]; duplicate {
			return contractError("strategy_plan_execution_invalid")
		}
		seen[fill.ID.String()] = struct{}{}
		total, err = total.Add(fill.Quantity)
		if err != nil {
			return contractError("strategy_plan_execution_invalid")
		}
	}
	if total.Compare(executionFact.CumulativeQuantity) != 0 ||
		executionFact.EvidenceHash != StrategyPlanExecutionEvidenceHash(executionFact) {
		return contractError("strategy_plan_execution_invalid")
	}
	return nil
}

// StrategyPlanExecutionEvidenceHash produces the canonical immutable digest
// used by the durable reader and every position projection consumer.
func StrategyPlanExecutionEvidenceHash(value StrategyPlanExecution) string {
	parts := []string{value.PlanID, string(value.Side), value.RequestedQuantity.String(),
		value.CumulativeQuantity.String(), value.ObservedAt.UTC().Format(time.RFC3339Nano)}
	for _, fill := range value.Fills {
		parts = append(parts, fill.ID.String(), fill.Quantity.String(), fill.Price.String(),
			fmt.Sprintf("%d", fill.Ordinal))
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// AverageFillPrice calculates a deterministic weighted entry price from the
// immutable fill facts. It deliberately rejects a zero-fill execution: a
// canceled or rejected entry leaves the strategy position closed rather than
// creating a fictitious zero-price position.
func (executionFact StrategyPlanExecution) AverageFillPrice() (domain.Price, error) {
	zeroQuantity, err := domain.ParseQuantity("0")
	if err != nil || executionFact.CumulativeQuantity.Compare(zeroQuantity) <= 0 {
		return domain.Price{}, contractError("strategy_plan_execution_no_fills")
	}
	zeroMoney, err := domain.ParseMoney("0")
	if err != nil {
		return domain.Price{}, contractError("strategy_plan_execution_invalid")
	}
	totalQuantity, totalCost := zeroQuantity, zeroMoney
	for _, fill := range executionFact.Fills {
		notional, notionalErr := domain.CalculateNotional(fill.Price, fill.Quantity, 18)
		if notionalErr != nil {
			return domain.Price{}, contractError("strategy_plan_execution_invalid")
		}
		cost, parseErr := domain.ParseMoney(notional.String())
		if parseErr != nil {
			return domain.Price{}, contractError("strategy_plan_execution_invalid")
		}
		totalQuantity, err = totalQuantity.Add(fill.Quantity)
		if err != nil {
			return domain.Price{}, contractError("strategy_plan_execution_invalid")
		}
		totalCost, err = totalCost.Add(cost)
		if err != nil {
			return domain.Price{}, contractError("strategy_plan_execution_invalid")
		}
	}
	if totalQuantity.Compare(executionFact.CumulativeQuantity) != 0 {
		return domain.Price{}, contractError("strategy_plan_execution_invalid")
	}
	owned, err := domain.ParseBalance(totalQuantity.String())
	if err != nil {
		return domain.Price{}, contractError("strategy_plan_execution_invalid")
	}
	average, err := domain.CalculateAveragePrice(totalCost, owned, 18)
	if err != nil {
		return domain.Price{}, contractError("strategy_plan_execution_invalid")
	}
	return average, nil
}

// StrategyPlanExecutionSource returns only a terminal, exact one-leg plan
// result for a decision journal entry. It fails closed while the order is
// pending, ambiguous, recovering, or not proven session-owned.
type StrategyPlanExecutionSource interface {
	StrategyPlanExecution(context.Context, StrategySessionWork, StrategyDecisionJournalEntry, time.Time) (StrategyPlanExecution, error)
}
