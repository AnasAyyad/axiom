package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Preset is a server-owned evaluation recipe. Browser requests are restricted
// to this semantic name; all immutable inputs are resolved by the server.
type Preset string

// BalancedFullV1 is the sole owner-startable server-owned campaign preset.
const BalancedFullV1 Preset = "balanced_full_v1"

// EvaluationRecorderSessionID is the deterministic campaign-bound recorder
// session base used by both public exchanges.
func EvaluationRecorderSessionID(campaignID string) string {
	digest := sha256.Sum256([]byte("evaluation-recorder:" + campaignID))
	return "evalrec-" + hex.EncodeToString(digest[:8])
}

// State is a durable campaign lifecycle state.
type State string

// Campaign lifecycle states are durable and owner-visible.
const (
	StatePending           State = "PENDING"
	StateRunning           State = "RUNNING"
	StatePausedRecoverable State = "PAUSED_RECOVERABLE"
	StateCompleted         State = "COMPLETED"
	StatePartial           State = "PARTIAL"
	StateBlocked           State = "BLOCKED"
	StateCanceled          State = "CANCELED"
)

// Stage is one ordered, checkpointed part of a complete evaluation.
type Stage string

// Campaign stages execute in this fixed automatic order.
const (
	StageHistoricalImport   Stage = "HISTORICAL_IMPORT"
	StageExistingDataAudit  Stage = "EXISTING_DATA_AUDIT"
	StageRecorderRotation   Stage = "RECORDER_ROTATION"
	StageRecorderQualify    Stage = "RECORDER_QUALIFICATION"
	StageBacktestMatrix     Stage = "BACKTEST_MATRIX"
	StageReplayMatrix       Stage = "REPLAY_MATRIX"
	StageCandidateSelection Stage = "CANDIDATE_SELECTION"
	StageCombinedShadow     Stage = "COMBINED_SHADOW"
	StageFinalReport        Stage = "FINAL_REPORT"
)

// Stages returns the only valid automatic stage order.
func Stages() []Stage {
	return []Stage{StageHistoricalImport, StageExistingDataAudit, StageRecorderRotation,
		StageRecorderQualify, StageBacktestMatrix, StageReplayMatrix, StageCandidateSelection,
		StageCombinedShadow, StageFinalReport}
}

// ReasonCode is a stable, owner-visible reason that contains no secret or raw
// exchange payload.
type ReasonCode string

// Stable campaign reason codes expose bounded failure categories.
const (
	ReasonStorageInsufficient ReasonCode = "STORAGE_INSUFFICIENT"
	ReasonDataUnavailable     ReasonCode = "DATA_UNAVAILABLE"
	ReasonDataCorrupt         ReasonCode = "DATA_CORRUPT"
	ReasonAccountingFailed    ReasonCode = "ACCOUNTING_FAILED"
	ReasonSafetyFailed        ReasonCode = "SAFETY_FAILED"
	ReasonPersistenceFailed   ReasonCode = "PERSISTENCE_FAILED"
	ReasonCanceled            ReasonCode = "CANCELED_BY_OWNER"
)

// Campaign is the state-machine projection persisted by the orchestration
// store. Timestamps are UTC and duration accounting uses valid duration rather
// than wall time, so recoverable feed interruptions do not qualify a campaign.
type Campaign struct {
	ID              string
	Preset          Preset
	State           State
	CurrentStage    Stage
	CompletedStages []Stage
	ValidRecording  time.Duration
	ValidShadow     time.Duration
	Reason          ReasonCode
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Revision        int64
}

// Valid reports whether a campaign is internally safe to persist.
func (value Campaign) Valid() bool {
	if value.ID == "" || value.Preset != BalancedFullV1 || !validState(value.State) || value.Revision < 0 {
		return false
	}
	if value.State == StatePending || value.State == StateCompleted || value.State == StatePartial ||
		value.State == StateBlocked || value.State == StateCanceled {
		return value.ValidRecording >= 0 && value.ValidShadow >= 0
	}
	return validStage(value.CurrentStage) && value.ValidRecording >= 0 && value.ValidShadow >= 0
}

func validState(value State) bool {
	switch value {
	case StatePending, StateRunning, StatePausedRecoverable, StateCompleted, StatePartial, StateBlocked, StateCanceled:
		return true
	default:
		return false
	}
}

func validStage(value Stage) bool {
	for _, stage := range Stages() {
		if value == stage {
			return true
		}
	}
	return false
}
