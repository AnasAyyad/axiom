package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"
	"axiom/internal/authentication"

	"github.com/jackc/pgx/v5"
)

const selectD1Activity = `
SELECT id,activity_revision,view_kind,source_type,source_id,source_revision,
       outcome,strategy_id,instrument_id,exchange_id,side,mode,reason_code,
       reason_version,reason_summary,reason_explanation,suggested_action,severity,
       unknown_reason,correlation_id,causation_id,occurred_at,details
FROM v1d_activity_explanations
WHERE ($1=0 OR activity_revision<$1)
  AND ($2='' OR view_kind=$2) AND ($3='' OR strategy_id=$3)
  AND ($4='' OR instrument_id=$4) AND ($5='' OR exchange_id=$5)
  AND ($6='' OR side=$6) AND ($7='' OR outcome=$7)
  AND ($8='' OR reason_code=$8) AND ($9='' OR mode=$9)
  AND ($10='' OR correlation_id=$10)
  AND ($11::timestamptz IS NULL OR occurred_at >= $11)
  AND ($12::timestamptz IS NULL OR occurred_at <= $12)
ORDER BY activity_revision DESC LIMIT $13`

// D1Activity reads the immutable allowlisted projection and safe reason view.
func (store *A11ConsoleStore) D1Activity(
	ctx context.Context, query console.D1ActivityQuery,
) (generated.ActivityPage, error) {
	position, err := store.cursor.Decode("v1d-d1:activity", query.Cursor)
	if err != nil {
		return generated.ActivityPage{}, err
	}
	var cursorRevision int64
	if position != "" {
		cursorRevision, err = strconv.ParseInt(position, 10, 64)
		if err != nil || cursorRevision <= 0 {
			return generated.ActivityPage{}, console.ErrInvalidRequest
		}
	}
	items, err := store.d1ActivityRows(ctx, query, cursorRevision)
	if err != nil {
		return generated.ActivityPage{}, err
	}
	hasMore := len(items) > query.PageSize
	if hasMore {
		items = items[:query.PageSize]
	}
	var next *string
	if hasMore && len(items) > 0 {
		value := store.cursor.Encode("v1d-d1:activity", items[len(items)-1].ActivityRevision)
		next = &value
	}
	var snapshot int64
	if err = store.pool.QueryRow(ctx, `SELECT coalesce(max(activity_revision),0) FROM v1d_activity_projection`).Scan(&snapshot); err != nil {
		return generated.ActivityPage{}, err
	}
	revision := strconv.FormatInt(snapshot, 10)
	return generated.ActivityPage{Items: items, HasMore: hasMore, NextCursor: next,
		Revision: revision, SnapshotRevision: revision}, nil
}

func (store *A11ConsoleStore) d1ActivityRows(
	ctx context.Context, query console.D1ActivityQuery, cursorRevision int64,
) ([]generated.ActivityResource, error) {
	rows, err := store.pool.Query(ctx, selectD1Activity, cursorRevision, query.View, query.Strategy,
		query.Instrument, query.Exchange, query.Side, query.Outcome, query.Reason,
		query.Mode, query.CorrelationID, query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.ActivityResource, 0, query.PageSize+1)
	for rows.Next() {
		item, scanErr := d1ScanActivity(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func d1ScanActivity(row pgx.Row) (generated.ActivityResource, error) {
	var item generated.ActivityResource
	var revision, reasonVersion int64
	var view, severity string
	var strategy, instrument, exchange, causation, mode, side *string
	var reasonCode, summary, explanation, action string
	var unknown bool
	var details []byte
	err := row.Scan(&item.Id, &revision, &view, &item.SourceType, &item.SourceId,
		&item.SourceRevision, &item.Outcome, &strategy, &instrument, &exchange, &side,
		&mode, &reasonCode, &reasonVersion, &summary, &explanation, &action, &severity,
		&unknown, &item.CorrelationId, &causation, &item.OccurredAt, &details)
	if err != nil {
		return generated.ActivityResource{}, err
	}
	item.ActivityRevision = strconv.FormatInt(revision, 10)
	item.View = generated.ActivityResourceView(view)
	item.StrategyId, item.InstrumentId, item.ExchangeId, item.CausationId = strategy, instrument, exchange, causation
	if side != nil {
		value := generated.ActivityResourceSide(*side)
		item.Side = &value
	}
	if mode != nil {
		value := generated.ActivityResourceMode(*mode)
		item.Mode = &value
	}
	item.Reason = generated.ReasonPresentation{Code: reasonCode, Version: strconv.FormatInt(reasonVersion, 10),
		Summary: summary, Explanation: explanation, SuggestedAction: action,
		Severity: generated.ReasonPresentationSeverity(severity), Unknown: unknown}
	item.Details = make(map[string]any)
	if err = json.Unmarshal(details, &item.Details); err != nil {
		return generated.ActivityResource{}, err
	}
	item.Links = d1ActivityLinks(item.Id, item.SourceType, item.SourceId, item.Details)
	return item, nil
}

// D1ActivityDetail returns one immutable projection row.
func (store *A11ConsoleStore) D1ActivityDetail(ctx context.Context, id string) (generated.ActivityResource, error) {
	rows, err := store.pool.Query(ctx, `
SELECT id,activity_revision,view_kind,source_type,source_id,source_revision,
       outcome,strategy_id,instrument_id,exchange_id,side,mode,reason_code,
       reason_version,reason_summary,reason_explanation,suggested_action,severity,
       unknown_reason,correlation_id,causation_id,occurred_at,details
FROM v1d_activity_explanations WHERE id=$1`, id)
	if err != nil {
		return generated.ActivityResource{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return generated.ActivityResource{}, console.ErrNotFound
	}
	return d1ScanActivity(rows)
}

// D1Export returns one unexpired artifact and appends download audit evidence.
func (store *A11ConsoleStore) D1Export(
	ctx context.Context,
	principal authentication.Principal,
	id string,
) (generated.ExportArtifact, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	artifact, err := d1ReadExport(ctx, tx, id)
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	if artifact.Deleted || !store.clock.Now().UTC.Before(artifact.ExpiresAt) {
		return generated.ExportArtifact{}, console.ErrCursorExpired
	}
	eventID, err := a11RandomID("artifact-access")
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	if _, err = tx.Exec(ctx, `
INSERT INTO v1d_artifact_access_events(
  id,artifact_id,actor_user_id,action,correlation_id,occurred_at
) VALUES ($1,$2,$3,'downloaded',$1,$4)`, eventID, id, principal.UserID, store.clock.Now().UTC); err != nil {
		return generated.ExportArtifact{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return generated.ExportArtifact{}, err
	}
	return artifact, nil
}

type d1ExportRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func d1ReadExport(ctx context.Context, source d1ExportRow, id string) (generated.ExportArtifact, error) {
	var artifact generated.ExportArtifact
	var format string
	var size int64
	var deletedAt *time.Time
	var held bool
	err := source.QueryRow(ctx, `
SELECT artifact.id,artifact.command_id,artifact.job_id,artifact.resource_type,artifact.resource_id,
       artifact.format,artifact.content_type,artifact.content,artifact.content_hash,
       artifact.size_bytes,artifact.redaction_version,artifact.created_at,artifact.expires_at,
       artifact.deleted_at,EXISTS(
         SELECT 1 FROM v1d_artifact_holds hold
         WHERE hold.artifact_id=artifact.id AND hold.released_at IS NULL
       )
FROM v1d_export_artifacts artifact WHERE artifact.id=$1`, id).Scan(
		&artifact.Id, &artifact.CommandId, &artifact.JobId, &artifact.ResourceType, &artifact.ResourceId,
		&format, &artifact.ContentType, &artifact.Content, &artifact.ContentHash, &size,
		&artifact.RedactionVersion, &artifact.CreatedAt, &artifact.ExpiresAt, &deletedAt, &held,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ExportArtifact{}, console.ErrNotFound
	}
	if err != nil {
		return generated.ExportArtifact{}, err
	}
	artifact.Format = generated.ExportArtifactFormat(format)
	artifact.SizeBytes = strconv.FormatInt(size, 10)
	artifact.Held, artifact.Deleted, artifact.Revision = held, deletedAt != nil, "1"
	return artifact, nil
}

func d1Resource(
	id, kind string,
	revision int64,
	state string,
	occurred *time.Time,
	attributes map[string]any,
	links map[string]string,
) generated.D1Resource {
	result := generated.D1Resource{Id: id, Kind: kind, Revision: strconv.FormatInt(revision, 10),
		State: state, CorrelationId: id, Attributes: attributes, Links: links}
	if occurred != nil {
		value := occurred.UTC()
		result.OccurredAt = &value
	}
	return result
}

func d1OptionalLink(prefix string, id *string) string {
	if id == nil {
		return ""
	}
	return prefix + *id
}

func d1QualificationLink(id string) string {
	if id == "c6-sandbox-72h" {
		return "/api/v1/sandbox/qualification"
	}
	return ""
}

func d1ActivityLinks(activityID, sourceType, sourceID string, details map[string]any) map[string]string {
	links := map[string]string{"self": "/api/v1/activity/" + activityID}
	switch sourceType {
	case "orders":
		links["order"] = "/api/v1/orders/" + sourceID
	case "incidents":
		links["incident"] = "/api/v1/incidents/" + sourceID
	case "jobs":
		links["command"] = "/api/v1/commands/" + sourceID
	}
	if value, ok := details["order_id"].(string); ok && value != "" {
		links["order"] = "/api/v1/orders/" + value
	}
	return links
}

var _ console.D1ReadService = (*A11ConsoleStore)(nil)
