package crossarb

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/replay"
)

// EvaluationDecision records an ordinary evaluation that produced no
// executable paired-venue direction. It is not a central-risk rejection.
type EvaluationDecision struct {
	Action                    string `json:"action"`
	ReasonCode                string `json:"reason_code"`
	CandidateCount            int    `json:"candidate_count"`
	ConfigurationVersion      string `json:"configuration_version"`
	ConfigurationHash         string `json:"configuration_hash"`
	InstrumentMetadataSetHash string `json:"instrument_metadata_set_hash"`
	DecisionOffsetNanos       uint64 `json:"decision_offset_nanos"`
}

type evaluationBalanceEvidence struct {
	Inventory   []VenueInventory          `json:"inventory"`
	QuoteBudget domain.Balance            `json:"quote_budget"`
	FeeBalances map[string]domain.Balance `json:"fee_balances"`
}

// OperationalProcessor preserves normal no-op evaluations while routing every
// real candidate through the complete shared saga pipeline.
type OperationalProcessor struct {
	pipeline *backtest.SagaPipelineProcessor
	trades   atomic.Uint64
}

// NewOperationalProcessor binds the shared saga pipeline to the runtime adapter.
func NewOperationalProcessor(pipeline *backtest.SagaPipelineProcessor) (*OperationalProcessor, error) {
	if pipeline == nil {
		return nil, strategyError("operational_pipeline_invalid")
	}
	return &OperationalProcessor{pipeline: pipeline}, nil
}

// Process evaluates one coherent recorded or public cross-venue input.
func (processor *OperationalProcessor) Process(
	ctx context.Context,
	event replay.Event,
) (backtest.EventResult, error) {
	var input Input
	if processor == nil || processor.pipeline == nil || json.Unmarshal(event.Canonical, &input) != nil ||
		input.ValidateEventBinding(event.Ordinal, event.LogicalTime) != nil {
		return backtest.EventResult{}, strategyError("operational_input_invalid")
	}
	evaluation, err := input.EvaluationInput()
	if err != nil {
		return backtest.EventResult{}, err
	}
	if _, err = Evaluate(evaluation); err != nil {
		var rejected *Error
		if !errors.As(err, &rejected) || rejected.Code != "no_eligible_direction" {
			return backtest.EventResult{}, err
		}
		decision, decisionErr := json.Marshal(EvaluationDecision{Action: "no_action",
			ReasonCode: rejected.Code, CandidateCount: 0,
			ConfigurationVersion:      input.Configuration.StrategyVersion,
			ConfigurationHash:         input.ConfigurationHash,
			InstrumentMetadataSetHash: input.InstrumentMetadataSetHash,
			DecisionOffsetNanos:       input.LogicalTime})
		balances, balanceErr := json.Marshal(evaluationBalanceEvidence{Inventory: append([]VenueInventory(nil), input.Inventory...),
			QuoteBudget: input.QuoteBudget, FeeBalances: cloneFeeBalances(input.FeeBalances)})
		if decisionErr != nil || balanceErr != nil {
			return backtest.EventResult{}, strategyError("operational_evidence_encode_failed")
		}
		return backtest.EventResult{Ordinal: event.Ordinal, Decision: decision,
			Orders: json.RawMessage("[]"), ExecutionEvents: json.RawMessage("[]"), Balances: balances}, nil
	}
	result, err := processor.pipeline.Process(ctx, event)
	if err == nil {
		processor.trades.Add(2)
	}
	return result, err
}

// Metrics returns the pipeline metrics accumulated by this processor.
func (processor *OperationalProcessor) Metrics() backtest.Metrics {
	if processor == nil || processor.pipeline == nil {
		return backtest.Metrics{}
	}
	metrics := processor.pipeline.Metrics()
	metrics.Trades = processor.trades.Load()
	return metrics
}

var _ backtest.Processor = (*OperationalProcessor)(nil)
