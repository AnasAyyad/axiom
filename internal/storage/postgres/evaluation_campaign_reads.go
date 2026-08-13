package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"
	"axiom/internal/evaluation"

	"github.com/jackc/pgx/v5"
)

// EvaluationCampaignEvents returns the bounded durable timeline for one
// opaque campaign identifier.
func (store *OwnerConsoleStore) EvaluationCampaignEvents(ctx context.Context, id string) (generated.EvaluationCampaignEventPage, error) {
	if _, err := store.EvaluationCampaign(ctx, id); err != nil {
		return generated.EvaluationCampaignEventPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT ordinal,event_type,stage,reason_code,summary,occurred_at FROM evaluation_campaign_events WHERE campaign_id=$1 ORDER BY ordinal LIMIT 500`, id)
	if err != nil {
		return generated.EvaluationCampaignEventPage{}, err
	}
	defer rows.Close()
	page := generated.EvaluationCampaignEventPage{Items: make([]generated.EvaluationCampaignEvent, 0)}
	for rows.Next() {
		var ordinal int64
		var eventType string
		var stage, reason, summary *string
		var at generated.Timestamp
		if err = rows.Scan(&ordinal, &eventType, &stage, &reason, &summary, &at); err != nil {
			return generated.EvaluationCampaignEventPage{}, err
		}
		page.Items = append(page.Items, generated.EvaluationCampaignEvent{Ordinal: generated.Revision(strconv.FormatInt(ordinal, 10)), EventType: eventType, Stage: stage, ReasonCode: reason, Summary: summary, OccurredAt: at})
	}
	return page, rows.Err()
}

// EvaluationCampaignReport exposes an immutable report or an explicit
// not-ready state; it never substitutes a global report.
func (store *OwnerConsoleStore) EvaluationCampaignReport(ctx context.Context, id string) (generated.EvaluationCampaignReport, error) {
	if _, err := store.EvaluationCampaign(ctx, id); err != nil {
		return generated.EvaluationCampaignReport{}, err
	}
	var state string
	var verdict, reason, summary *string
	var hash []byte
	var at generated.Timestamp
	var canonical []byte
	err := store.pool.QueryRow(ctx, `SELECT state,verdict,reason_code,summary,report_hash,canonical_payload,generated_at FROM evaluation_campaign_reports WHERE campaign_id=$1`, id).Scan(&state, &verdict, &reason, &summary, &hash, &canonical, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.EvaluationCampaignReport{State: generated.EvaluationCampaignReportStateNotReady, GeneratedAt: generated.Timestamp(store.clock.Now().UTC)}, nil
	}
	if err != nil {
		return generated.EvaluationCampaignReport{}, err
	}
	hashText := fmtHex(hash)
	value := generated.EvaluationCampaignReport{State: generated.EvaluationCampaignReportState(state), ReasonCode: reason, Summary: summary, ReportHash: &hashText, GeneratedAt: at}
	var content map[string]interface{}
	if !json.Valid(canonical) || json.Unmarshal(canonical, &content) != nil {
		return generated.EvaluationCampaignReport{}, console.ErrUnavailable
	}
	value.Content = &content
	if verdict != nil {
		value.Verdict = ptr(generated.EvaluationCampaignReportVerdict(*verdict))
	}
	return value, nil
}

// CreateDataAudit creates one owner-authorized, idempotent standalone recording audit.
func (store *OwnerConsoleStore) CreateDataAudit(ctx context.Context, principal authentication.Principal, key string) (generated.DataAudit, error) {
	requestPayload := []byte(`{"action":"create_data_audit"}`)
	requestHash := sha256.Sum256(requestPayload)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return generated.DataAudit{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`,
		"axiom:data-audit:"+principal.UserID+":"+key); err != nil {
		return generated.DataAudit{}, err
	}
	var existingHash, response []byte
	if err = tx.QueryRow(ctx, `SELECT request_hash,response_payload FROM evaluation_campaign_commands
	  WHERE actor_id=$1 AND idempotency_key=$2`, principal.UserID, key).Scan(&existingHash, &response); err == nil {
		if !bytes.Equal(existingHash, requestHash[:]) {
			return generated.DataAudit{}, console.ErrIdempotencyConflict
		}
		var existing generated.DataAudit
		if json.Unmarshal(response, &existing) != nil {
			return generated.DataAudit{}, console.ErrUnavailable
		}
		return existing, tx.Commit(ctx)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return generated.DataAudit{}, err
	}
	now := store.clock.Now().UTC
	id, err := ownerConsoleIdentifier("data-audit")
	if err != nil {
		return generated.DataAudit{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_data_audits(id,state,baseline_at,created_at,updated_at)
	  VALUES($1,'PENDING',$2,$2,$2)`, id, now); err != nil {
		return generated.DataAudit{}, err
	}
	value := generated.DataAudit{Id: id, State: generated.DataAuditStatePENDING, CreatedAt: generated.Timestamp(now)}
	response, _ = json.Marshal(value)
	commandID, commandErr := ownerConsoleIdentifier("evaluation-command")
	if commandErr != nil {
		return generated.DataAudit{}, commandErr
	}
	if _, err = tx.Exec(ctx, `INSERT INTO evaluation_campaign_commands(id,actor_id,idempotency_key,
	  request_hash,target_id,state,response_payload,created_at) VALUES($1,$2,$3,$4,$5,'accepted',$6,$7)`,
		commandID, principal.UserID, key, requestHash[:], id, response, now); err != nil {
		return generated.DataAudit{}, err
	}
	return value, tx.Commit(ctx)
}

// DataAudit returns owner-visible progress for one standalone recording audit.
func (store *OwnerConsoleStore) DataAudit(ctx context.Context, id string) (generated.DataAudit, error) {
	var state string
	var reason *string
	var created generated.Timestamp
	var completed *generated.Timestamp
	err := store.pool.QueryRow(ctx, `SELECT state,reason_code,created_at,completed_at FROM evaluation_data_audits WHERE id=$1`, id).Scan(&state, &reason, &created, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.DataAudit{}, console.ErrNotFound
	}
	return generated.DataAudit{Id: id, State: generated.DataAuditState(state), ReasonCode: reason, CreatedAt: created, CompletedAt: completed}, err
}

const campaignSelect = `SELECT id,preset,state,current_stage,completed_stages,valid_recording_seconds,
valid_shadow_seconds,reason_code,revision,created_at,updated_at,campaign_recorded_bytes,
measured_bytes_per_hour,shadow_reserved_bytes,recording_last_valid_at,shadow_last_valid_at
FROM evaluation_campaigns`

type campaignScanner interface{ Scan(...any) error }

func scanCampaign(row campaignScanner) (generated.EvaluationCampaign, error) {
	var id, preset, state string
	var stage, reason *string
	var stages []string
	var recording, shadow, revision, recordedBytes int64
	var bytesPerHour, shadowReserve *int64
	var created, updated generated.Timestamp
	var recordingLastValid, shadowLastValid *generated.Timestamp
	err := row.Scan(&id, &preset, &state, &stage, &stages, &recording, &shadow, &reason, &revision,
		&created, &updated, &recordedBytes, &bytesPerHour, &shadowReserve, &recordingLastValid, &shadowLastValid)
	if err != nil {
		return generated.EvaluationCampaign{}, err
	}
	complete := make([]generated.EvaluationCampaignCompletedStages, len(stages))
	for i, s := range stages {
		complete[i] = generated.EvaluationCampaignCompletedStages(s)
	}
	var current *generated.EvaluationCampaignCurrentStage
	if stage != nil {
		v := generated.EvaluationCampaignCurrentStage(*stage)
		current = &v
	}
	limit := int64(evaluation.CampaignStorageLimitBytes)
	return generated.EvaluationCampaign{Id: id, Preset: generated.EvaluationCampaignPreset(preset),
		State: generated.EvaluationCampaignState(state), CurrentStage: current, CompletedStages: complete,
		ValidRecordingSeconds: &recording, ValidShadowSeconds: &shadow, ReasonCode: reason,
		Revision: generated.Revision(strconv.FormatInt(revision, 10)), CreatedAt: created, UpdatedAt: updated,
		RecordedBytes: &recordedBytes, RecordingLimitBytes: &limit, MeasuredBytesPerHour: bytesPerHour,
		ShadowReservedBytes: shadowReserve, RecordingLastValidAt: recordingLastValid,
		ShadowLastValidAt: shadowLastValid}, nil
}

func (store *OwnerConsoleStore) decorateEvaluationCampaign(campaign *generated.EvaluationCampaign) {
	now := store.clock.Now().UTC
	created := time.Time(campaign.CreatedAt)
	wall := int64(now.Sub(created).Seconds())
	if wall < 0 {
		wall = 0
	}
	campaign.WallTimeSeconds = &wall
	var remaining int64
	if campaign.CurrentStage != nil {
		switch string(*campaign.CurrentStage) {
		case string(evaluation.StageRecorderQualify):
			remaining = int64((72 * time.Hour) / time.Second)
			if campaign.ValidRecordingSeconds != nil {
				remaining -= *campaign.ValidRecordingSeconds
			}
		case string(evaluation.StageCombinedShadow):
			remaining = int64((7 * 24 * time.Hour) / time.Second)
			if campaign.ValidShadowSeconds != nil {
				remaining -= *campaign.ValidShadowSeconds
			}
		default:
			return
		}
		if remaining < 0 {
			remaining = 0
		}
		campaign.EstimatedRemainingSeconds = &remaining
	}
	if campaign.ReasonCode != nil {
		action := evaluationCampaignSuggestedAction(*campaign.ReasonCode)
		campaign.SuggestedAction = &action
	}
}

func evaluationCampaignSuggestedAction(reason string) string {
	switch reason {
	case "STORAGE_INSUFFICIENT":
		return "Provide additional storage or reduce the reviewed recording universe before starting a new campaign."
	case "DATA_UNAVAILABLE", "FEED_UNHEALTHY", "CLOCK_UNSAFE":
		return "Restore both public feeds and stable clocks; valid time will resume automatically when evidence is healthy."
	case "DATA_CORRUPT":
		return "Inspect the preserved data audit and recorder evidence; do not repair immutable evidence in place."
	case "CANCELED_BY_OWNER":
		return "Review the partial report. Start a new campaign only if the full evaluation should run again."
	default:
		return "Inspect the durable timeline and partial report, correct the shared prerequisite, then start a new campaign."
	}
}
