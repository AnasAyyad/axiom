package reconciliation

import (
	"time"

	"axiom/internal/risk"
	runtimecore "axiom/internal/runtime"
)

// RecoveryEvidenceStore persists one proof for every ordered startup stage.
type RecoveryEvidenceStore interface {
	Append(runtimecore.RecoveryStage, string) error
}

// StartupRecovery composes the existing ordered gate with central risk state.
type StartupRecovery struct {
	gate     *runtimecore.RecoveryGate
	risk     *risk.Engine
	evidence RecoveryEvidenceStore
}

// NewStartupRecovery starts locked and retains the existing recovery sequence.
func NewStartupRecovery(
	gate *runtimecore.RecoveryGate,
	riskEngine *risk.Engine,
	evidence RecoveryEvidenceStore,
	at time.Time,
) (*StartupRecovery, error) {
	if gate == nil || riskEngine == nil || evidence == nil || riskEngine.BeginStartupRecovery(at) != nil {
		return nil, reconciliationError("startup_recovery_configuration_invalid")
	}
	return &StartupRecovery{gate: gate, risk: riskEngine, evidence: evidence}, nil
}

// Complete persists evidence before advancing exactly one documented stage.
func (recovery *StartupRecovery) Complete(stage runtimecore.RecoveryStage, evidenceHash string) error {
	if evidenceHash == "" || recovery.evidence.Append(stage, evidenceHash) != nil {
		return reconciliationError("recovery_evidence_failed")
	}
	if err := recovery.gate.Complete(stage); err != nil {
		return reconciliationError("recovery_stage_rejected")
	}
	return nil
}

// AdministrativeReady ends at PAUSED and never activates entry.
func (recovery *StartupRecovery) AdministrativeReady(at time.Time) error {
	if !recovery.gate.Snapshot().Ready || recovery.risk.CompleteStartupRecovery(at) != nil {
		return reconciliationError("administrative_readiness_rejected")
	}
	return nil
}
