package bootstrap

import (
	"sync/atomic"

	"axiom/internal/api/generated"
)

type lifecycleState struct{ value atomic.Uint32 }

const (
	stateStarting uint32 = iota
	stateReadyPaused
	stateStopping
)

func (state *lifecycleState) ready() { state.value.Store(stateReadyPaused) }

func (state *lifecycleState) stopping() { state.value.Store(stateStopping) }

func (state *lifecycleState) current() generated.SystemStatusLifecycleState {
	switch state.value.Load() {
	case stateReadyPaused:
		return generated.SystemStatusLifecycleStateREADYPAUSED
	case stateStopping:
		return generated.SystemStatusLifecycleStateSTOPPING
	default:
		return generated.SystemStatusLifecycleStateSTARTING
	}
}
