package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"axiom/internal/domain"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const evaluationCampaignLease = 30 * time.Second

// EvaluationWorkerStore owns exclusively fenced campaign transitions.
type EvaluationWorkerStore struct {
	pool  *pgxpool.Pool
	owner string
	clock domain.Clock
}

// NewEvaluationWorkerStore constructs the durable campaign worker boundary.
func NewEvaluationWorkerStore(pool *pgxpool.Pool, owner string, clock domain.Clock) (*EvaluationWorkerStore, error) {
	if pool == nil || owner == "" || clock == nil {
		return nil, fmt.Errorf("evaluation_worker_store_dependencies_missing")
	}
	return &EvaluationWorkerStore{pool: pool, owner: owner, clock: clock}, nil
}

// Claim leases one active checkpoint or one terminal campaign still missing
// its required partial report.
func (store *EvaluationWorkerStore) Claim(ctx context.Context) (evaluation.Claim, bool, error) {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return evaluation.Claim{}, false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	row := tx.QueryRow(ctx, `SELECT campaign.id,campaign.preset,campaign.state,campaign.current_stage,
	  campaign.completed_stages,campaign.valid_recording_seconds,campaign.valid_shadow_seconds,
	  campaign.reason_code,campaign.created_at,campaign.updated_at,campaign.revision,campaign.claim_epoch
	FROM evaluation_campaigns campaign
	WHERE (campaign.state IN ('PENDING','RUNNING','PAUSED_RECOVERABLE')
	    OR (campaign.state IN ('BLOCKED','CANCELED') AND NOT EXISTS (
	      SELECT 1 FROM evaluation_campaign_reports report WHERE report.campaign_id=campaign.id)))
	  AND (campaign.current_stage IS NULL OR EXISTS (
	    SELECT 1 FROM evaluation_campaign_stages stage
	    WHERE stage.campaign_id=campaign.id AND stage.stage=campaign.current_stage
	      AND (stage.next_retry_at IS NULL OR stage.next_retry_at<=$1)))
	  AND (campaign.claim_owner IS NULL OR campaign.claim_expires_at<=$1)
	ORDER BY campaign.created_at,campaign.id
	FOR UPDATE OF campaign SKIP LOCKED LIMIT 1`, now)
	claim, err := scanEvaluationClaim(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return evaluation.Claim{}, false, tx.Commit(ctx)
	}
	if err != nil {
		return evaluation.Claim{}, false, err
	}
	claim.ClaimEpoch++
	tag, err := tx.Exec(ctx, `UPDATE evaluation_campaigns SET claim_owner=$2,claim_epoch=$3,
	  claim_expires_at=$4,updated_at=$1 WHERE id=$5`, now, store.owner, claim.ClaimEpoch,
		now.Add(evaluationCampaignLease), claim.Campaign.ID)
	if err != nil || tag.RowsAffected() != 1 {
		return evaluation.Claim{}, false, fmt.Errorf("evaluation_campaign_claim_failed")
	}
	claim.Campaign.UpdatedAt = now
	return claim, true, tx.Commit(ctx)
}

// Renew extends a still-valid fenced claim.
func (store *EvaluationWorkerStore) Renew(ctx context.Context, claim evaluation.Claim) error {
	now := store.clock.Now().UTC
	tag, err := store.pool.Exec(ctx, `UPDATE evaluation_campaigns SET claim_expires_at=$1,updated_at=$2
	  WHERE id=$3 AND claim_owner=$4 AND claim_epoch=$5 AND claim_expires_at>$2`,
		now.Add(evaluationCampaignLease), now, claim.Campaign.ID, store.owner, claim.ClaimEpoch)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_campaign_claim_lost")
	}
	return nil
}

// Apply atomically commits one stage outcome and releases the worker claim.
func (store *EvaluationWorkerStore) Apply(ctx context.Context, claim evaluation.Claim, outcome evaluation.Outcome) error {
	now := store.clock.Now().UTC
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	current, epoch, err := loadClaimedEvaluationCampaign(ctx, tx, claim.Campaign.ID, store.owner, now)
	if err != nil || epoch != claim.ClaimEpoch || current.Revision != claim.Campaign.Revision {
		return fmt.Errorf("evaluation_campaign_claim_conflict")
	}
	changed, err := applyEvaluationOutcome(ctx, tx, &current, outcome, now)
	if err != nil {
		return err
	}
	if err = updateEvaluationStage(ctx, tx, current, claim.Campaign.CurrentStage, outcome, now, changed); err != nil {
		return err
	}
	completed := make([]string, len(current.CompletedStages))
	for index, stage := range current.CompletedStages {
		completed[index] = string(stage)
	}
	var stage *string
	if current.CurrentStage != "" {
		value := string(current.CurrentStage)
		stage = &value
	}
	tag, err := tx.Exec(ctx, `UPDATE evaluation_campaigns SET state=$2,current_stage=$3,
	  completed_stages=$4,valid_recording_seconds=$5,valid_shadow_seconds=$6,reason_code=$7,
	  revision=$8,updated_at=$9,claim_owner=NULL,claim_expires_at=NULL WHERE id=$1
	  AND claim_owner=$10 AND claim_epoch=$11`, current.ID, string(current.State), stage, completed,
		int64(current.ValidRecording/time.Second), int64(current.ValidShadow/time.Second), nullableReason(current.Reason),
		current.Revision, now, store.owner, claim.ClaimEpoch)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_campaign_apply_conflict")
	}
	return tx.Commit(ctx)
}

func applyEvaluationOutcome(ctx context.Context, tx pgx.Tx, campaign *evaluation.Campaign,
	outcome evaluation.Outcome, now time.Time) (bool, error) {
	campaign.ValidRecording += outcome.ValidRecordingDelta
	campaign.ValidShadow += outcome.ValidShadowDelta
	priorStage := campaign.CurrentStage
	changed := outcome.Kind != evaluation.OutcomeWaiting || len(outcome.Checkpoint) > 0 ||
		outcome.ValidRecordingDelta > 0 || outcome.ValidShadowDelta > 0
	if err := transitionEvaluationCampaign(campaign, outcome); err != nil {
		return false, err
	}
	if outcome.Kind == evaluation.OutcomeStarted {
		priorStage = campaign.CurrentStage
	}
	if err := persistEvaluationOutcomeReport(ctx, tx, *campaign, priorStage, outcome); err != nil {
		return false, err
	}
	if campaign.Revision <= 0 {
		return false, fmt.Errorf("evaluation_revision_invalid")
	}
	if !changed {
		return false, nil
	}
	return true, recordEvaluationOutcome(ctx, tx, *campaign, priorStage, outcome, now)
}

func transitionEvaluationCampaign(campaign *evaluation.Campaign, outcome evaluation.Outcome) error {
	switch outcome.Kind {
	case evaluation.OutcomeStarted:
		if err := evaluation.Start(campaign); err != nil {
			return err
		}
	case evaluation.OutcomeWaiting:
		if len(outcome.Checkpoint) > 0 || outcome.ValidRecordingDelta > 0 || outcome.ValidShadowDelta > 0 {
			campaign.Revision++
		}
	case evaluation.OutcomeResumed:
		if err := evaluation.Resume(campaign); err != nil {
			return err
		}
	case evaluation.OutcomeRetryDeferred:
		if err := evaluation.DeferRecovery(campaign, outcome.Reason); err != nil {
			return err
		}
	case evaluation.OutcomePaused:
		if err := evaluation.PauseRecoverable(campaign, outcome.Reason); err != nil {
			return err
		}
	case evaluation.OutcomeBlocked:
		if campaign.State == evaluation.StatePending {
			campaign.State, campaign.Reason, campaign.Revision = evaluation.StateBlocked, outcome.Reason, campaign.Revision+1
		} else if err := evaluation.Block(campaign, outcome.Reason); err != nil {
			return err
		}
		campaign.CurrentStage = ""
	case evaluation.OutcomeCompleted:
		if err := evaluation.CompleteStage(campaign); err != nil {
			return err
		}
	case evaluation.OutcomePartialReported:
		if err := evaluation.MarkPartial(campaign); err != nil {
			return err
		}
	case evaluation.OutcomeKind(""):
		return fmt.Errorf("evaluation_outcome_missing")
	default:
		return fmt.Errorf("evaluation_outcome_invalid")
	}
	return nil
}
