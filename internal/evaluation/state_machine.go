package evaluation

import "errors"

// State-machine errors reject unsupported presets and unsafe transitions.
var (
	ErrInvalidTransition = errors.New("evaluation_invalid_transition")
	ErrInvalidPreset     = errors.New("evaluation_invalid_preset")
)

// Start begins only the first stage. All later stage progression is owned by
// the worker and must be checkpointed before a later stage begins.
func Start(value *Campaign) error {
	if value == nil || value.Preset != BalancedFullV1 || value.State != StatePending {
		return ErrInvalidTransition
	}
	value.State = StateRunning
	value.CurrentStage = StageHistoricalImport
	value.Revision++
	return nil
}

// CompleteStage records a successful stage and starts the next automatic
// stage. Final report completion is the only path to COMPLETED.
func CompleteStage(value *Campaign) error {
	if value == nil || value.State != StateRunning || !validStage(value.CurrentStage) {
		return ErrInvalidTransition
	}
	if !containsStage(value.CompletedStages, value.CurrentStage) {
		value.CompletedStages = append(value.CompletedStages, value.CurrentStage)
	}
	if value.CurrentStage == StageFinalReport {
		value.State = StateCompleted
		value.CurrentStage = ""
		value.Revision++
		return nil
	}
	value.CurrentStage = nextStage(value.CurrentStage)
	value.Revision++
	return nil
}

// PauseRecoverable retains the checkpoint and valid-time counters. Only a
// feed interruption or similarly recoverable dependency failure may use it.
func PauseRecoverable(value *Campaign, reason ReasonCode) error {
	if value == nil || value.State != StateRunning || reason == "" {
		return ErrInvalidTransition
	}
	value.State, value.Reason = StatePausedRecoverable, reason
	value.Revision++
	return nil
}

// Resume resumes the exact checkpoint; it never restarts or resets valid time.
func Resume(value *Campaign) error {
	if value == nil || value.State != StatePausedRecoverable {
		return ErrInvalidTransition
	}
	value.State, value.Reason = StateRunning, ""
	value.Revision++
	return nil
}

// Block terminates automatic work while retaining all evidence for a partial
// report. It is reserved for shared data, accounting, safety, persistence, or
// storage failures.
func Block(value *Campaign, reason ReasonCode) error {
	if value == nil || (value.State != StateRunning && value.State != StatePausedRecoverable) || reason == "" {
		return ErrInvalidTransition
	}
	value.State, value.Reason = StateBlocked, reason
	value.Revision++
	return nil
}

// Cancel is the sole owner control after start. A report worker must create a
// partial report after this transition; cancellation never deletes evidence.
func Cancel(value *Campaign) error {
	if value == nil || (value.State != StatePending && value.State != StateRunning && value.State != StatePausedRecoverable) {
		return ErrInvalidTransition
	}
	value.State, value.Reason = StateCanceled, ReasonCanceled
	value.Revision++
	return nil
}

// MarkPartial records that the final report is immutable but incomplete.
func MarkPartial(value *Campaign) error {
	if value == nil || (value.State != StateBlocked && value.State != StateCanceled) {
		return ErrInvalidTransition
	}
	value.State = StatePartial
	value.Revision++
	return nil
}

func nextStage(current Stage) Stage {
	stages := Stages()
	for index, stage := range stages {
		if stage == current && index+1 < len(stages) {
			return stages[index+1]
		}
	}
	return ""
}

func containsStage(values []Stage, wanted Stage) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
