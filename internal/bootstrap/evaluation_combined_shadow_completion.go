package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"axiom/internal/backtest"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

func (engine *evaluationCombinedShadowEngine) completeSession(ctx context.Context, campaignID string) error {
	now := engine.clock.Now().UTC
	tx, err := engine.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	values, err := loadCompletedShadowMembers(ctx, tx, campaignID)
	if err != nil {
		return err
	}
	if err = completeShadowMembers(ctx, tx, campaignID, values, now); err != nil {
		return err
	}
	if err = completeShadowSessionRecord(ctx, tx, campaignID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func loadCompletedShadowMembers(ctx context.Context, tx pgx.Tx,
	campaignID string) ([]evaluationCompletedShadowMember, error) {
	rows, err := tx.Query(ctx, `SELECT checkpoint.member_id,member.strategy_id,checkpoint.metrics_payload
FROM evaluation_shadow_member_checkpoints checkpoint
JOIN evaluation_campaign_members member ON member.campaign_id=checkpoint.campaign_id AND member.id=checkpoint.member_id
	WHERE checkpoint.campaign_id=$1 AND checkpoint.state='RUNNING' ORDER BY checkpoint.member_id FOR UPDATE`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]evaluationCompletedShadowMember, 0, 4)
	for rows.Next() {
		var id, strategy string
		var payload []byte
		if err = rows.Scan(&id, &strategy, &payload); err != nil {
			return nil, err
		}
		verdict, reason := combinedShadowVerdict(evaluation.Strategy(strategy), payload)
		values = append(values, evaluationCompletedShadowMember{id: id, strategy: strategy,
			verdict: string(verdict), reason: string(reason)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func completeShadowMembers(ctx context.Context, tx pgx.Tx, campaignID string,
	values []evaluationCompletedShadowMember, now time.Time) error {
	for _, value := range values {
		if _, err := tx.Exec(ctx, `UPDATE evaluation_shadow_member_checkpoints SET state='SUCCEEDED',updated_at=$3
WHERE campaign_id=$1 AND member_id=$2 AND state='RUNNING'`, campaignID, value.id, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE evaluation_campaign_members SET state='SUCCEEDED',verdict=$3,
reason_code=$4,updated_at=$5 WHERE campaign_id=$1 AND id=$2 AND mode='shadow'`, campaignID, value.id,
			value.verdict, value.reason, now); err != nil {
			return err
		}
	}
	return nil
}

func completeShadowSessionRecord(ctx context.Context, tx pgx.Tx, campaignID string, now time.Time) error {
	var valid int64
	if err := tx.QueryRow(ctx, `SELECT valid_shadow_seconds FROM evaluation_campaigns WHERE id=$1`, campaignID).
		Scan(&valid); err != nil {
		return err
	}
	if valid < int64(evaluation.RequiredShadowValidTime/time.Second) {
		return fmt.Errorf("evaluation_shadow_valid_time_incomplete")
	}
	tag, err := tx.Exec(ctx, `UPDATE evaluation_shadow_sessions SET state='COMPLETED',valid_seconds=$2,
completed_at=$3,updated_at=$3 WHERE campaign_id=$1 AND state='RUNNING'`, campaignID, valid, now)
	if err != nil || tag.RowsAffected() != 1 {
		return fmt.Errorf("evaluation_shadow_completion_conflict")
	}
	return nil
}

func combinedShadowVerdict(strategy evaluation.Strategy, payload []byte) (evaluation.Verdict, evaluation.ReasonCode) {
	var metrics backtest.Metrics
	if json.Unmarshal(payload, &metrics) != nil {
		return evaluation.VerdictBlocked, evaluation.ReasonAccountingFailed
	}
	drawdown, drawdownOK := new(big.Rat).SetString(metrics.MaximumDrawdown)
	result, resultOK := new(big.Rat).SetString(metrics.TotalNetReturn)
	if !drawdownOK || !resultOK || drawdown.Sign() < 0 {
		return evaluation.VerdictBlocked, evaluation.ReasonAccountingFailed
	}
	if drawdown.Cmp(big.NewRat(3, 100)) > 0 {
		return evaluation.VerdictReject, "SHADOW_DRAWDOWN_LIMIT_EXCEEDED"
	}
	if metrics.Trades < 20 {
		return evaluation.VerdictImprove, "SHADOW_SAMPLE_INSUFFICIENT"
	}
	if result.Sign() <= 0 {
		return evaluation.VerdictReject, "SHADOW_NET_RESULT_NOT_POSITIVE"
	}
	_ = strategy
	return evaluation.VerdictContinue, "SHADOW_GATE_PASSED"
}

func (engine *evaluationCombinedShadowEngine) shadowHealth(ctx context.Context,
	campaignID string) (int64, bool, time.Time, error) {
	var valid int64
	var observed *time.Time
	var eligible, persistence bool
	err := engine.pool.QueryRow(ctx, `SELECT campaign.valid_shadow_seconds,
  (SELECT observation.observed_at FROM evaluation_recorder_observations observation
   WHERE observation.campaign_id=campaign.id ORDER BY observation.ordinal DESC LIMIT 1),
  COALESCE((SELECT observation.all_collectors_eligible FROM evaluation_recorder_observations observation
   WHERE observation.campaign_id=campaign.id ORDER BY observation.ordinal DESC LIMIT 1),false),
  COALESCE((SELECT observation.persistence_healthy FROM evaluation_recorder_observations observation
   WHERE observation.campaign_id=campaign.id ORDER BY observation.ordinal DESC LIMIT 1),false)
FROM evaluation_campaigns campaign WHERE campaign.id=$1`, campaignID).
		Scan(&valid, &observed, &eligible, &persistence)
	if err != nil {
		return 0, false, time.Time{}, err
	}
	var at time.Time
	if observed != nil {
		at = observed.UTC()
	}
	return valid, eligible && persistence, at, nil
}

func (engine *evaluationCombinedShadowEngine) blockSession(ctx context.Context, campaignID string,
	reason evaluation.ReasonCode, summary string) (evaluation.StageProgress, error) {
	if reason == "" {
		reason = evaluation.ReasonSafetyFailed
	}
	now := engine.clock.Now().UTC
	_, err := engine.pool.Exec(ctx, `UPDATE evaluation_shadow_sessions SET state='BLOCKED',reason_code=$2,
updated_at=$3 WHERE campaign_id=$1 AND state NOT IN ('COMPLETED','BLOCKED','CANCELED')`, campaignID,
		string(reason), now)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if err = engine.control.Block(ctx, campaignID, reason); err != nil {
		return evaluation.StageProgress{}, err
	}
	delete(engine.runtimes, campaignID)
	session, _, err := engine.readSession(ctx, campaignID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	return evaluation.StageProgress{State: evaluation.ProgressBlock, Reason: reason, Summary: summary,
		Checkpoint: shadowSessionCheckpoint(session), LinkedResourceType: "combined_shadow",
		LinkedResourceID: campaignID}, nil
}

func (engine *evaluationCombinedShadowEngine) completeEmptyShadow(ctx context.Context, campaign evaluation.Campaign,
	session evaluationShadowSessionProjection) (evaluation.StageProgress, error) {
	rotation, err := engine.control.Rotation(ctx, campaign.ID)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	if rotation.State == "ACTIVE" || rotation.State == "PAUSED" {
		if rotation.State == "PAUSED" {
			if err = engine.control.ResumeCampaign(ctx, campaign.ID); err != nil {
				return evaluation.StageProgress{}, err
			}
		}
		if err = engine.control.RequestCompletion(ctx, campaign.ID); err != nil {
			return evaluation.StageProgress{}, err
		}
		return evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary:    "No strategy passed the pre-shadow gate; the campaign recorder is finalizing safely.",
			Checkpoint: shadowSessionCheckpoint(session)}, nil
	}
	if rotation.State != "COMPLETED" {
		return evaluation.StageProgress{State: evaluation.ProgressWaiting,
			Summary:    "No strategy passed the pre-shadow gate; final recorder evidence is pending.",
			Checkpoint: shadowSessionCheckpoint(session)}, nil
	}
	now := engine.clock.Now().UTC
	_, err = engine.pool.Exec(ctx, `UPDATE evaluation_shadow_sessions SET state='COMPLETED',
completed_at=$2,updated_at=$2 WHERE campaign_id=$1 AND state='RUNNING'`, campaign.ID, now)
	if err != nil {
		return evaluation.StageProgress{}, err
	}
	session, _, err = engine.readSession(ctx, campaign.ID)
	return evaluation.StageProgress{State: evaluation.ProgressComplete,
		Summary:    "Combined shadow was skipped because no candidate passed the pre-shadow gate.",
		Checkpoint: shadowSessionCheckpoint(session)}, err
}

func shadowDecisionMaterial(result backtest.EventResult) bool {
	if len(result.Decision) == 0 {
		return false
	}
	var decision map[string]any
	if json.Unmarshal(result.Decision, &decision) != nil {
		return true
	}
	action, _ := decision["action"].(string)
	return action != "" && action != "observation_only"
}

func sharedShadowFailure(err error) bool {
	if err == nil {
		return false
	}
	value := err.Error()
	for _, marker := range []string{"evaluation_book_", "evaluation_shadow_", "dataset_", "recorder:"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func shadowSessionCheckpoint(value evaluationShadowSessionProjection) []byte {
	payload, _ := json.Marshal(map[string]any{"state": value.state, "recorder_session_id": value.sessionID,
		"start_ordinal": value.startOrdinal, "last_processed_ordinal": value.lastOrdinal,
		"valid_seconds": value.validSeconds, "reason_code": value.reason,
		"started_at": value.startedAt.UTC(), "updated_at": value.updatedAt.UTC(),
		"simulation_only": true})
	return payload
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
