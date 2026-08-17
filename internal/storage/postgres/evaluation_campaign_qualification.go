package postgres

import (
	"time"

	"axiom/internal/evaluation"
)

const evaluationRecorderMaxUnresolvedObservations int64 = 3

func evaluationRecorderQualificationPolicy(now time.Time, qualification EvaluationRecorderQualification,
	checkpoint []byte) (evaluation.StageProgress, bool, bool) {
	if qualification.State == "BLOCKED" {
		reason := qualification.Reason
		if reason == "" {
			reason = evaluation.ReasonPersistenceFailed
		}
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: reason,
			Summary: "Fresh recorder qualification is blocked with preserved evidence.", Checkpoint: checkpoint}, true, false
	}
	if qualification.UnresolvedObservations >= evaluationRecorderMaxUnresolvedObservations {
		return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: evaluation.ReasonDataCorrupt,
			Summary: "Recorder resynchronization could not be validated after three consecutive observations; " +
				"the affected evidence remains preserved.", Checkpoint: checkpoint}, true, true
	}
	observationAge := now.Sub(qualification.LastObservedAt)
	healthy := qualification.LatestAllEligible && qualification.LatestPersistence &&
		!qualification.LastObservedAt.IsZero() && observationAge >= 0 &&
		observationAge <= evaluationRecorderMaxObservationInterval
	if qualification.UnresolvedObservations > 0 {
		return evaluation.StageProgress{State: evaluation.ProgressPause, Reason: evaluation.ReasonDataUnavailable,
			Summary: "A recorded feed gap invalidated the affected book interval; automatic snapshot " +
				"resynchronization is being validated before valid-time accounting resumes.",
			Checkpoint: checkpoint}, true, false
	}
	if qualification.ObservationCount == 0 || !healthy {
		return evaluation.StageProgress{State: evaluation.ProgressPause, Reason: evaluation.ReasonDataUnavailable,
			Summary:    "Valid-time clock is paused until all six public feeds, clocks, and persistence recover.",
			Checkpoint: checkpoint}, true, false
	}
	if qualification.ObservationCount > 1 && !qualification.LatestIntervalValid {
		return evaluation.StageProgress{State: evaluation.ProgressPause, Reason: evaluation.ReasonDataUnavailable,
			Summary: "Valid-time clock is paused until one complete healthy observation proves continuous " +
				"post-recovery recording.", Checkpoint: checkpoint}, true, false
	}
	if qualification.ValidSeconds < int64(evaluation.RequiredRecordingValidTime/time.Second) {
		return evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary:    "Fresh simultaneous Binance and Bybit evidence is accumulating valid time.",
			Checkpoint: checkpoint}, true, false
	}
	return evaluation.StageProgress{}, false, false
}
