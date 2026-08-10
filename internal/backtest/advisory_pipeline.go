package backtest

import (
	"context"
	"encoding/json"

	"axiom/internal/replay"
)

// AdvisoryCandidate is one strategy recommendation or explained no-action
// outcome. Its payload can be consumed only by the advisory allocation path.
type AdvisoryCandidate struct {
	Ordinal  uint64
	Decision json.RawMessage
	Payload  json.RawMessage
}

// AdvisoryAllocation records the inventory-ownership decision without
// reserving funds or authorizing an external action.
type AdvisoryAllocation struct {
	Ordinal  uint64
	Evidence json.RawMessage
	Payload  json.RawMessage
}

// AdvisoryRiskDecision records the central risk review of an advisory result.
type AdvisoryRiskDecision struct {
	Ordinal  uint64
	Evidence json.RawMessage
	Payload  json.RawMessage
}

// AdvisoryPlan is an explainable manual plan. ExternalActionAllowed must stay
// false; the advisory pipeline has no broker or exchange adapter dependency.
type AdvisoryPlan struct {
	Ordinal               uint64
	Evidence              json.RawMessage
	Payload               json.RawMessage
	ExternalActionAllowed bool
}

// AdvisoryAccountingRecord proves that the advisory run did not mutate a
// portfolio or journal while still producing an owner-visible balance view.
type AdvisoryAccountingRecord struct {
	Ordinal          uint64
	Evidence         json.RawMessage
	Payload          json.RawMessage
	Balances         json.RawMessage
	MutationRecorded bool
}

// AdvisoryReconciliation records the final no-action consistency check.
type AdvisoryReconciliation struct {
	Ordinal                   uint64
	Evidence                  json.RawMessage
	NoExternalActionConfirmed bool
}

// AdvisoryStrategy evaluates immutable input without creating an order intent.
type AdvisoryStrategy interface {
	EvaluateAdvisory(context.Context, replay.Event) (AdvisoryCandidate, error)
}

// AdvisoryAllocator validates inventory ownership without reserving capital.
type AdvisoryAllocator interface {
	AllocateAdvisory(context.Context, AdvisoryCandidate) (AdvisoryAllocation, error)
}

// AdvisoryRiskEngine applies central risk policy to advisory evidence.
type AdvisoryRiskEngine interface {
	ReviewAdvisory(context.Context, AdvisoryAllocation) (AdvisoryRiskDecision, error)
}

// AdvisoryPlanner converts an approved recommendation into a manual-only plan.
type AdvisoryPlanner interface {
	PlanAdvisory(context.Context, AdvisoryRiskDecision) (AdvisoryPlan, error)
}

// AdvisoryAccounting records the explicit no-mutation accounting result.
type AdvisoryAccounting interface {
	RecordAdvisory(context.Context, AdvisoryPlan) (AdvisoryAccountingRecord, error)
}

// AdvisoryReconciler confirms that no external action or journal mutation
// escaped the advisory boundary.
type AdvisoryReconciler interface {
	ReconcileAdvisory(context.Context, AdvisoryAccountingRecord) (AdvisoryReconciliation, error)
}

// AdvisoryPipelineDependencies are the complete fail-closed stages for a
// recommendation-only strategy. A broker is deliberately not representable.
type AdvisoryPipelineDependencies struct {
	Strategy       AdvisoryStrategy
	Allocator      AdvisoryAllocator
	Risk           AdvisoryRiskEngine
	Planner        AdvisoryPlanner
	Accounting     AdvisoryAccounting
	Reconciliation AdvisoryReconciler
	Metrics        func() Metrics
}

// AdvisoryPipelineProcessor runs every advisory mode through the same
// evaluation, allocation, risk, planning, accounting, and reconciliation path.
type AdvisoryPipelineProcessor struct{ dependencies AdvisoryPipelineDependencies }

// NewAdvisoryPipelineProcessor rejects any incomplete advisory runtime.
func NewAdvisoryPipelineProcessor(dependencies AdvisoryPipelineDependencies) (*AdvisoryPipelineProcessor, error) {
	if dependencies.Strategy == nil || dependencies.Allocator == nil || dependencies.Risk == nil ||
		dependencies.Planner == nil || dependencies.Accounting == nil || dependencies.Reconciliation == nil ||
		dependencies.Metrics == nil {
		return nil, backtestError("advisory_pipeline_incomplete")
	}
	return &AdvisoryPipelineProcessor{dependencies: dependencies}, nil
}

// Process emits no orders or execution events and fails if any stage attempts
// to cross the recommendation-only boundary.
func (processor *AdvisoryPipelineProcessor) Process(ctx context.Context, event replay.Event) (EventResult, error) {
	candidate, err := processor.dependencies.Strategy.EvaluateAdvisory(ctx, event)
	if err != nil || !validAdvisoryStage(event.Ordinal, candidate.Ordinal, candidate.Decision, candidate.Payload) {
		return EventResult{}, backtestError("advisory_strategy_stage_failed")
	}
	allocation, err := processor.dependencies.Allocator.AllocateAdvisory(ctx, candidate)
	if err != nil || !validAdvisoryStage(event.Ordinal, allocation.Ordinal, allocation.Evidence, allocation.Payload) {
		return EventResult{}, backtestError("advisory_allocation_stage_failed")
	}
	riskDecision, err := processor.dependencies.Risk.ReviewAdvisory(ctx, allocation)
	if err != nil || !validAdvisoryStage(event.Ordinal, riskDecision.Ordinal, riskDecision.Evidence, riskDecision.Payload) {
		return EventResult{}, backtestError("advisory_risk_stage_failed")
	}
	plan, err := processor.dependencies.Planner.PlanAdvisory(ctx, riskDecision)
	if err != nil || plan.ExternalActionAllowed ||
		!validAdvisoryStage(event.Ordinal, plan.Ordinal, plan.Evidence, plan.Payload) {
		return EventResult{}, backtestError("advisory_planning_stage_failed")
	}
	accountingRecord, err := processor.dependencies.Accounting.RecordAdvisory(ctx, plan)
	if err != nil || accountingRecord.MutationRecorded ||
		!validAdvisoryStage(event.Ordinal, accountingRecord.Ordinal, accountingRecord.Evidence, accountingRecord.Payload) ||
		!validCanonicalJSON(accountingRecord.Balances) {
		return EventResult{}, backtestError("advisory_accounting_stage_failed")
	}
	reconciliation, err := processor.dependencies.Reconciliation.ReconcileAdvisory(ctx, accountingRecord)
	if err != nil || !reconciliation.NoExternalActionConfirmed ||
		!validAdvisoryEvidence(event.Ordinal, reconciliation.Ordinal, reconciliation.Evidence) {
		return EventResult{}, backtestError("advisory_reconciliation_stage_failed")
	}
	decision, err := json.Marshal(struct {
		AdvisoryOnly   bool            `json:"advisory_only"`
		Strategy       json.RawMessage `json:"strategy"`
		Allocation     json.RawMessage `json:"allocation"`
		Risk           json.RawMessage `json:"risk"`
		Plan           json.RawMessage `json:"plan"`
		Accounting     json.RawMessage `json:"accounting"`
		Reconciliation json.RawMessage `json:"reconciliation"`
	}{true, candidate.Decision, allocation.Evidence, riskDecision.Evidence, plan.Evidence,
		accountingRecord.Evidence, reconciliation.Evidence})
	if err != nil {
		return EventResult{}, backtestError("advisory_output_invalid")
	}
	return EventResult{Ordinal: event.Ordinal, Decision: decision, Orders: json.RawMessage("[]"),
		ExecutionEvents: json.RawMessage("[]"), Balances: append(json.RawMessage(nil), accountingRecord.Balances...)}, nil
}

// Metrics returns the strategy-independent advisory result metrics.
func (processor *AdvisoryPipelineProcessor) Metrics() Metrics {
	return processor.dependencies.Metrics()
}

func validAdvisoryStage(want, got uint64, evidence, payload json.RawMessage) bool {
	return validAdvisoryEvidence(want, got, evidence) && validCanonicalJSON(payload)
}

func validAdvisoryEvidence(want, got uint64, evidence json.RawMessage) bool {
	return want > 0 && got == want && validCanonicalJSON(evidence)
}

func validCanonicalJSON(value json.RawMessage) bool {
	return len(value) > 0 && json.Valid(value)
}

var _ Processor = (*AdvisoryPipelineProcessor)(nil)
