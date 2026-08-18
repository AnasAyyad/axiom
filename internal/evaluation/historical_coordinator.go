package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/recorder"
)

// HistoricalImportTask is one exclusively leased import checkpoint.
type HistoricalImportTask struct {
	Spec       HistoricalImportSpec
	SessionID  string
	DatasetID  string
	CreatedAt  time.Time
	ClaimEpoch int64
}

// HistoricalImportSummary is bounded stage progress and retry posture.
type HistoricalImportSummary struct {
	Total         int        `json:"total"`
	Completed     int        `json:"completed"`
	Blocked       int        `json:"blocked"`
	RowCount      uint64     `json:"row_count"`
	ByteCount     int64      `json:"byte_count"`
	RetryAt       time.Time  `json:"retry_at,omitempty"`
	BlockedReason ReasonCode `json:"blocked_reason,omitempty"`
}

// HistoricalImportCompletion links a verified filesystem manifest to its
// registered immutable dataset identity.
type HistoricalImportCompletion struct {
	Manifest            recorder.DatasetManifest
	RegisteredDatasetID string
}

// HistoricalImportTaskStore fences checkpoint and retry updates separately
// from immutable filesystem evidence.
type HistoricalImportTaskStore interface {
	HistoricalImportSummary(context.Context, string) (HistoricalImportSummary, error)
	ClaimHistoricalImport(context.Context, string) (HistoricalImportTask, bool, error)
	CommitHistoricalImport(context.Context, HistoricalImportTask, HistoricalImportProgress,
		*HistoricalImportCompletion) error
	FailHistoricalImport(context.Context, HistoricalImportTask, HistoricalImportError) (bool, error)
}

// HistoricalDatasetRegistrar makes a fully verified public-market import
// discoverable without qualifying it as a decision-input dataset.
type HistoricalDatasetRegistrar interface {
	Register(context.Context, recorder.DatasetManifest, string) (string, error)
}

// HistoricalCoordinator advances one bounded import page per campaign claim.
type HistoricalCoordinator struct {
	root         string
	sourceCommit string
	clock        domain.Clock
	importer     *HistoricalImporter
	tasks        HistoricalImportTaskStore
	segments     HistoricalSegmentRepository
	registrar    HistoricalDatasetRegistrar
}

// NewHistoricalCoordinator constructs the bounded official-candle import
// stage over durable task, segment, and dataset stores.
func NewHistoricalCoordinator(root, sourceCommit string, clock domain.Clock, importer *HistoricalImporter,
	tasks HistoricalImportTaskStore, segmentRepository HistoricalSegmentRepository,
	registrar HistoricalDatasetRegistrar) (*HistoricalCoordinator, error) {
	if root == "" || sourceCommit == "" || clock == nil || importer == nil || tasks == nil ||
		segmentRepository == nil || registrar == nil {
		return nil, fmt.Errorf("evaluation_historical_coordinator_dependencies_missing")
	}
	return &HistoricalCoordinator{root: root, sourceCommit: sourceCommit, clock: clock,
		importer: importer, tasks: tasks, segments: segmentRepository, registrar: registrar}, nil
}

// Advance handles recovery, one official page, optional final manifest
// registration, and a durable task checkpoint.
func (coordinator *HistoricalCoordinator) Advance(ctx context.Context,
	campaign Campaign) (StageProgress, error) {
	summary, err := coordinator.tasks.HistoricalImportSummary(ctx, campaign.ID)
	if err != nil {
		return StageProgress{}, err
	}
	if progress, terminal := coordinator.historicalSummaryState(campaign, summary); terminal {
		return progress, nil
	}
	task, found, err := coordinator.tasks.ClaimHistoricalImport(ctx, campaign.ID)
	if err != nil {
		return StageProgress{}, err
	}
	if !found {
		return coordinator.historicalWaitProgress(summary)
	}
	if progress, terminal, advanceErr := coordinator.advanceHistoricalTask(ctx, task); terminal || advanceErr != nil {
		return progress, advanceErr
	}
	summary, err = coordinator.tasks.HistoricalImportSummary(ctx, campaign.ID)
	if err != nil {
		return StageProgress{}, err
	}
	state, message := ProgressWaiting, "Historical candle import advanced one durable page."
	if summary.Completed == summary.Total {
		state, message = ProgressComplete, "Historical candle import is complete."
	}
	return historicalSummaryProgress(state, "", message, summary)
}

func (coordinator *HistoricalCoordinator) historicalSummaryState(campaign Campaign,
	summary HistoricalImportSummary) (StageProgress, bool) {
	if summary.Blocked > 0 {
		progress, _ := historicalSummaryProgress(ProgressBlock, summary.BlockedReason,
			"Historical candle import is blocked.", summary)
		return progress, true
	}
	if summary.Total == 0 {
		return StageProgress{State: ProgressBlock, Reason: ReasonDataUnavailable,
			Summary: "Historical import plan is missing."}, true
	}
	if summary.Completed == summary.Total {
		progress, _ := historicalSummaryProgress(ProgressComplete, "", "Historical candle import is complete.", summary)
		return progress, true
	}
	if campaign.State == StatePausedRecoverable {
		if !summary.RetryAt.IsZero() && coordinator.clock.Now().UTC.Before(summary.RetryAt) {
			progress, _ := historicalSummaryProgress(ProgressPause, ReasonDataUnavailable,
				"Official candle source retry is pending.", summary)
			progress.RetryAfter = summary.RetryAt.Sub(coordinator.clock.Now().UTC)
			return progress, true
		}
		progress, _ := historicalSummaryProgress(ProgressWaiting, "", "Official candle source is ready to retry.", summary)
		return progress, true
	}
	return StageProgress{}, false
}

func (coordinator *HistoricalCoordinator) historicalWaitProgress(
	summary HistoricalImportSummary) (StageProgress, error) {
	if !summary.RetryAt.IsZero() && coordinator.clock.Now().UTC.Before(summary.RetryAt) {
		progress, err := historicalSummaryProgress(ProgressPause, ReasonDataUnavailable,
			"Official candle source retry is pending.", summary)
		progress.RetryAfter = summary.RetryAt.Sub(coordinator.clock.Now().UTC)
		return progress, err
	}
	return historicalSummaryProgress(ProgressWaiting, "", "Historical import worker is waiting.", summary)
}

func (coordinator *HistoricalCoordinator) advanceHistoricalTask(ctx context.Context,
	task HistoricalImportTask) (StageProgress, bool, error) {
	progress, err := coordinator.importer.ImportPage(ctx, task.Spec)
	if err != nil {
		failure, classified := HistoricalFailure(err)
		if !classified {
			failure = HistoricalImportError{Reason: ReasonPersistenceFailed, Code: "historical_import_unknown",
				Recoverable: true, cause: err}
		}
		blocked, failErr := coordinator.tasks.FailHistoricalImport(ctx, task, failure)
		if failErr != nil {
			return StageProgress{}, true, failErr
		}
		if blocked {
			return StageProgress{State: ProgressBlock, Reason: failure.Reason,
				Summary: "Historical import stopped with preserved evidence."}, true, nil
		}
		return StageProgress{State: ProgressPause, Reason: failure.Reason,
			Summary: "Historical source is temporarily unavailable."}, true, nil
	}
	var completion *HistoricalImportCompletion
	if progress.Complete {
		completion, err = coordinator.completeTask(ctx, task)
		if err != nil {
			failure := HistoricalImportError{Reason: ReasonPersistenceFailed,
				Code: "historical_import_manifest_failed", Recoverable: true, cause: err}
			_, _ = coordinator.tasks.FailHistoricalImport(ctx, task, failure)
			return StageProgress{State: ProgressPause, Reason: ReasonPersistenceFailed,
				Summary: "Historical import manifest finalization will retry from the preserved page checkpoint."}, true, nil
		}
	}
	if err = coordinator.tasks.CommitHistoricalImport(ctx, task, progress, completion); err != nil {
		return StageProgress{}, true, err
	}
	return StageProgress{}, false, nil
}

func (coordinator *HistoricalCoordinator) completeTask(ctx context.Context,
	task HistoricalImportTask) (*HistoricalImportCompletion, error) {
	stored, err := coordinator.segments.HistoricalImportSegments(ctx, task.Spec.ID)
	if err != nil || len(stored) == 0 || len(stored)%2 != 0 {
		return nil, fmt.Errorf("evaluation_historical_segments_incomplete")
	}
	references := make([]recorder.SegmentReference, len(stored))
	for index, item := range stored {
		references[index] = recorder.SegmentReference{Kind: item.Kind, Manifest: item.Manifest}
	}
	manifest, err := recorder.WriteImportedDatasetManifest(coordinator.root, task.DatasetID, task.SessionID,
		task.Spec.Exchange, references, task.CreatedAt)
	if err != nil || recorder.VerifyManifestChain(coordinator.root, manifest) != nil {
		return nil, fmt.Errorf("evaluation_historical_manifest_invalid")
	}
	registeredID, err := coordinator.registrar.Register(ctx, manifest, coordinator.sourceCommit)
	if err != nil || registeredID == "" {
		return nil, fmt.Errorf("evaluation_historical_dataset_registration_failed")
	}
	return &HistoricalImportCompletion{Manifest: manifest, RegisteredDatasetID: registeredID}, nil
}

func historicalSummaryProgress(state ProgressState, reason ReasonCode, message string,
	summary HistoricalImportSummary) (StageProgress, error) {
	checkpoint, err := json.Marshal(summary)
	if err != nil {
		return StageProgress{}, err
	}
	return StageProgress{State: state, Reason: reason, Summary: message, Checkpoint: checkpoint}, nil
}
