package risk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/portfolio"
)

// ObservationProvider supplies one complete current immutable risk view.
type ObservationProvider interface {
	Current() (Observations, []Policy, time.Time, error)
}

// PipelineEngine adapts real A9 policy to the shared A8 pipeline approval token.
type PipelineEngine struct {
	engine   *Engine
	vault    *portfolio.ApprovalVault
	registry portfolio.AssetRegistry
	inputs   ObservationProvider
}

// NewPipelineEngine constructs the real central-risk pipeline adapter.
func NewPipelineEngine(
	engine *Engine,
	vault *portfolio.ApprovalVault,
	registry portfolio.AssetRegistry,
	inputs ObservationProvider,
) (*PipelineEngine, error) {
	if engine == nil || vault == nil || registry == nil || inputs == nil {
		return nil, riskError("pipeline_risk_invalid")
	}
	return &PipelineEngine{engine: engine, vault: vault, registry: registry, inputs: inputs}, nil
}

// Approve evaluates central policy and issues only a vault-backed opaque token.
func (adapter *PipelineEngine) Approve(
	_ context.Context,
	allocated backtest.AllocatedIntent,
) (execution.ApprovedIntent, error) {
	var allocation portfolio.Allocation
	if allocated.Ordinal == 0 || json.Unmarshal(allocated.Payload, &allocation) != nil {
		return execution.ApprovedIntent{}, riskError("allocated_intent_invalid")
	}
	observations, policies, evaluatedAt, err := adapter.inputs.Current()
	if err != nil {
		return execution.ApprovedIntent{}, riskError("risk_inputs_unavailable")
	}
	decision, err := adapter.engine.Evaluate(Request{Intent: IntentEntry, Policies: policies,
		Observations: observations, EvaluatedAt: evaluatedAt})
	if err != nil {
		return execution.ApprovedIntent{}, riskError("risk_evaluation_failed")
	}
	if decision.Action != ActionApprove {
		return execution.ApprovedIntent{}, riskError(decision.ReasonCode)
	}
	return adapter.issue(allocation, decision)
}

func (adapter *PipelineEngine) issue(
	allocation portfolio.Allocation,
	decision Decision,
) (execution.ApprovedIntent, error) {
	candidate := allocation.Candidate
	assets := make([]portfolio.AssetEligibility, 0, 2)
	for _, item := range []struct {
		asset   domain.AssetSymbol
		version uint64
	}{{candidate.Instrument.Base, candidate.BaseEligibility}, {candidate.Instrument.Quote, candidate.QuoteEligibility}} {
		current, exists := adapter.registry.Current(item.asset)
		if !exists || current.Status != domain.AssetApproved || current.Version != item.version {
			return execution.ApprovedIntent{}, riskError("asset_not_approved")
		}
		assets = append(assets, current)
	}
	decisionID, err := domain.NewDecisionID(candidate.ID)
	if err != nil {
		return execution.ApprovedIntent{}, riskError("decision_identity_invalid")
	}
	policyHash := policyDecisionHash(decision)
	return adapter.vault.Issue(portfolio.ApprovalRecord{DecisionID: decisionID, Assets: assets, PolicyHash: policyHash})
}

func policyDecisionHash(decision Decision) string {
	encoded, _ := json.Marshal(decision)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

var _ backtest.RiskEngine = (*PipelineEngine)(nil)
