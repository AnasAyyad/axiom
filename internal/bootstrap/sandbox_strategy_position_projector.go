package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/sandbox"
	"axiom/internal/strategies/meanreversion"
	"axiom/internal/strategies/trend"
)

// SandboxStrategyPositionProjector deterministically replays one session's
// immutable strategy decisions and terminal fill facts. It has no market-data,
// account, broker, dispatcher, or scheduler authority; callers use its output
// only to construct the next pure strategy input.
type SandboxStrategyPositionProjector struct {
	journal    sandbox.StrategyDecisionJournalSource
	executions sandbox.StrategyPlanExecutionSource
}

// NewSandboxStrategyPositionProjector rebuilds owned positions from durable evidence.
func NewSandboxStrategyPositionProjector(
	journal sandbox.StrategyDecisionJournalSource,
	executions sandbox.StrategyPlanExecutionSource,
) (*SandboxStrategyPositionProjector, error) {
	if journal == nil || executions == nil {
		return nil, fmt.Errorf("sandbox_strategy_position_projector_invalid")
	}
	return &SandboxStrategyPositionProjector{journal: journal, executions: executions}, nil
}

// TrendPosition replays only the exact Trend session chain. A decision input
// that disagrees with the replayed prior state is rejected rather than used to
// overwrite it. This prevents an unowned account balance or a stale in-memory
// position from authorizing a later sell.
func (projector *SandboxStrategyPositionProjector) TrendPosition(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	configuration trend.Configuration,
	now time.Time,
) (trend.PositionState, error) {
	if projector == nil || ctx == nil || work.Strategy != sandbox.StrategyTrend ||
		work.ValidAt(now) != nil {
		return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	evaluator, err := trend.NewEvaluator(configuration)
	if err != nil {
		return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	entries, err := projector.entries(ctx, work, now)
	if err != nil {
		return trend.PositionState{}, err
	}
	position := trend.PositionState{}
	for _, entry := range entries {
		var input trend.Input
		var decision trend.Decision
		if json.Unmarshal(entry.Evidence.CanonicalInput, &input) != nil ||
			json.Unmarshal(entry.Evidence.CanonicalDecision, &decision) != nil ||
			input.Ordinal != entry.Evidence.EventOrdinal ||
			input.LogicalTime != entry.Evidence.EventLogicalTime ||
			input.Instrument.Symbol() != work.Instrument || decision.ID.String() != entry.Evidence.DecisionID ||
			decision.Ordinal != entry.Evidence.EventOrdinal || !sameStrategyState(input.Position, position) {
			return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		expected, evaluateErr := evaluator.Evaluate(input)
		if evaluateErr != nil || !sameStrategyDecision(expected, decision) {
			return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		next, transitionErr := projectTrendDecision(position, input, decision, configuration)
		if transitionErr != nil {
			return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		position, err = projector.applyTrendPlan(ctx, work, entry, decision, next, configuration, now)
		if err != nil {
			return trend.PositionState{}, err
		}
	}
	return position, nil
}

// MeanReversionPosition replays holding time, cooldown, and exact fill-owned
// quantity for the immutable Mean Reversion rule graph.
func (projector *SandboxStrategyPositionProjector) MeanReversionPosition(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	configuration meanreversion.Configuration,
	now time.Time,
) (meanreversion.PositionState, error) {
	if projector == nil || ctx == nil || work.Strategy != sandbox.StrategyMeanReversion ||
		work.ValidAt(now) != nil {
		return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	evaluator, err := meanreversion.NewEvaluator(configuration)
	if err != nil {
		return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	entries, err := projector.entries(ctx, work, now)
	if err != nil {
		return meanreversion.PositionState{}, err
	}
	position := meanreversion.PositionState{}
	for _, entry := range entries {
		var input meanreversion.Input
		var decision meanreversion.Decision
		if json.Unmarshal(entry.Evidence.CanonicalInput, &input) != nil ||
			json.Unmarshal(entry.Evidence.CanonicalDecision, &decision) != nil ||
			input.Ordinal != entry.Evidence.EventOrdinal ||
			input.LogicalTime != entry.Evidence.EventLogicalTime ||
			input.Instrument.Symbol() != work.Instrument || decision.ID.String() != entry.Evidence.DecisionID ||
			decision.Ordinal != entry.Evidence.EventOrdinal || !sameStrategyState(input.Position, position) {
			return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		expected, evaluateErr := evaluator.Evaluate(input)
		if evaluateErr != nil || !sameStrategyDecision(expected, decision) {
			return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		next := projectMeanReversionDecision(position, decision)
		position, err = projector.applyMeanReversionPlan(ctx, work, entry, decision, next, configuration, now)
		if err != nil {
			return meanreversion.PositionState{}, err
		}
	}
	return position, nil
}

func (projector *SandboxStrategyPositionProjector) entries(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	now time.Time,
) ([]sandbox.StrategyDecisionJournalEntry, error) {
	entries, err := projector.journal.StrategyDecisionJournal(ctx, work, now)
	if err != nil {
		return nil, fmt.Errorf("sandbox_strategy_position_projection_unavailable")
	}
	var prior uint64
	for _, entry := range entries {
		if entry.ValidFor(work, now) != nil || (prior != 0 && entry.Evidence.EventOrdinal <= prior) {
			return nil, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		prior = entry.Evidence.EventOrdinal
	}
	return entries, nil
}

func projectTrendDecision(
	position trend.PositionState,
	input trend.Input,
	decision trend.Decision,
	configuration trend.Configuration,
) (trend.PositionState, error) {
	next := position
	if position.Open && (decision.Action == trend.ActionExit || decision.ReasonCode == trend.ReasonExistingPosition) {
		if len(input.Candles) == 0 {
			return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		return trend.AdvancePosition(position, input.Candles[len(input.Candles)-1].Close,
			decision.Explanation.ATR14, configuration)
	}
	if !position.Open && position.CooldownRemaining > 0 && decision.ReasonCode == trend.ReasonCooldown {
		next.CooldownRemaining = trend.AdvanceCooldown(position.CooldownRemaining)
	}
	return next, nil
}

func projectMeanReversionDecision(
	position meanreversion.PositionState,
	decision meanreversion.Decision,
) meanreversion.PositionState {
	if position.Open && (decision.Action == meanreversion.ActionExit || decision.ReasonCode == meanreversion.ReasonHoldPosition) {
		return meanreversion.AdvanceHolding(position)
	}
	if !position.Open && position.CooldownRemaining > 0 && decision.ReasonCode == meanreversion.ReasonCooldown {
		position.CooldownRemaining = meanreversion.AdvanceCooldown(position.CooldownRemaining)
	}
	return position
}

func (projector *SandboxStrategyPositionProjector) applyTrendPlan(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	entry sandbox.StrategyDecisionJournalEntry,
	decision trend.Decision,
	position trend.PositionState,
	configuration trend.Configuration,
	now time.Time,
) (trend.PositionState, error) {
	if entry.PlanID == "" {
		return position, nil
	}
	if decision.Candidate == nil || (decision.Action != trend.ActionEntry && decision.Action != trend.ActionExit) {
		return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	executionFact, err := projector.executions.StrategyPlanExecution(ctx, work, entry, now)
	if err != nil || executionFact.ValidFor(entry, now) != nil ||
		executionFact.Side != decision.Candidate.Side ||
		executionFact.RequestedQuantity.Compare(decision.Candidate.Quantity) != 0 {
		return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_unavailable")
	}
	return applyTrendExecution(position, decision, executionFact, configuration)
}

func applyTrendExecution(
	position trend.PositionState,
	decision trend.Decision,
	executionFact sandbox.StrategyPlanExecution,
	configuration trend.Configuration,
) (trend.PositionState, error) {
	zero, _ := domain.ParseQuantity("0")
	if decision.Action == trend.ActionEntry {
		if position.Open {
			return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		if executionFact.CumulativeQuantity.Compare(zero) == 0 {
			return position, nil
		}
		average, err := executionFact.AverageFillPrice()
		if err != nil {
			return trend.PositionState{}, err
		}
		return trend.OpenPosition(average, decision.Explanation.ATR14, executionFact.CumulativeQuantity,
			configuration)
	}
	if !position.Open {
		return trend.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	remaining, err := position.Quantity.Subtract(executionFact.CumulativeQuantity)
	if err != nil {
		return trend.PositionState{}, err
	}
	if remaining.Compare(zero) == 0 {
		return trend.PositionState{CooldownRemaining: decision.CooldownStart}, nil
	}
	position.Quantity = remaining
	return position, nil
}

func (projector *SandboxStrategyPositionProjector) applyMeanReversionPlan(
	ctx context.Context,
	work sandbox.StrategySessionWork,
	entry sandbox.StrategyDecisionJournalEntry,
	decision meanreversion.Decision,
	position meanreversion.PositionState,
	configuration meanreversion.Configuration,
	now time.Time,
) (meanreversion.PositionState, error) {
	if entry.PlanID == "" {
		return position, nil
	}
	if decision.Candidate == nil || (decision.Action != meanreversion.ActionEntry && decision.Action != meanreversion.ActionExit) {
		return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	executionFact, err := projector.executions.StrategyPlanExecution(ctx, work, entry, now)
	if err != nil || executionFact.ValidFor(entry, now) != nil ||
		executionFact.Side != decision.Candidate.Side ||
		executionFact.RequestedQuantity.Compare(decision.Candidate.Quantity) != 0 {
		return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_unavailable")
	}
	return applyMeanReversionExecution(position, decision, executionFact, configuration)
}

func applyMeanReversionExecution(
	position meanreversion.PositionState,
	decision meanreversion.Decision,
	executionFact sandbox.StrategyPlanExecution,
	configuration meanreversion.Configuration,
) (meanreversion.PositionState, error) {
	zero, _ := domain.ParseQuantity("0")
	if decision.Action == meanreversion.ActionEntry {
		if position.Open {
			return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
		}
		if executionFact.CumulativeQuantity.Compare(zero) == 0 {
			return position, nil
		}
		average, err := executionFact.AverageFillPrice()
		if err != nil {
			return meanreversion.PositionState{}, err
		}
		return meanreversion.OpenPosition(average, decision.Explanation.ATR14, executionFact.CumulativeQuantity,
			configuration)
	}
	if !position.Open {
		return meanreversion.PositionState{}, fmt.Errorf("sandbox_strategy_position_projection_invalid")
	}
	remaining, err := position.Quantity.Subtract(executionFact.CumulativeQuantity)
	if err != nil {
		return meanreversion.PositionState{}, err
	}
	if remaining.Compare(zero) == 0 {
		return meanreversion.PositionState{CooldownRemaining: decision.CooldownStart}, nil
	}
	position.Quantity = remaining
	return position, nil
}

func sameStrategyState(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func sameStrategyDecision(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
