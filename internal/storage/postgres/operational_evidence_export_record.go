package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"

	"github.com/jackc/pgx/v5"
)

func operationalEvidenceExportReportRecord(
	ctx context.Context, tx pgx.Tx, id string, expected int64, record map[string]string,
) error {
	var revision, sourceRevision int64
	var reportType, state, mode, confidence, valuation, models, maturity, source, hash string
	var content []byte
	var generatedAt *time.Time
	err := tx.QueryRow(ctx, `SELECT report_type,state,mode,confidence_tier,valuation_basis,
model_provenance::text,maturity,source_identity,source_revision,coalesce(content_hash,''),
generated_at,revision,coalesce(content,'{}'::jsonb) FROM owner_console_reports WHERE id=$1`, id).Scan(&reportType, &state,
		&mode, &confidence, &valuation, &models, &maturity, &source, &sourceRevision,
		&hash, &generatedAt, &revision, &content)
	if err != nil {
		return ownerControlNotFound(err)
	}
	if revision != expected {
		return console.ErrConflict
	}
	if state != "SUCCEEDED" || hash == "" || generatedAt == nil {
		return console.ErrPrecondition
	}
	record["report_type"], record["state"], record["mode"] = reportType, state, mode
	record["confidence_tier"], record["valuation_basis"] = confidence, valuation
	record["model_provenance"], record["maturity"] = models, maturity
	record["source_identity"] = source
	record["source_revision"] = strconv.FormatInt(sourceRevision, 10)
	record["report_revision"], record["content_hash"] = strconv.FormatInt(revision, 10), hash
	record["report_content"] = string(content)
	if generatedAt != nil {
		record["generated_at"] = generatedAt.UTC().Format(time.RFC3339Nano)
	}
	return nil
}

func operationalEvidenceExportIncidentEvidence(
	ctx context.Context, tx pgx.Tx, id string, record map[string]string,
) error {
	var alerts, activity, holds, events int64
	if err := tx.QueryRow(ctx, `SELECT
(SELECT count(*) FROM owner_console_incident_alert_links WHERE incident_id=$1),
(SELECT count(*) FROM owner_console_incident_activity_links WHERE incident_id=$1),
(SELECT count(*) FROM owner_console_artifact_holds WHERE reference_id=$1 AND hold_type='incident' AND released_at IS NULL),
(SELECT count(*) FROM owner_console_incident_events WHERE incident_id=$1)`, id).Scan(
		&alerts, &activity, &holds, &events); err != nil {
		return err
	}
	record["related_alert_count"] = strconv.FormatInt(alerts, 10)
	record["related_activity_count"] = strconv.FormatInt(activity, 10)
	record["active_evidence_hold_count"] = strconv.FormatInt(holds, 10)
	record["timeline_event_count"] = strconv.FormatInt(events, 10)
	var headHash, resolutionHash string
	if err := tx.QueryRow(ctx, `SELECT
coalesce((SELECT event_hash::text FROM owner_console_incident_events WHERE incident_id=$1
  ORDER BY incident_revision DESC LIMIT 1),''),
coalesce((SELECT evidence_hash::text FROM owner_console_incident_resolution_evidence
  WHERE incident_id=$1),'')`, id).Scan(&headHash, &resolutionHash); err != nil {
		return err
	}
	record["timeline_head_hash"], record["resolution_evidence_hash"] = headHash, resolutionHash
	var dataset, source string
	var first, last int64
	err := tx.QueryRow(ctx, `SELECT dataset_id,first_ordinal,last_ordinal,source_identity
FROM owner_console_incident_replay_inputs WHERE incident_id=$1`, id).Scan(&dataset, &first, &last, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	record["replay_dataset_id"], record["replay_source_identity"] = dataset, source
	record["replay_first_ordinal"] = strconv.FormatInt(first, 10)
	record["replay_last_ordinal"] = strconv.FormatInt(last, 10)
	return nil
}
