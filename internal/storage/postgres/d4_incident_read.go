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

func (store *A11ConsoleStore) populateD4Incident(
	ctx context.Context, item *generated.IncidentDetail, raw bool,
) error {
	var err error
	if item.Timeline, err = store.d4IncidentTimeline(ctx, item.Id, raw); err != nil {
		return err
	}
	if err = store.d4IncidentReplay(ctx, item); err != nil {
		return err
	}
	if item.RelatedAlertIds, err = d4StringRows(ctx, store.pool,
		`SELECT alert_id FROM v1d_incident_alert_links WHERE incident_id=$1 ORDER BY alert_id`, item.Id); err != nil {
		return err
	}
	if item.RelatedActivityIds, err = d4StringRows(ctx, store.pool,
		`SELECT activity_id FROM v1d_incident_activity_links WHERE incident_id=$1 ORDER BY activity_id`, item.Id); err != nil {
		return err
	}
	if item.EvidenceHolds, err = store.d4IncidentHolds(ctx, item.Id); err != nil {
		return err
	}
	item.RemediationNotes, err = d4StringRows(ctx, store.pool, `SELECT detail FROM v1d_incident_events
WHERE incident_id=$1 AND event_type='remediation_added' ORDER BY incident_revision`, item.Id)
	if err != nil {
		return err
	}
	var evidence string
	err = store.pool.QueryRow(ctx, `SELECT evidence FROM v1d_incident_resolution_evidence
WHERE incident_id=$1`, item.Id).Scan(&evidence)
	if err == nil {
		item.ResolutionEvidence = &evidence
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func (store *A11ConsoleStore) d4IncidentTimeline(
	ctx context.Context, id string, raw bool,
) ([]generated.TimelineEvent, error) {
	rows, err := store.pool.Query(ctx, `SELECT id,event_type,actor,reason,reference_type,
reference_id,event_hash::text,correlation_id,occurred_at,detail
FROM v1d_incident_events WHERE incident_id=$1 ORDER BY incident_revision`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.TimelineEvent{}
	for rows.Next() {
		var item generated.TimelineEvent
		var detail *string
		if err = rows.Scan(&item.Id, &item.EventType, &item.Actor, &item.Reason,
			&item.ReferenceType, &item.ReferenceId, &item.EventHash, &item.CorrelationId,
			&item.OccurredAt, &detail); err != nil {
			return nil, err
		}
		item.Redacted = !raw
		if raw && detail != nil {
			value, _ := json.Marshal(map[string]string{"detail": *detail, "event_hash": *item.EventHash})
			safe := string(value)
			item.SafeDetail = &safe
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d4IncidentReplay(ctx context.Context, item *generated.IncidentDetail) error {
	var first, last int64
	var source string
	err := store.pool.QueryRow(ctx, `SELECT dataset_id,first_ordinal,last_ordinal,source_identity
FROM v1d_incident_replay_inputs WHERE incident_id=$1`, item.Id).Scan(
		&item.ReplayWindow.DatasetId, &first, &last, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		dataset, fallbackFirst, fallbackLast, fallbackErr := a11IncidentReplayWindow(ctx, store.pool, item.Id)
		if errors.Is(fallbackErr, console.ErrPrecondition) {
			return nil
		}
		if fallbackErr != nil {
			return fallbackErr
		}
		item.ReplayWindow.DatasetId, first, last, source = dataset, fallbackFirst, fallbackLast, "qualified-dataset-window"
	} else if err != nil {
		return err
	}
	item.ReplayWindow.FirstOrdinal = strconv.FormatInt(first, 10)
	item.ReplayWindow.LastOrdinal = strconv.FormatInt(last, 10)
	item.ReplayWindow.SourceIdentity = &source
	return nil
}

type d4Rows interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func d4StringRows(ctx context.Context, query d4Rows, statement, id string) ([]string, error) {
	rows, err := query.Query(ctx, statement, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var item *string
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, rows.Err()
}

func (store *A11ConsoleStore) d4IncidentHolds(ctx context.Context, id string) ([]generated.IncidentEvidenceHold, error) {
	rows, err := store.pool.Query(ctx, `SELECT hold.id,hold.artifact_id,hold.hold_type,hold.created_at
FROM v1d_artifact_holds hold WHERE hold.reference_id=$1 AND hold.hold_type='incident'
AND hold.released_at IS NULL ORDER BY hold.created_at,hold.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []generated.IncidentEvidenceHold{}
	for rows.Next() {
		var item generated.IncidentEvidenceHold
		var holdType string
		if err = rows.Scan(&item.Id, &item.ArtifactId, &holdType, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.HoldType = generated.IncidentEvidenceHoldHoldType(holdType)
		items = append(items, item)
	}
	return items, rows.Err()
}
