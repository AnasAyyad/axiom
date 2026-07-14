package runtimecore

import "sync"

// RecoveryStage is one ordered shadow-startup recovery gate.
type RecoveryStage string

// RecoveryStages is the closed, safety-ordered recovery sequence. Completing
// the sequence establishes administrative readiness only; it never activates
// strategy execution.
var recoveryStages = [...]RecoveryStage{
	"database_prerequisites",
	"fenced_ownership",
	"build_safety_manifest",
	"configuration_graph",
	"schema_and_durability",
	"checkpoint_and_cursor",
	"protected_state",
	"committed_event_replay",
	"journal_and_projections",
	"simulator_reconciliation",
	"recorder_segments",
	"public_market_state",
	"operational_invariants",
	"administrative_readiness",
}

// RecoverySnapshot is immutable measured recovery progress.
type RecoverySnapshot struct {
	Completed int
	Total     int
	Elapsed   LogicalTime
	Ready     bool
}

// RecoveryGate enforces the documented order while the execution gate remains
// fail closed. Durable implementations complete stages only after their own
// evidence has committed.
type RecoveryGate struct {
	mutex     sync.Mutex
	clock     Clock
	gate      *SafetyGate
	startedAt LogicalTime
	completed int
}

// NewRecoveryGate begins locked recovery at the injected authoritative clock.
func NewRecoveryGate(clock Clock, gate *SafetyGate) (*RecoveryGate, error) {
	if clock == nil || gate == nil || clock.Now() == 0 {
		return nil, runtimeError("invalid_recovery", "configuration")
	}
	gate.Lock("startup_recovery")
	return &RecoveryGate{clock: clock, gate: gate, startedAt: clock.Now()}, nil
}

// Complete accepts only the next documented stage.
func (recovery *RecoveryGate) Complete(stage RecoveryStage) error {
	recovery.mutex.Lock()
	defer recovery.mutex.Unlock()
	if recovery.completed >= len(recoveryStages) || recoveryStages[recovery.completed] != stage {
		return runtimeError("recovery_stage_rejected", string(stage))
	}
	recovery.completed++
	return nil
}

// Snapshot reports readiness and elapsed logical time without changing engine
// acceptance. A locked engine therefore cannot auto-activate after recovery.
func (recovery *RecoveryGate) Snapshot() RecoverySnapshot {
	recovery.mutex.Lock()
	defer recovery.mutex.Unlock()
	now := recovery.clock.Now()
	elapsed := LogicalTime(0)
	if now >= recovery.startedAt {
		elapsed = now - recovery.startedAt
	}
	return RecoverySnapshot{
		Completed: recovery.completed,
		Total:     len(recoveryStages),
		Elapsed:   elapsed,
		Ready:     recovery.completed == len(recoveryStages),
	}
}

// RecoverySequence returns a defensive copy of the closed gate order.
func RecoverySequence() []RecoveryStage {
	stages := make([]RecoveryStage, len(recoveryStages))
	copy(stages, recoveryStages[:])
	return stages
}
