package runtimecore

import (
	"context"

	"axiom/internal/domain"
)

// Stage is one fixed in-process V1A hot-path boundary.
type Stage uint8

// Exact V1A hot-path stage order.
const (
	StagePublicEvent Stage = iota + 1
	StageRawRecording
	StageValidation
	StageMarketView
	StageTrend
	StageAllocation
	StageRisk
	StageSimulation
	StageDurableOrderJournal
	StageOutbox
	StageMetricsAudit
	StageAPIStream
)

// Causation is immutable identity carried across every hot-path stage.
type Causation struct {
	EventID           domain.EventID
	ConfigurationHash string
	ViewVectorHash    string
	Stage             Stage
}

// StageHandler reduces one stage in process without an HTTP or RPC boundary.
type StageHandler func(context.Context, Causation) (Causation, error)

// Pipeline enforces the documented hot-path order for supplied phase handlers.
type Pipeline struct{ handlers []StageHandler }

// NewPipeline requires exactly one handler for every ordered stage.
func NewPipeline(handlers []StageHandler) (*Pipeline, error) {
	if len(handlers) != int(StageAPIStream) {
		return nil, runtimeError("pipeline_rejected", "stage_count")
	}
	for _, handler := range handlers {
		if handler == nil {
			return nil, runtimeError("pipeline_rejected", "nil_stage")
		}
	}
	return &Pipeline{handlers: append([]StageHandler(nil), handlers...)}, nil
}

// Process runs every stage synchronously and verifies causation continuity.
func (pipeline *Pipeline) Process(ctx context.Context, causation Causation) (Causation, error) {
	if causation.EventID.Value() == "" || !validDigest(causation.ConfigurationHash) || !validDigest(causation.ViewVectorHash) {
		return Causation{}, runtimeError("pipeline_rejected", "causation")
	}
	current := causation
	for index, handler := range pipeline.handlers {
		expected := Stage(index + 1)
		current.Stage = expected
		next, err := handler(ctx, current)
		if err != nil {
			return Causation{}, err
		}
		if !sameCausationIdentity(current, next) || next.Stage != expected {
			return Causation{}, runtimeError("pipeline_causation_broken", "stage")
		}
		current = next
	}
	return current, nil
}

func sameCausationIdentity(left, right Causation) bool {
	return left.EventID == right.EventID && left.ConfigurationHash == right.ConfigurationHash &&
		left.ViewVectorHash == right.ViewVectorHash
}
