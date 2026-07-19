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

// TrendDecisions returns durable decision evidence in newest-first order.
func (store *A11ConsoleStore) TrendDecisions(ctx context.Context, cursor string, limit int) (generated.TrendDecisionPage, error) {
	occurred, id, _, err := decodeA11TimeCursor(store.cursor, "trend-decisions", cursor)
	if err != nil {
		return generated.TrendDecisionPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT d.id,d.outcome,d.reason_code,d.decided_at,td.candle_view_id,td.market_view_id,
    convert_from(td.canonical_explanation,'UTF8') FROM trend_decisions td JOIN decisions d ON d.id=td.decision_id
		WHERE ($1::timestamptz IS NULL OR d.decided_at<$1 OR (d.decided_at=$1 AND d.id<$2))
		ORDER BY d.decided_at DESC,d.id DESC LIMIT $3`, nullableA11Time(occurred), id, limit+1)
	if err != nil {
		return generated.TrendDecisionPage{}, err
	}
	defer rows.Close()
	items := make([]generated.TrendDecision, 0, limit+1)
	for rows.Next() {
		var item generated.TrendDecision
		var outcome string
		if err = rows.Scan(&item.Id, &outcome, &item.ReasonCode, &item.OccurredAt, &item.CandleViewId, &item.MarketViewId, &item.Explanation); err != nil {
			return generated.TrendDecisionPage{}, err
		}
		if outcome == "approved" || outcome == "accepted" {
			item.Outcome = generated.TrendDecisionOutcome("accepted")
		} else {
			item.Outcome = generated.TrendDecisionOutcome("rejected")
		}
		item.Revision = strconv.FormatInt(item.OccurredAt.UnixNano(), 10)
		items = append(items, item)
	}
	page := generated.TrendDecisionPage{Items: items, Revision: "0"}
	if len(items) > 0 {
		page.Revision = items[0].Revision
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeA11TimeCursor(store.cursor, "trend-decisions", last.OccurredAt, last.Id)
		page.NextCursor = &next
	}
	return page, rows.Err()
}

// Job returns one durable backtest or replay lifecycle record.
func (store *A11ConsoleStore) Job(ctx context.Context, id string) (generated.JobResource, error) {
	var item generated.JobResource
	var kind, state string
	var revision int64
	var failure *string
	var result []byte
	err := store.pool.QueryRow(ctx, `SELECT id,job_type,state,created_at,updated_at,progress_revision,failure_code,coalesce(result_payload::text,'') FROM jobs WHERE id=$1`, id).
		Scan(&item.Id, &kind, &state, &item.CreatedAt, &item.UpdatedAt, &revision, &failure, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.JobResource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.JobResource{}, err
	}
	item.Kind = generated.JobResourceKind(kind)
	item.State = generated.JobResourceState(state)
	item.Revision = strconv.FormatInt(revision, 10)
	item.FailureCode = failure
	if kind == "backtest" {
		item.ModeLabel = generated.BACKTEST
	} else {
		item.ModeLabel = generated.REPLAY
	}
	if len(result) > 0 {
		var projection generated.JobResult
		if err = json.Unmarshal(result, &projection); err != nil {
			return generated.JobResource{}, err
		}
		item.Result = &projection
	}
	return item, nil
}

// Shadow returns one public-only simulation session and its simulated orders.
func (store *A11ConsoleStore) Shadow(ctx context.Context, id string) (generated.ShadowSessionResource, error) {
	var item generated.ShadowSessionResource
	var state string
	var revision int64
	err := store.pool.QueryRow(ctx, `SELECT id,state,revision,entries_enabled,created_at,started_at,stopped_at,failure_code FROM shadow_sessions WHERE id=$1`, id).
		Scan(&item.Id, &state, &revision, &item.EntriesEnabled, &item.CreatedAt, &item.StartedAt, &item.StoppedAt, &item.FailureCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return generated.ShadowSessionResource{}, console.ErrNotFound
	}
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	item.State = generated.ShadowSessionResourceState(state)
	item.Revision = strconv.FormatInt(revision, 10)
	item.Label = generated.PUBLICLIVESHADOWVIRTUAL
	item.PublicOnly = true
	item.SimulationOnly = true
	rows, err := store.pool.Query(ctx, `SELECT o.id,o.instrument_id,o.side,o.quantity::text,coalesce((SELECT sum(f.quantity)::text FROM fills f WHERE f.order_id=o.id),'0'),o.state
    FROM orders o JOIN virtual_accounts va ON va.id=o.account_id JOIN shadow_sessions ss ON ss.run_id=va.run_id WHERE ss.id=$1 ORDER BY o.created_at,o.id`, id)
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	defer rows.Close()
	orders := []generated.SimulatedOrder{}
	for rows.Next() {
		var order generated.SimulatedOrder
		var filled string
		if err = rows.Scan(&order.Id, &order.Instrument, &order.Side, &order.Quantity, &filled, &order.State); err != nil {
			return generated.ShadowSessionResource{}, err
		}
		order.FilledQuantity = &filled
		order.LimitPrice = "0"
		order.Simulated = true
		orders = append(orders, order)
	}
	item.Orders = &orders
	return item, rows.Err()
}

// Incidents returns immutable operational incident summaries.
func (store *A11ConsoleStore) Incidents(ctx context.Context, cursor string, limit int) (generated.IncidentPage, error) {
	occurred, id, _, err := decodeA11TimeCursor(store.cursor, "incidents", cursor)
	if err != nil {
		return generated.IncidentPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT id,severity,state,reason_code,opened_at FROM incidents
		WHERE ($1::timestamptz IS NULL OR opened_at<$1 OR (opened_at=$1 AND id<$2))
		ORDER BY opened_at DESC,id DESC LIMIT $3`, nullableA11Time(occurred), id, limit+1)
	if err != nil {
		return generated.IncidentPage{}, err
	}
	defer rows.Close()
	items := make([]generated.IncidentSummary, 0, limit+1)
	for rows.Next() {
		var item generated.IncidentSummary
		if err = rows.Scan(&item.Id, &item.Severity, &item.State, &item.ReasonCode, &item.OpenedAt); err != nil {
			return generated.IncidentPage{}, err
		}
		item.Revision = strconv.FormatInt(item.OpenedAt.UnixNano(), 10)
		items = append(items, item)
	}
	page := generated.IncidentPage{Items: items, Revision: "0"}
	if len(items) > 0 {
		page.Revision = items[0].Revision
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeA11TimeCursor(store.cursor, "incidents", last.OpenedAt, last.Id)
		page.NextCursor = &next
	}
	return page, rows.Err()
}

// Incident returns redacted evidence and a replay window when durable data exists.
func (store *A11ConsoleStore) Incident(ctx context.Context, id string, raw bool) (generated.IncidentDetail, error) {
	var item generated.IncidentDetail
	if err := store.pool.QueryRow(ctx, `SELECT id,severity,state,reason_code,opened_at FROM incidents WHERE id=$1`, id).Scan(&item.Id, &item.Severity, &item.State, &item.ReasonCode, &item.OpenedAt); errors.Is(err, pgx.ErrNoRows) {
		return generated.IncidentDetail{}, console.ErrNotFound
	} else if err != nil {
		return generated.IncidentDetail{}, err
	}
	item.Revision = strconv.FormatInt(item.OpenedAt.UnixNano(), 10)
	item.Timeline = []generated.TimelineEvent{}
	rows, err := store.pool.Query(ctx, `SELECT id,event_type,correlation_id,recorded_at FROM audit_events WHERE causation_id=$1 OR correlation_id=$1 ORDER BY recorded_at,id`, id)
	if err != nil {
		return generated.IncidentDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var event generated.TimelineEvent
		if err = rows.Scan(&event.Id, &event.EventType, &event.CorrelationId, &event.OccurredAt); err != nil {
			return generated.IncidentDetail{}, err
		}
		event.Redacted = !raw
		item.Timeline = append(item.Timeline, event)
	}
	var dataset string
	var first, last int64
	_ = store.pool.QueryRow(ctx, `SELECT id,first_ordinal,last_ordinal FROM market_data_segments WHERE started_at<=$1 AND ended_at>=$1 ORDER BY started_at DESC LIMIT 1`, item.OpenedAt).Scan(&dataset, &first, &last)
	item.ReplayWindow.DatasetId = dataset
	item.ReplayWindow.FirstOrdinal = strconv.FormatInt(first, 10)
	item.ReplayWindow.LastOrdinal = strconv.FormatInt(last, 10)
	return item, rows.Err()
}

// Audit returns immutable redacted administrative events.
func (store *A11ConsoleStore) Audit(ctx context.Context, cursor string, limit int, raw bool) (generated.AuditEventPage, error) {
	occurred, id, _, err := decodeA11TimeCursor(store.cursor, "audit", cursor)
	if err != nil {
		return generated.AuditEventPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT id,event_type,actor,causation_id,correlation_id,recorded_at FROM audit_events
		WHERE ($1::timestamptz IS NULL OR recorded_at<$1 OR (recorded_at=$1 AND id<$2))
		ORDER BY recorded_at DESC,id DESC LIMIT $3`, nullableA11Time(occurred), id, limit+1)
	if err != nil {
		return generated.AuditEventPage{}, err
	}
	defer rows.Close()
	items := make([]generated.AuditEvent, 0, limit+1)
	for rows.Next() {
		var item generated.AuditEvent
		if err = rows.Scan(&item.Id, &item.EventType, &item.Actor, &item.CausationId, &item.CorrelationId, &item.RecordedAt); err != nil {
			return generated.AuditEventPage{}, err
		}
		item.Redacted = !raw
		items = append(items, item)
	}
	page := generated.AuditEventPage{Items: items, Revision: "0"}
	if len(items) > 0 {
		page.Revision = strconv.FormatInt(items[0].RecordedAt.UnixNano(), 10)
	}
	if len(items) > limit {
		page.HasMore = true
		items = items[:limit]
		page.Items = items
		last := items[len(items)-1]
		next := encodeA11TimeCursor(store.cursor, "audit", last.RecordedAt, last.Id)
		page.NextCursor = &next
	}
	return page, rows.Err()
}
