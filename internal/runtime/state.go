package runtimecore

import "sync"

// EngineState is the fail-closed execution acceptance state.
type EngineState string

// Engine states start paused and can become locked on an unresolved failure.
const (
	StatePaused EngineState = "PAUSED"
	StateActive EngineState = "ACTIVE"
	StateLocked EngineState = "LOCKED"
)

// SafetyGate serializes state transitions and never automatically activates.
type SafetyGate struct {
	mutex  sync.RWMutex
	state  EngineState
	reason string
}

// NewSafetyGate creates a paused gate.
func NewSafetyGate() *SafetyGate { return &SafetyGate{state: StatePaused, reason: "startup"} }

// State returns the current state and bounded reason code.
func (gate *SafetyGate) State() (EngineState, string) {
	gate.mutex.RLock()
	defer gate.mutex.RUnlock()
	return gate.state, gate.reason
}

// ManualActivate requires an explicit confirmation and cannot unlock a locked gate.
func (gate *SafetyGate) ManualActivate(confirmed bool) error {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if !confirmed || gate.state == StateLocked {
		return runtimeError("activation_rejected", "engine")
	}
	gate.state, gate.reason = StateActive, "manual"
	return nil
}

// Pause synchronously stops new decision acceptance.
func (gate *SafetyGate) Pause(reason string) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.state != StateLocked {
		gate.state, gate.reason = StatePaused, boundedReason(reason)
	}
}

// Lock enters a state that requires an external recovery decision.
func (gate *SafetyGate) Lock(reason string) {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	gate.state, gate.reason = StateLocked, boundedReason(reason)
}

// Accepting reports whether new execution plans may be considered.
func (gate *SafetyGate) Accepting() bool {
	gate.mutex.RLock()
	defer gate.mutex.RUnlock()
	return gate.state == StateActive
}

func boundedReason(reason string) string {
	if reason == "" || len(reason) > 64 {
		return "unspecified"
	}
	return reason
}
