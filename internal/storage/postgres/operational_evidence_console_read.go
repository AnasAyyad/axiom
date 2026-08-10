package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"axiom/internal/api/console"
	"axiom/internal/api/generated"

	"github.com/jackc/pgx/v5"
)

// OperationalEvidenceReport returns one report with immutable provenance and hash identity.
func (store *OwnerConsoleStore) OperationalEvidenceReport(ctx context.Context, id string) (generated.ReportResource, error) {
	var item generated.ReportResource
	var reportType, state, mode string
	var model []byte
	var sourceRevision, revision int64
	err := store.pool.QueryRow(ctx, `SELECT report.id,report.job_id,report.schedule_id,
report.report_type,report.state,report.mode,report.confidence_tier,report.valuation_basis,
report.model_provenance,report.maturity,report.source_identity,report.source_revision,
report.generated_at,report.content_hash,report.failure_code,report.created_at,report.revision
FROM owner_console_reports report WHERE report.id=$1`, id).Scan(
		&item.Id, &item.JobId, &item.ScheduleId, &reportType, &state, &mode,
		&item.Provenance.ConfidenceTier, &item.Provenance.ValuationBasis, &model,
		&item.Provenance.Maturity, &item.Provenance.SourceIdentity, &sourceRevision,
		&item.GeneratedAt, &item.ContentHash, &item.FailureCode, &item.CreatedAt, &revision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ReportResource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.ReportResource{}, err
	}
	if err = json.Unmarshal(model, &item.Provenance.ModelProvenance); err != nil {
		return generated.ReportResource{}, err
	}
	item.ReportType = generated.ReportResourceReportType(reportType)
	item.State = generated.ReportResourceState(state)
	item.Provenance.Mode = generated.ReportProvenanceMode(mode)
	item.Provenance.SourceRevision = strconv.FormatInt(sourceRevision, 10)
	item.Revision = strconv.FormatInt(revision, 10)
	return item, nil
}

// OperationalEvidenceReportSchedules returns deterministic bounded schedule projections.
func (store *OwnerConsoleStore) OperationalEvidenceReportSchedules(
	ctx context.Context, query console.OwnerControlListQuery,
) (generated.ReportSchedulePage, error) {
	if query.PageSize < 1 || query.PageSize > 200 {
		return generated.ReportSchedulePage{}, console.ErrInvalidRequest
	}
	position, err := store.cursor.Decode("owner_console-operational_evidence:report-schedules", query.Cursor)
	if err != nil {
		return generated.ReportSchedulePage{}, err
	}
	items, err := store.operationalEvidenceScheduleRows(ctx, position, query)
	if err != nil {
		return generated.ReportSchedulePage{}, err
	}
	page := generated.ReportSchedulePage{Items: items, Revision: "0"}
	if len(items) > query.PageSize {
		page.HasMore, page.Items = true, items[:query.PageSize]
		next := store.cursor.Encode("owner_console-operational_evidence:report-schedules", page.Items[len(page.Items)-1].Id)
		page.NextCursor = &next
	}
	if len(page.Items) > 0 {
		page.Revision = page.Items[0].Revision
	}
	return page, nil
}

func (store *OwnerConsoleStore) operationalEvidenceScheduleRows(
	ctx context.Context, position string, query console.OwnerControlListQuery,
) ([]generated.ReportSchedule, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,report_type,frequency,minute_utc,
hour_utc,weekday_utc,state,next_run_at,last_run_at,revision,created_at,updated_at
FROM owner_console_report_schedules WHERE ($1='' OR id<$1) AND ($2='' OR state=$2)
AND ($3::timestamptz IS NULL OR updated_at >= $3)
AND ($4::timestamptz IS NULL OR updated_at <= $4)
ORDER BY id DESC LIMIT $5`, position, query.Filters["state"], query.From, query.To, query.PageSize+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]generated.ReportSchedule, 0, query.PageSize+1)
	for rows.Next() {
		var item generated.ReportSchedule
		var reportType, frequency, state string
		var revision int64
		if err = rows.Scan(&item.Id, &reportType, &frequency, &item.MinuteUtc, &item.HourUtc,
			&item.WeekdayUtc, &state, &item.NextRunAt, &item.LastRunAt, &revision,
			&item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.ReportType, item.Frequency = generated.ReportScheduleReportType(reportType), generated.ReportScheduleFrequency(frequency)
		item.State, item.Revision = generated.ReportScheduleState(state), strconv.FormatInt(revision, 10)
		items = append(items, item)
	}
	return items, rows.Err()
}

// OperationalEvidenceAlert returns sanitized delivery and escalation evidence.
func (store *OwnerConsoleStore) OperationalEvidenceAlert(ctx context.Context, id string) (generated.AlertDetail, error) {
	var item generated.AlertDetail
	var severity, state string
	var revision int64
	err := store.pool.QueryRow(ctx, `SELECT id,severity,reason_code,alert_type,state,
occurrences,revision,correlation_id,incident_id,created_at,last_seen_at FROM alerts WHERE id=$1`, id).Scan(
		&item.Id, &severity, &item.ReasonCode, &item.Component, &state, &item.Occurrences,
		&revision, &item.CorrelationId, &item.IncidentId, &item.CreatedAt, &item.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.AlertDetail{}, console.ErrNotFound
	}
	if err != nil {
		return generated.AlertDetail{}, err
	}
	item.Severity, item.State = generated.AlertDetailSeverity(severity), generated.AlertDetailState(state)
	item.Revision = strconv.FormatInt(revision, 10)
	if item.Deliveries, err = store.operationalEvidenceAlertAttempts(ctx, id); err != nil {
		return generated.AlertDetail{}, err
	}
	item.Escalations, err = store.operationalEvidenceAlertEscalations(ctx, id)
	return item, err
}

func (store *OwnerConsoleStore) operationalEvidenceAlertAttempts(ctx context.Context, id string) ([]generated.AlertDeliveryAttempt, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,sink_name,attempt,state,reason_code,
started_at,completed_at,latency_ms FROM owner_console_alert_delivery_attempts
WHERE alert_id=$1 ORDER BY started_at,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.AlertDeliveryAttempt{}
	for rows.Next() {
		var item generated.AlertDeliveryAttempt
		var state string
		var latency int64
		if err = rows.Scan(&item.Id, &item.SinkName, &item.Attempt, &state, &item.ReasonCode,
			&item.StartedAt, &item.CompletedAt, &latency); err != nil {
			return nil, err
		}
		item.State = generated.AlertDeliveryAttemptState(state)
		item.LatencyMs = &latency
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *OwnerConsoleStore) operationalEvidenceAlertEscalations(ctx context.Context, id string) ([]generated.AlertEscalation, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,revision,actor_user_id,reason,escalated_at
FROM owner_console_alert_escalations WHERE alert_id=$1 ORDER BY revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.AlertEscalation{}
	for rows.Next() {
		var item generated.AlertEscalation
		var revision int64
		if err = rows.Scan(&item.Id, &revision, &item.ActorUserId, &item.Reason, &item.EscalatedAt); err != nil {
			return nil, err
		}
		item.Revision = strconv.FormatInt(revision, 10)
		items = append(items, item)
	}
	return items, rows.Err()
}

// OperationalEvidenceAlertRoutes exposes labels and state only; endpoint URLs and credentials
// remain runtime configuration and are never selected here.
func (store *OwnerConsoleStore) OperationalEvidenceAlertRoutes(ctx context.Context) (generated.AlertRoutePage, error) {
	rows, err := store.pool.Query(ctx, `SELECT route.id,route.sink_name,route.enabled,
route.minimum_severity,route.target_label,route.revision,test.state,test.requested_at
FROM owner_console_alert_routes route LEFT JOIN LATERAL (
  SELECT state,requested_at FROM owner_console_alert_route_tests WHERE route_id=route.id
  ORDER BY requested_at DESC,id DESC LIMIT 1
) test ON true ORDER BY route.id`)
	if err != nil {
		return generated.AlertRoutePage{}, err
	}
	defer rows.Close()
	page := generated.AlertRoutePage{Items: []generated.AlertRoute{}, Revision: "0"}
	var maximum int64
	for rows.Next() {
		var item generated.AlertRoute
		var severity string
		var state *string
		var revision int64
		if err = rows.Scan(&item.Id, &item.SinkName, &item.Enabled, &severity,
			&item.TargetLabel, &revision, &state, &item.LastTestedAt); err != nil {
			return generated.AlertRoutePage{}, err
		}
		item.MinimumSeverity = generated.AlertRouteMinimumSeverity(severity)
		item.Revision = strconv.FormatInt(revision, 10)
		if state != nil {
			value := generated.AlertRouteLastTestState(*state)
			item.LastTestState = &value
		}
		if revision > maximum {
			maximum = revision
		}
		page.Items = append(page.Items, item)
	}
	page.Revision = strconv.FormatInt(maximum, 10)
	return page, rows.Err()
}

var _ console.OperationalEvidenceReadService = (*OwnerConsoleStore)(nil)
