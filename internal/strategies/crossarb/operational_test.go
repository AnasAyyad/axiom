package crossarb

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"axiom/internal/backtest"
	"axiom/internal/replay"
)

func TestOperationalProcessorPreservesNoEligibleDirectionAsNoActionEvidence(t *testing.T) {
	evaluation := evaluationFixture(t, "BTC", false)
	evaluation.Restoration.MarginalInventoryReplacement = money("100")
	input := inputFromEvaluation(t, evaluation)
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	pipeline := crossNoActionPipeline(t)
	processor, err := NewOperationalProcessor(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), replay.Event{
		Ordinal: input.Ordinal, LogicalTime: input.LogicalTime, Canonical: payload,
	})
	if err != nil || result.Ordinal != input.Ordinal || string(result.Orders) != "[]" ||
		string(result.ExecutionEvents) != "[]" || !json.Valid(result.Balances) || processor.Metrics().Trades != 0 {
		t.Fatalf("no-action result=%#v metrics=%#v error=%v", result, processor.Metrics(), err)
	}
	var decision EvaluationDecision
	if err = json.Unmarshal(result.Decision, &decision); err != nil || decision.Action != "no_action" ||
		decision.ReasonCode != "no_eligible_direction" || decision.CandidateCount != 0 ||
		decision.ConfigurationHash != input.ConfigurationHash ||
		decision.InstrumentMetadataSetHash != input.InstrumentMetadataSetHash {
		t.Fatalf("no-action decision=%#v error=%v", decision, err)
	}
}

func TestOperationalProcessorDoesNotConvertInvalidInputToNoAction(t *testing.T) {
	pipeline := crossNoActionPipeline(t)
	processor, err := NewOperationalProcessor(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processor.Process(context.Background(), replay.Event{Ordinal: 1, LogicalTime: 1,
		Canonical: json.RawMessage(`{"ordinal":1,"logical_time":1}`)}); err == nil {
		t.Fatal("invalid cross-exchange input was converted to no action")
	}
}

func crossNoActionPipeline(t *testing.T) *backtest.SagaPipelineProcessor {
	t.Helper()
	stage := crossUnexpectedSagaStage{}
	pipeline, err := backtest.NewSagaPipelineProcessor(backtest.SagaPipelineDependencies{
		Strategy: stage, Allocator: stage, Risk: stage, Planner: stage, Broker: stage, Reducer: stage,
		Metrics: func() backtest.Metrics { return backtest.Metrics{TotalNetReturn: "not_evaluated"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	return pipeline
}

type crossUnexpectedSagaStage struct{}

func (crossUnexpectedSagaStage) EvaluateSaga(context.Context, replay.Event) (backtest.SagaCandidate, error) {
	return backtest.SagaCandidate{}, errors.New("unexpected strategy stage")
}
func (crossUnexpectedSagaStage) AllocateSaga(context.Context, backtest.SagaCandidate) (backtest.SagaAllocation, error) {
	return backtest.SagaAllocation{}, errors.New("unexpected allocation stage")
}
func (crossUnexpectedSagaStage) CloseSagaAllocation(context.Context, backtest.SagaAllocation, backtest.AllocationDisposition) error {
	return errors.New("unexpected allocation close")
}
func (crossUnexpectedSagaStage) ApproveSaga(context.Context, backtest.SagaAllocation) (backtest.SagaApproval, error) {
	return backtest.SagaApproval{}, errors.New("unexpected risk stage")
}
func (crossUnexpectedSagaStage) PlanSaga(context.Context, backtest.SagaApproval) (backtest.SagaPlan, error) {
	return backtest.SagaPlan{}, errors.New("unexpected planning stage")
}
func (crossUnexpectedSagaStage) SubmitSaga(context.Context, backtest.SagaPlan) (backtest.SagaExecution, error) {
	return backtest.SagaExecution{}, errors.New("unexpected broker stage")
}
func (crossUnexpectedSagaStage) ReduceSaga(context.Context, backtest.SagaAllocation, backtest.SagaPlan, backtest.SagaExecution) (backtest.EventResult, error) {
	return backtest.EventResult{}, errors.New("unexpected reducer stage")
}
