package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

func persistEvaluationOutcomeReport(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	priorStage evaluation.Stage, outcome evaluation.Outcome) error {
	if outcome.Kind == evaluation.OutcomePartialReported {
		if outcome.Report == nil || insertEvaluationReport(ctx, tx, campaign.ID, *outcome.Report) != nil {
			return fmt.Errorf("evaluation_partial_report_invalid")
		}
	}
	if outcome.Kind == evaluation.OutcomeCompleted && priorStage == evaluation.StageFinalReport {
		if outcome.Report == nil || insertEvaluationReport(ctx, tx, campaign.ID, *outcome.Report) != nil {
			return fmt.Errorf("evaluation_final_report_invalid")
		}
	}
	return nil
}

func updateEvaluationStage(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	priorStage evaluation.Stage, outcome evaluation.Outcome, now time.Time, changed bool) error {
	if !changed {
		return nil
	}
	checkpoint, checkpointHash := nullableCheckpoint(outcome.Checkpoint)
	linkType, linkID := evaluationNullableText(outcome.LinkedResourceType), evaluationNullableText(outcome.LinkedResourceID)
	switch outcome.Kind {
	case evaluation.OutcomeStarted:
		_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='RUNNING',attempt=attempt+1,
		  started_at=COALESCE(started_at,$3),updated_at=$3 WHERE campaign_id=$1 AND stage=$2`,
			campaign.ID, string(evaluation.StageHistoricalImport), now)
		return err
	case evaluation.OutcomeWaiting:
		return updateEvaluationStageCheckpoint(ctx, tx, campaign, priorStage, outcome, now,
			linkType, linkID, checkpoint, checkpointHash)
	case evaluation.OutcomePaused:
		return updateEvaluationStageCheckpoint(ctx, tx, campaign, priorStage, outcome, now,
			linkType, linkID, checkpoint, checkpointHash)
	case evaluation.OutcomeResumed:
		_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='RUNNING',reason_code=NULL,
		  updated_at=$3 WHERE campaign_id=$1 AND stage=$2`, campaign.ID, string(priorStage), now)
		return err
	case evaluation.OutcomeBlocked:
		return updateEvaluationStageCheckpoint(ctx, tx, campaign, priorStage, outcome, now,
			linkType, linkID, checkpoint, checkpointHash)
	case evaluation.OutcomeCompleted:
		return completeEvaluationStage(ctx, tx, campaign, priorStage, now, linkType, linkID,
			checkpoint, checkpointHash)
	}
	return nil
}

func updateEvaluationStageCheckpoint(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	priorStage evaluation.Stage, outcome evaluation.Outcome, now time.Time,
	linkType, linkID, checkpoint, checkpointHash any) error {
	if priorStage == "" {
		return nil
	}
	state := "RUNNING"
	var reason any
	if outcome.Kind == evaluation.OutcomePaused {
		state, reason = "PAUSED_RECOVERABLE", string(outcome.Reason)
	} else if outcome.Kind == evaluation.OutcomeBlocked {
		state, reason = "BLOCKED", string(outcome.Reason)
	} else if campaign.State == evaluation.StatePausedRecoverable {
		state = "PAUSED_RECOVERABLE"
	}
	_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state=$3,reason_code=COALESCE($4,reason_code),
linked_resource_type=COALESCE($5,linked_resource_type),linked_resource_id=COALESCE($6,linked_resource_id),
checkpoint_payload=COALESCE($7,checkpoint_payload),checkpoint_hash=COALESCE($8,checkpoint_hash),
updated_at=$9 WHERE campaign_id=$1 AND stage=$2`, campaign.ID, string(priorStage), state, reason,
		linkType, linkID, checkpoint, checkpointHash, now)
	return err
}

func completeEvaluationStage(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	priorStage evaluation.Stage, now time.Time, linkType, linkID, checkpoint, checkpointHash any) error {
	if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='COMPLETED',reason_code=NULL,
linked_resource_type=COALESCE($3,linked_resource_type),linked_resource_id=COALESCE($4,linked_resource_id),
checkpoint_payload=COALESCE($5,checkpoint_payload),checkpoint_hash=COALESCE($6,checkpoint_hash),
completed_at=$7,updated_at=$7 WHERE campaign_id=$1 AND stage=$2`, campaign.ID, string(priorStage),
		linkType, linkID, checkpoint, checkpointHash, now); err != nil {
		return err
	}
	if campaign.CurrentStage == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE evaluation_campaign_stages SET state='RUNNING',attempt=attempt+1,
started_at=COALESCE(started_at,$3),updated_at=$3 WHERE campaign_id=$1 AND stage=$2`,
		campaign.ID, string(campaign.CurrentStage), now)
	return err
}

func recordEvaluationOutcome(ctx context.Context, tx pgx.Tx, campaign evaluation.Campaign,
	priorStage evaluation.Stage, outcome evaluation.Outcome, now time.Time) error {
	eventType := "stage_waiting"
	switch outcome.Kind {
	case evaluation.OutcomeStarted:
		eventType = "campaign_started"
	case evaluation.OutcomeCompleted:
		eventType = "stage_completed"
	case evaluation.OutcomePaused:
		eventType = "campaign_paused"
	case evaluation.OutcomeResumed:
		eventType = "campaign_resumed"
	case evaluation.OutcomeBlocked:
		eventType = "campaign_blocked"
	case evaluation.OutcomePartialReported:
		eventType = "partial_report_created"
	}
	_, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_events(campaign_id,ordinal,event_type,
	  stage,reason_code,summary,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, campaign.ID,
		campaign.Revision, eventType, nullableStage(priorStage), nullableReason(outcome.Reason),
		outcome.Summary, now)
	return err
}

func insertEvaluationReport(ctx context.Context, tx pgx.Tx, campaignID string, report evaluation.Report) error {
	if !report.Valid() {
		return fmt.Errorf("evaluation_report_invalid")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO evaluation_campaign_reports(campaign_id,state,verdict,reason_code,
	  summary,report_hash,canonical_payload,generated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
	  ON CONFLICT (campaign_id) DO NOTHING`, campaignID, report.State, string(report.Verdict),
		nullableReason(report.Reason), report.Summary, report.Hash[:], report.CanonicalPayload, report.GeneratedAt)
	if err != nil || tag.RowsAffected() == 1 {
		return err
	}
	var state, verdict, summary string
	var reason *string
	var hash, payload []byte
	var generatedAt time.Time
	if err = tx.QueryRow(ctx, `SELECT state,verdict,reason_code,summary,report_hash,canonical_payload,
	  generated_at FROM evaluation_campaign_reports WHERE campaign_id=$1`, campaignID).Scan(&state,
		&verdict, &reason, &summary, &hash, &payload, &generatedAt); err != nil {
		return err
	}
	wantedReason := ""
	if report.Reason != "" {
		wantedReason = string(report.Reason)
	}
	actualReason := ""
	if reason != nil {
		actualReason = *reason
	}
	if state != report.State || verdict != string(report.Verdict) || actualReason != wantedReason ||
		summary != report.Summary || !bytes.Equal(hash, report.Hash[:]) || !bytes.Equal(payload, report.CanonicalPayload) ||
		!generatedAt.Equal(report.GeneratedAt) {
		return fmt.Errorf("evaluation_report_immutable_conflict")
	}
	return nil
}

func loadClaimedEvaluationCampaign(ctx context.Context, tx pgx.Tx, id, owner string,
	now time.Time) (evaluation.Campaign, int64, error) {
	row := tx.QueryRow(ctx, `SELECT id,preset,state,current_stage,completed_stages,
	  valid_recording_seconds,valid_shadow_seconds,reason_code,created_at,updated_at,revision,claim_epoch
	  FROM evaluation_campaigns WHERE id=$1 AND claim_owner=$2 AND claim_expires_at>$3 FOR UPDATE`, id, owner, now)
	claim, err := scanEvaluationClaim(row)
	return claim.Campaign, claim.ClaimEpoch, err
}

type evaluationClaimScanner interface{ Scan(...any) error }

func scanEvaluationClaim(row evaluationClaimScanner) (evaluation.Claim, error) {
	var id, preset, state string
	var stage, reason *string
	var completed []string
	var recordingSeconds, shadowSeconds, revision, epoch int64
	var created, updated time.Time
	err := row.Scan(&id, &preset, &state, &stage, &completed, &recordingSeconds, &shadowSeconds,
		&reason, &created, &updated, &revision, &epoch)
	if err != nil {
		return evaluation.Claim{}, err
	}
	value := evaluation.Campaign{ID: id, Preset: evaluation.Preset(preset), State: evaluation.State(state),
		ValidRecording: time.Duration(recordingSeconds) * time.Second,
		ValidShadow:    time.Duration(shadowSeconds) * time.Second, CreatedAt: created.UTC(),
		UpdatedAt: updated.UTC(), Revision: revision}
	if stage != nil {
		value.CurrentStage = evaluation.Stage(*stage)
	}
	if reason != nil {
		value.Reason = evaluation.ReasonCode(*reason)
	}
	for _, item := range completed {
		value.CompletedStages = append(value.CompletedStages, evaluation.Stage(item))
	}
	return evaluation.Claim{Campaign: value, ClaimEpoch: epoch}, nil
}

func nullableReason(value evaluation.ReasonCode) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func nullableStage(value evaluation.Stage) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func evaluationCheckpointHash(payload []byte) []byte {
	hash := sha256.Sum256(payload)
	return hash[:]
}

func nullableCheckpoint(payload []byte) (any, any) {
	if len(payload) == 0 {
		return nil, nil
	}
	return payload, evaluationCheckpointHash(payload)
}

func evaluationNullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

var _ evaluation.Store = (*EvaluationWorkerStore)(nil)
