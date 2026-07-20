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

// Shadow returns one public-only simulation session and its simulated orders.
func (store *A11ConsoleStore) Shadow(ctx context.Context, id string) (generated.ShadowSessionResource, error) {
	var item generated.ShadowSessionResource
	var state string
	var revision int64
	err := store.pool.QueryRow(ctx, `SELECT session.id,session.state,session.revision,session.entries_enabled,
	  session.created_at,session.started_at,session.stopped_at,session.failure_code,session.configuration_id,
	  CASE WHEN strategy.id='trend-v1a-1' THEN 'trend.v1a.1' ELSE strategy.version::text END,
	  coalesce(session.decision_dataset_id,''),coalesce(session.model_namespace_id,''),
	  (SELECT count(*)::integer FROM decisions WHERE run_id=session.run_id AND outcome='accepted'),
	  (SELECT count(*)::integer FROM decisions WHERE run_id=session.run_id AND outcome='rejected'),
	  (SELECT count(*)::integer FROM journal_transactions WHERE run_id=session.run_id)
	  FROM shadow_sessions session JOIN strategy_versions strategy ON strategy.id=session.strategy_version_id
	  WHERE session.id=$1`, id).
		Scan(&item.Id, &state, &revision, &item.EntriesEnabled, &item.CreatedAt, &item.StartedAt, &item.StoppedAt,
			&item.FailureCode, &item.ConfigurationId, &item.StrategyVersion, &item.DecisionDatasetId,
			&item.ModelNamespaceId, &item.AcceptedDecisions, &item.RejectedDecisions, &item.JournalTransactions)
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
	rows, err := store.pool.Query(ctx, `SELECT o.id,o.instrument_id,o.side,o.quantity::text,
	coalesce((SELECT sum(f.quantity)::text FROM fills f WHERE f.order_id=o.id),'0'),o.state,
	o.requested_limit_price::text,o.simulation_latency_ms::text
    FROM orders o JOIN virtual_accounts va ON va.id=o.account_id JOIN shadow_sessions ss ON ss.run_id=va.run_id WHERE ss.id=$1 ORDER BY o.created_at,o.id`, id)
	if err != nil {
		return generated.ShadowSessionResource{}, err
	}
	defer rows.Close()
	orders := []generated.SimulatedOrder{}
	for rows.Next() {
		var order generated.SimulatedOrder
		var filled string
		if err = rows.Scan(&order.Id, &order.Instrument, &order.Side, &order.Quantity, &filled, &order.State,
			&order.LimitPrice, &order.LatencyMs); err != nil {
			return generated.ShadowSessionResource{}, err
		}
		order.FilledQuantity = &filled
		order.Simulated = true
		orders = append(orders, order)
	}
	item.Orders = &orders
	return item, rows.Err()
}

// Incidents returns immutable operational incident summaries.
func (store *A11ConsoleStore) Incidents(ctx context.Context, cursor string, limit int, state string) (generated.IncidentPage, error) {
	scope := "incidents:" + state
	occurred, id, _, err := decodeA11TimeCursor(store.cursor, scope, cursor)
	if err != nil {
		return generated.IncidentPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT id,severity,state,reason_code,opened_at FROM incidents
		WHERE ($1='' OR state=$1) AND ($2::timestamptz IS NULL OR opened_at<$2 OR (opened_at=$2 AND id<$3))
		ORDER BY opened_at DESC,id DESC LIMIT $4`, state, nullableA11Time(occurred), id, limit+1)
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
		next := encodeA11TimeCursor(store.cursor, scope, last.OpenedAt, last.Id)
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
	rows, err := store.pool.Query(ctx, `SELECT audit.id,audit.event_type,audit.correlation_id,audit.recorded_at,
	  audit.event_hash::text,command.command_kind,command.target_type,command.target_id,command.reason,
	  command.state,coalesce(command.result_payload::text,'')
	  FROM audit_events audit LEFT JOIN LATERAL (SELECT command_kind,target_type,target_id,reason,state,result_payload
	    FROM command_requests WHERE audit_event_id=audit.id ORDER BY id LIMIT 1) command ON true
	  WHERE audit.causation_id=$1 OR audit.correlation_id=$1 ORDER BY audit.recorded_at,audit.id`, id)
	if err != nil {
		return generated.IncidentDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var event generated.TimelineEvent
		var evidenceHash string
		var commandKind, targetType, targetID, reason, state *string
		var result string
		if err = rows.Scan(&event.Id, &event.EventType, &event.CorrelationId, &event.OccurredAt, &evidenceHash,
			&commandKind, &targetType, &targetID, &reason, &state, &result); err != nil {
			return generated.IncidentDetail{}, err
		}
		event.Redacted = !raw
		if raw {
			detail := a11SafeAuditDetail(evidenceHash, commandKind, targetType, targetID, reason, state, result)
			event.SafeDetail = &detail
		}
		item.Timeline = append(item.Timeline, event)
	}
	dataset, first, last, replayErr := a11IncidentReplayWindow(ctx, store.pool, id)
	if replayErr == nil {
		item.ReplayWindow.DatasetId = dataset
		item.ReplayWindow.FirstOrdinal = strconv.FormatInt(first, 10)
		item.ReplayWindow.LastOrdinal = strconv.FormatInt(last, 10)
	} else if !errors.Is(replayErr, console.ErrPrecondition) {
		return generated.IncidentDetail{}, replayErr
	}
	return item, rows.Err()
}

type a11RowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func a11IncidentReplayWindow(ctx context.Context, query a11RowQuerier, incidentID string) (string, int64, int64, error) {
	var dataset string
	var first, last int64
	err := query.QueryRow(ctx, `SELECT manifest.id,segment.first_ordinal,segment.last_ordinal
		FROM incidents incident
		JOIN market_data_segments segment ON segment.started_at<=incident.opened_at AND segment.ended_at>=incident.opened_at
		JOIN dataset_segments member ON member.segment_id=segment.id
		JOIN dataset_manifests manifest ON manifest.id=member.dataset_id
		WHERE incident.id=$1 AND segment.state='ready' AND manifest.state='qualified'
		  AND manifest.dataset_kind='decision_inputs'
		ORDER BY manifest.manifest_revision DESC NULLS LAST,manifest.created_at DESC,manifest.id DESC,member.ordinal DESC
		LIMIT 1`, incidentID).Scan(&dataset, &first, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, 0, console.ErrPrecondition
	}
	if err != nil || dataset == "" || first <= 0 || last < first {
		if err != nil {
			return "", 0, 0, err
		}
		return "", 0, 0, console.ErrPrecondition
	}
	return dataset, first, last, nil
}

// Audit returns immutable redacted administrative events.
func (store *A11ConsoleStore) Audit(ctx context.Context, cursor string, limit int, eventType string, raw bool) (generated.AuditEventPage, error) {
	scope := "audit:" + eventType
	occurred, id, _, err := decodeA11TimeCursor(store.cursor, scope, cursor)
	if err != nil {
		return generated.AuditEventPage{}, err
	}
	rows, err := store.pool.Query(ctx, `SELECT audit.id,audit.event_type,audit.actor,audit.causation_id,
	  audit.correlation_id,audit.recorded_at,audit.event_hash::text,command.command_kind,command.target_type,
	  command.target_id,command.reason,command.state,coalesce(command.result_payload::text,'')
	  FROM audit_events audit LEFT JOIN LATERAL (SELECT command_kind,target_type,target_id,reason,state,result_payload
	    FROM command_requests WHERE audit_event_id=audit.id ORDER BY id LIMIT 1) command ON true
	  WHERE ($1='' OR audit.event_type=$1) AND ($2::timestamptz IS NULL OR audit.recorded_at<$2 OR
	    (audit.recorded_at=$2 AND audit.id<$3)) ORDER BY audit.recorded_at DESC,audit.id DESC LIMIT $4`,
		eventType, nullableA11Time(occurred), id, limit+1)
	if err != nil {
		return generated.AuditEventPage{}, err
	}
	defer rows.Close()
	items := make([]generated.AuditEvent, 0, limit+1)
	for rows.Next() {
		var item generated.AuditEvent
		var evidenceHash string
		var commandKind, targetType, targetID, reason, state *string
		var result string
		if err = rows.Scan(&item.Id, &item.EventType, &item.Actor, &item.CausationId, &item.CorrelationId,
			&item.RecordedAt, &evidenceHash, &commandKind, &targetType, &targetID, &reason, &state, &result); err != nil {
			return generated.AuditEventPage{}, err
		}
		item.Redacted = !raw
		if raw {
			detail := a11SafeAuditDetail(evidenceHash, commandKind, targetType, targetID, reason, state, result)
			item.SafeDetail = &detail
		}
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
		next := encodeA11TimeCursor(store.cursor, scope, last.RecordedAt, last.Id)
		page.NextCursor = &next
	}
	return page, rows.Err()
}

func a11SafeAuditDetail(eventHash string, commandKind, targetType, targetID, reason, state *string, result string) string {
	detail := struct {
		EventHash   string          `json:"event_hash"`
		CommandKind *string         `json:"command_kind,omitempty"`
		TargetType  *string         `json:"target_type,omitempty"`
		TargetID    *string         `json:"target_id,omitempty"`
		Reason      *string         `json:"reason,omitempty"`
		State       *string         `json:"state,omitempty"`
		Result      json.RawMessage `json:"result,omitempty"`
	}{EventHash: eventHash, CommandKind: commandKind, TargetType: targetType, TargetID: targetID,
		Reason: reason, State: state}
	if json.Valid([]byte(result)) {
		detail.Result = json.RawMessage(result)
	}
	canonical, err := json.Marshal(detail)
	if err != nil || len(canonical) > 2000 {
		canonical, _ = json.Marshal(struct {
			EventHash string `json:"event_hash"`
		}{EventHash: eventHash})
	}
	return string(canonical)
}
