package backtest

import (
	"context"
	"encoding/json"
	"testing"

	"axiom/internal/replay"
)

func TestAdvisoryPipelineRunsEveryNoActionStageInOrder(t *testing.T) {
	stages := []string{}
	fixture := &advisoryPipelineFixture{stages: &stages}
	processor, err := NewAdvisoryPipelineProcessor(AdvisoryPipelineDependencies{
		Strategy: fixture, Allocator: fixture, Risk: fixture, Planner: fixture,
		Accounting: fixture, Reconciliation: fixture,
		Metrics: func() Metrics { return Metrics{TotalNetReturn: "not_applicable"} },
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := processor.Process(context.Background(), replay.Event{Ordinal: 7, LogicalTime: 11, Canonical: []byte(`{}`)})
	if err != nil || string(result.Orders) != "[]" || string(result.ExecutionEvents) != "[]" ||
		string(result.Balances) != `{"USDT":"100"}` {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []string{"strategy", "allocation", "risk", "plan", "accounting", "reconciliation"}
	if stringSlice(stages) != stringSlice(want) {
		t.Fatalf("stages=%v want=%v", stages, want)
	}
	var decision map[string]json.RawMessage
	if json.Unmarshal(result.Decision, &decision) != nil || len(decision) != 7 {
		t.Fatalf("decision=%s", result.Decision)
	}
}

func TestAdvisoryPipelineRejectsExternalActionAndMutation(t *testing.T) {
	for _, test := range []struct {
		name     string
		external bool
		mutation bool
	}{
		{name: "external action", external: true},
		{name: "accounting mutation", mutation: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := &advisoryPipelineFixture{stages: &[]string{}, external: test.external, mutation: test.mutation}
			processor, err := NewAdvisoryPipelineProcessor(AdvisoryPipelineDependencies{
				Strategy: fixture, Allocator: fixture, Risk: fixture, Planner: fixture,
				Accounting: fixture, Reconciliation: fixture, Metrics: func() Metrics { return Metrics{} },
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = processor.Process(context.Background(), replay.Event{Ordinal: 1, LogicalTime: 1, Canonical: []byte(`{}`)}); err == nil {
				t.Fatal("unsafe advisory result accepted")
			}
		})
	}
}

type advisoryPipelineFixture struct {
	stages   *[]string
	external bool
	mutation bool
}

func (fixture *advisoryPipelineFixture) append(stage string) {
	*fixture.stages = append(*fixture.stages, stage)
}

func (fixture *advisoryPipelineFixture) EvaluateAdvisory(context.Context, replay.Event) (AdvisoryCandidate, error) {
	fixture.append("strategy")
	return AdvisoryCandidate{Ordinal: 7, Decision: json.RawMessage(`{"outcome":"recommended"}`), Payload: json.RawMessage(`{}`)}, nil
}

func (fixture *advisoryPipelineFixture) AllocateAdvisory(context.Context, AdvisoryCandidate) (AdvisoryAllocation, error) {
	fixture.append("allocation")
	return AdvisoryAllocation{Ordinal: 7, Evidence: json.RawMessage(`{"inventory":"confirmed"}`), Payload: json.RawMessage(`{}`)}, nil
}

func (fixture *advisoryPipelineFixture) ReviewAdvisory(context.Context, AdvisoryAllocation) (AdvisoryRiskDecision, error) {
	fixture.append("risk")
	return AdvisoryRiskDecision{Ordinal: 7, Evidence: json.RawMessage(`{"status":"reviewed"}`), Payload: json.RawMessage(`{}`)}, nil
}

func (fixture *advisoryPipelineFixture) PlanAdvisory(context.Context, AdvisoryRiskDecision) (AdvisoryPlan, error) {
	fixture.append("plan")
	return AdvisoryPlan{Ordinal: 7, Evidence: json.RawMessage(`{"manual_only":true}`), Payload: json.RawMessage(`{}`), ExternalActionAllowed: fixture.external}, nil
}

func (fixture *advisoryPipelineFixture) RecordAdvisory(context.Context, AdvisoryPlan) (AdvisoryAccountingRecord, error) {
	fixture.append("accounting")
	return AdvisoryAccountingRecord{Ordinal: 7, Evidence: json.RawMessage(`{"unchanged":true}`), Payload: json.RawMessage(`{}`), Balances: json.RawMessage(`{"USDT":"100"}`), MutationRecorded: fixture.mutation}, nil
}

func (fixture *advisoryPipelineFixture) ReconcileAdvisory(context.Context, AdvisoryAccountingRecord) (AdvisoryReconciliation, error) {
	fixture.append("reconciliation")
	return AdvisoryReconciliation{Ordinal: 7, Evidence: json.RawMessage(`{"no_external_action":true}`), NoExternalActionConfirmed: true}, nil
}

func stringSlice(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}
